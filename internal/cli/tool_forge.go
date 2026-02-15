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
	toolForgeMaxFiles = 20
)

type toolManifestEntry struct {
	Time     string            `json:"time"`
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
	if len(tool.Files) > toolForgeMaxFiles {
		return fmt.Errorf("tool forge: too many files (%d > %d)", len(tool.Files), toolForgeMaxFiles)
	}
	if strings.TrimSpace(tool.Run.Command) == "" {
		return fmt.Errorf("tool forge: missing run.command")
	}

	toolsRoot := filepath.Join(sessionDir, "artifacts", "tools")
	if err := os.MkdirAll(toolsRoot, 0o755); err != nil {
		return fmt.Errorf("tool forge mkdir: %w", err)
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
	if requireApproval && !dryRun {
		cmdLine := strings.TrimSpace(strings.Join(append([]string{tool.Run.Command}, tool.Run.Args...), " "))
		prompt := fmt.Sprintf("Build tool '%s' (%s), write %d file(s) under %s, then run: %s?",
			tool.Name, tool.Language, len(plannedFiles), toolsRoot, cmdLine)
		ok, err := r.confirm(prompt)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("execution not approved")
		}
	}

	r.setTask("tool")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	toolLogPath := ""
	if !dryRun {
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

		toolLogPath, _ = writeFSLog(sessionDir, "tool", renderToolLog(tool, plannedFiles))
		_ = r.appendToolManifest(sessionDir, toolManifestEntry{
			Time:     time.Now().UTC().Format(time.RFC3339),
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
		r.recordObservation("tool_build", []string{tool.Name, tool.Language}, toolLogPath, fmt.Sprintf("files=%d", len(plannedFiles)), nil)
	}

	if dryRun {
		fmt.Printf("Tool (dry-run): %s (%s)\n", tool.Name, tool.Language)
		return nil
	}

	return r.executeToolRun(tool.Run.Command, tool.Run.Args)
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
