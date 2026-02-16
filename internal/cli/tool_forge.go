package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
)

const (
	toolForgeDefaultMaxFiles = 20
	toolForgeDefaultMaxFixes = 2
)

type toolManifestEntry struct {
	Time     string            `json:"time"`
	Event    string            `json:"event,omitempty"`
	Attempt  int               `json:"attempt,omitempty"`
	Name     string            `json:"name"`
	Language string            `json:"language"`
	Purpose  string            `json:"purpose,omitempty"`
	Files    []string          `json:"files"`
	Hashes   map[string]string `json:"hashes,omitempty"`
	Run      string            `json:"run"`
}

func (r *Runner) executeToolSuggestion(tool assist.ToolSpec, dryRun bool) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: tool forge not permitted")
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	tool.Language = strings.ToLower(strings.TrimSpace(tool.Language))
	tool.Name = strings.TrimSpace(tool.Name)
	tool.Purpose = strings.TrimSpace(tool.Purpose)
	if tool.Language == "" {
		tool.Language = "python"
	}
	if tool.Name == "" {
		tool.Name = "tool"
	}
	if len(tool.Files) == 0 {
		return fmt.Errorf("tool forge: no files provided")
	}
	if strings.TrimSpace(tool.Run.Command) == "" {
		return fmt.Errorf("tool forge: missing run.command")
	}

	maxFiles := toolForgeDefaultMaxFiles
	if r.cfg.Agent.ToolMaxFiles > 0 {
		maxFiles = r.cfg.Agent.ToolMaxFiles
	}
	if len(tool.Files) > maxFiles {
		return fmt.Errorf("tool forge: too many files (%d > %d)", len(tool.Files), maxFiles)
	}

	toolsRoot := filepath.Join(sessionDir, "artifacts", "tools")
	if err := os.MkdirAll(toolsRoot, 0o755); err != nil {
		return fmt.Errorf("tool forge mkdir: %w", err)
	}

	// Build -> run -> (optional) fix loop.
	maxFixes := toolForgeDefaultMaxFixes
	if r.cfg.Agent.ToolMaxFixes > 0 {
		maxFixes = r.cfg.Agent.ToolMaxFixes
	}
	current := tool
	var lastErr error
	for attempt := 0; attempt <= maxFixes; attempt++ {
		if attempt > 0 {
			r.logger.Printf("Tool recovery attempt %d/%d", attempt, maxFixes)
		}
		runErr := r.buildAndRunTool(sessionDir, toolsRoot, current, dryRun, attempt)
		if runErr == nil {
			return nil
		}
		lastErr = runErr
		if dryRun || attempt >= maxFixes || !r.llmAllowed() {
			return lastErr
		}

		// Ask the assistant for a corrected tool spec. This keeps the repair loop local and bounded.
		recoveryGoal := r.buildToolRecoveryGoal(current, lastErr)
		stopIndicator := r.startLLMIndicatorIfAllowed("tool recover")
		recovery, err := r.getAssistSuggestion(recoveryGoal, "recover")
		stopIndicator()
		if err != nil {
			r.logger.Printf("Tool recovery suggestion failed: %v", err)
			return lastErr
		}
		if recovery.Type != "tool" || recovery.Tool == nil {
			r.logger.Printf("Tool recovery returned %s; expected tool.", recovery.Type)
			return lastErr
		}
		current = *recovery.Tool
	}
	return lastErr
}

func (r *Runner) buildAndRunTool(sessionDir, toolsRoot string, tool assist.ToolSpec, dryRun bool, attempt int) error {
	tool.Language = strings.ToLower(strings.TrimSpace(tool.Language))
	tool.Name = strings.TrimSpace(tool.Name)
	tool.Purpose = strings.TrimSpace(tool.Purpose)
	if tool.Language == "" {
		tool.Language = "python"
	}
	if tool.Name == "" {
		tool.Name = "tool"
	}
	if len(tool.Files) == 0 {
		return fmt.Errorf("tool forge: no files provided")
	}
	maxFiles := toolForgeDefaultMaxFiles
	if r.cfg.Agent.ToolMaxFiles > 0 {
		maxFiles = r.cfg.Agent.ToolMaxFiles
	}
	if len(tool.Files) > maxFiles {
		return fmt.Errorf("tool forge: too many files (%d > %d)", len(tool.Files), maxFiles)
	}
	if strings.TrimSpace(tool.Run.Command) == "" {
		return fmt.Errorf("tool forge: missing run.command")
	}

	plannedFiles := make([]string, 0, len(tool.Files))
	hashes := map[string]string{}
	for _, f := range tool.Files {
		if strings.TrimSpace(f.Path) == "" {
			continue
		}
		outPath, err := r.resolveToolWritePath(sessionDir, f.Path)
		if err != nil {
			return err
		}
		plannedFiles = append(plannedFiles, outPath)
		sum := sha256.Sum256([]byte(f.Content))
		hashes[outPath] = hex.EncodeToString(sum[:])
	}
	if len(plannedFiles) == 0 {
		return fmt.Errorf("tool forge: no valid file paths")
	}

	requireApproval := r.cfg.Permissions.Level == "default" && r.cfg.Permissions.RequireApproval
	upToDate, upToDateReason := r.toolFilesUpToDate(sessionDir, tool.Files, hashes)
	if requireApproval && !dryRun && !upToDate {
		prompt := fmt.Sprintf("Write tool '%s' (%s) files (%d) under %s%s?",
			tool.Name, tool.Language, len(plannedFiles), toolsRoot, renderToolFilePreview(plannedFiles))
		ok, err := r.confirm(prompt)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("write not approved")
		}
	}

	r.setTask("tool")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	toolLogPath := ""
	if !dryRun {
		if !upToDate {
			for _, f := range tool.Files {
				if strings.TrimSpace(f.Path) == "" {
					continue
				}
				outPath, err := r.resolveToolWritePath(sessionDir, f.Path)
				if err != nil {
					return err
				}
				if len(f.Content) > writeFileMaxBytes {
					return fmt.Errorf("tool forge: file too large: %s (%d bytes > %d)", f.Path, len(f.Content), writeFileMaxBytes)
				}
				if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
					return fmt.Errorf("tool forge mkdir: %w", err)
				}
				if err := os.WriteFile(outPath, []byte(f.Content), 0o644); err != nil {
					return fmt.Errorf("tool forge write: %w", err)
				}
			}
		}

		logPrefix := "tool"
		if upToDate {
			logPrefix = "tool-reuse"
		}
		toolLogPath, _ = writeFSLog(sessionDir, logPrefix, renderToolLog(tool, plannedFiles)+renderToolReuseNote(upToDate, upToDateReason))
		event := "build"
		if upToDate {
			event = "reuse"
		}
		_ = r.appendToolManifest(sessionDir, toolManifestEntry{
			Time:     time.Now().UTC().Format(time.RFC3339),
			Event:    event,
			Attempt:  attempt,
			Name:     tool.Name,
			Language: tool.Language,
			Purpose:  tool.Purpose,
			Files:    plannedFiles,
			Hashes:   hashes,
			Run:      strings.TrimSpace(strings.Join(append([]string{tool.Run.Command}, tool.Run.Args...), " ")),
		})
	}

	if toolLogPath != "" {
		r.recordActionArtifact(toolLogPath)
		kind := "tool_build"
		if upToDate {
			kind = "tool_reuse"
		}
		r.recordObservation(kind, []string{tool.Name, tool.Language, fmt.Sprintf("attempt=%d", attempt)}, toolLogPath, fmt.Sprintf("files=%d", len(plannedFiles)), nil)
	}

	if dryRun {
		fmt.Printf("Tool (dry-run): %s (%s)\n", tool.Name, tool.Language)
		return nil
	}

	cmd, args := r.normalizeToolRun(tool.Run.Command, tool.Run.Args)
	if requireApproval {
		cmdLine := strings.TrimSpace(strings.Join(append([]string{cmd}, args...), " "))
		ok, err := r.confirm(fmt.Sprintf("Run tool '%s': %s?", tool.Name, cmdLine))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("execution not approved")
		}
	}
	return r.executeToolRun(cmd, args)
}

func (r *Runner) normalizeToolRun(command string, args []string) (string, []string) {
	command = strings.TrimSpace(command)
	outArgs := make([]string, 0, len(args))
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	toolsRoot := filepath.Join(sessionDir, "artifacts", "tools")
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		a = strings.ReplaceAll(a, "{{TOOLS_DIR}}", toolsRoot)
		a = strings.ReplaceAll(a, "$TOOLS_DIR", toolsRoot)
		// If the argument is a relative path that exists under toolsRoot, make it absolute.
		if !filepath.IsAbs(a) {
			candidate := filepath.Join(toolsRoot, filepath.Clean(a))
			if pathWithinRoot(candidate, toolsRoot) {
				if _, err := os.Stat(candidate); err == nil {
					a = candidate
				}
			}
		}
		outArgs = append(outArgs, a)
	}
	return command, outArgs
}

func (r *Runner) toolFilesUpToDate(sessionDir string, files []assist.ToolFile, desiredHashes map[string]string) (bool, string) {
	if len(files) == 0 {
		return false, "no files"
	}
	if len(desiredHashes) == 0 {
		return false, "no hashes"
	}
	for _, f := range files {
		if strings.TrimSpace(f.Path) == "" {
			continue
		}
		outPath, err := r.resolveToolWritePath(sessionDir, f.Path)
		if err != nil {
			return false, "invalid path"
		}
		want := strings.TrimSpace(desiredHashes[outPath])
		if want == "" {
			return false, "missing desired hash"
		}
		data, err := os.ReadFile(outPath)
		if err != nil {
			return false, "missing file"
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if got != want {
			return false, "content changed"
		}
	}
	return true, "match"
}

func (r *Runner) buildToolRecoveryGoal(tool assist.ToolSpec, runErr error) string {
	builder := strings.Builder{}
	builder.WriteString("The previous tool run failed. Provide a corrected tool spec (type=tool) that fixes the issue.\n")
	if tool.Name != "" {
		builder.WriteString("Tool name: " + tool.Name + "\n")
	}
	if tool.Language != "" {
		builder.WriteString("Tool language: " + tool.Language + "\n")
	}
	if tool.Purpose != "" {
		builder.WriteString("Tool purpose: " + tool.Purpose + "\n")
	}
	if strings.TrimSpace(tool.Run.Command) != "" {
		builder.WriteString("Tool run: " + strings.TrimSpace(strings.Join(append([]string{tool.Run.Command}, tool.Run.Args...), " ")) + "\n")
	}
	if len(tool.Files) > 0 {
		builder.WriteString("Tool files:\n")
		for _, f := range tool.Files {
			if strings.TrimSpace(f.Path) == "" {
				continue
			}
			builder.WriteString("- " + f.Path + "\n")
		}
	}
	if runErr != nil {
		builder.WriteString("Error: " + runErr.Error() + "\n")
		var cmdErr commandError
		if errors.As(runErr, &cmdErr) {
			if out := firstLines(cmdErr.Result.Output, 6); out != "" {
				builder.WriteString("Output:\n" + out + "\n")
			}
			if strings.TrimSpace(cmdErr.Result.LogPath) != "" {
				builder.WriteString("Log path: " + cmdErr.Result.LogPath + "\n")
			}
		}
	}
	builder.WriteString("Return JSON with type=tool. Modify files/run as needed. Keep changes minimal.")
	return builder.String()
}

func renderToolReuseNote(upToDate bool, reason string) string {
	if !upToDate {
		return ""
	}
	if strings.TrimSpace(reason) == "" {
		reason = "match"
	}
	return "\nReuse: true (" + reason + ")\n"
}

func renderToolFilePreview(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	max := 5
	if len(paths) < max {
		max = len(paths)
	}
	builder := strings.Builder{}
	builder.WriteString(" (e.g. ")
	for i := 0; i < max; i++ {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(filepath.Base(paths[i]))
	}
	if len(paths) > max {
		builder.WriteString(", ...")
	}
	builder.WriteString(")")
	return builder.String()
}

func renderToolLog(tool assist.ToolSpec, outFiles []string) string {
	builder := strings.Builder{}
	builder.WriteString(fmt.Sprintf("Tool: %s\nLanguage: %s\n", tool.Name, tool.Language))
	if tool.Purpose != "" {
		builder.WriteString("Purpose: " + tool.Purpose + "\n")
	}
	builder.WriteString(fmt.Sprintf("Files: %d\n", len(outFiles)))
	for _, path := range outFiles {
		builder.WriteString("- " + path + "\n")
	}
	builder.WriteString("\nRun:\n")
	builder.WriteString(strings.TrimSpace(strings.Join(append([]string{tool.Run.Command}, tool.Run.Args...), " ")) + "\n")
	return builder.String()
}

func (r *Runner) appendToolManifest(sessionDir string, entry toolManifestEntry) error {
	toolsRoot := filepath.Join(sessionDir, "artifacts", "tools")
	if err := os.MkdirAll(toolsRoot, 0o755); err != nil {
		return err
	}
	path := filepath.Join(toolsRoot, "manifest.json")
	var entries []toolManifestEntry
	if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
		_ = json.Unmarshal(data, &entries)
	}
	entries = append(entries, entry)
	if len(entries) > 200 {
		entries = entries[len(entries)-200:]
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (r *Runner) executeToolRun(command string, args []string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("tool run: empty command")
	}

	// Allow internal primitives.
	switch strings.ToLower(command) {
	case "browse":
		return r.handleBrowse(args)
	case "crawl":
		return r.handleCrawl(args)
	case "parse_links", "links":
		return r.handleParseLinks(args)
	case "read_file", "read":
		return r.handleReadFile(args)
	case "list_dir", "ls":
		return r.handleListDir(args)
	case "write_file", "write":
		return r.handleWriteFile(args)
	}

	if !r.cfg.Tools.Shell.Enabled {
		return fmt.Errorf("shell execution disabled by config")
	}

	start := time.Now()
	timeout := time.Duration(r.cfg.Tools.Shell.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interruptCh, stopInterrupt, keyErr := startInterruptWatcher()
	if keyErr == nil {
		r.logger.Printf("Press ESC or Ctrl-C to interrupt")
	} else if r.isTTY() {
		r.logger.Printf("Ctrl-C to interrupt (ESC unavailable: %v)", keyErr)
	}
	if interruptCh != nil {
		go func() {
			<-interruptCh
			cancel()
		}()
	}

	liveWriter := r.liveWriter()
	activityWriter := newActivityWriter(liveWriter)
	stopIndicator := r.startWorkingIndicator(activityWriter)
	defer stopIndicator()
	if activityWriter != nil {
		liveWriter = activityWriter
	}

	runner := exec.Runner{
		Permissions:      exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval:  false,
		LogDir:           filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:          timeout,
		Reader:           r.reader,
		ScopeNetworks:    r.cfg.Scope.Networks,
		ScopeTargets:     r.cfg.Scope.Targets,
		ScopeDenyTargets: r.cfg.Scope.DenyTargets,
		LiveWriter:       liveWriter,
	}

	result, err := runner.RunCommandWithContext(ctx, command, args...)
	wasCanceled := errors.Is(err, context.Canceled)
	if stopInterrupt != nil {
		stopInterrupt()
	}
	r.ensureTTYLineBreak()
	if result.LogPath != "" {
		r.logger.Printf("Log saved: %s", result.LogPath)
		r.recordActionArtifact(result.LogPath)
	}
	r.recordObservationFromResult("tool_run", result, err)
	r.maybeAutoSummarize(result.LogPath, "tool_run")

	fmt.Print(renderExecSummary(r.currentTask, command, args, time.Since(start), result.LogPath, "disabled", result.Output, err))
	if err != nil {
		if wasCanceled {
			r.logger.Printf("Interrupted. What should I do differently?")
			return fmt.Errorf("command interrupted")
		}
		return commandError{Result: result, Err: err}
	}
	return nil
}
