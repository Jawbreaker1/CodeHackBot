package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/msf"
)

func executeWorkerAssistCommand(ctx context.Context, command string, args []string, workDir string) workerToolResult {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)

	if isBuiltinListDir(command) {
		output, err := builtinListDir(args, workDir)
		return workerToolResult{command: "list_dir", args: args, output: output, runErr: err}
	}
	if isBuiltinReadFile(command) {
		output, err := builtinReadFile(args, workDir)
		return workerToolResult{command: "read_file", args: args, output: output, runErr: err}
	}
	if isBuiltinWriteFile(command) {
		output, err := builtinWriteFile(args, workDir)
		return workerToolResult{command: "write_file", args: args, output: output, runErr: err}
	}
	if isBuiltinBrowse(command) {
		output, err := builtinBrowse(ctx, args)
		return workerToolResult{command: "browse", args: args, output: output, runErr: err}
	}
	adaptedCmd, adaptedArgs, notes, adaptErr := msf.AdaptRuntimeCommand(command, args, workDir)
	if adaptErr != nil {
		return workerToolResult{command: command, args: args, output: []byte(adaptErr.Error() + "\n"), runErr: adaptErr}
	}
	command = adaptedCmd
	args = adaptedArgs

	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	output, err := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
	if len(notes) > 0 {
		prefix := strings.Join(notes, "\n")
		if prefix != "" {
			prefix += "\n"
		}
		output = append([]byte(prefix), output...)
	}
	return workerToolResult{command: command, args: args, output: output, runErr: err}
}

func executeWorkerAssistTool(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, scopePolicy *ScopePolicy, workDir string, spec *assist.ToolSpec) (workerToolResult, error) {
	if spec == nil {
		return workerToolResult{}, fmt.Errorf("tool suggestion missing spec")
	}
	runCommand := strings.TrimSpace(spec.Run.Command)
	if runCommand == "" {
		return workerToolResult{}, fmt.Errorf("tool suggestion missing run.command")
	}
	files := spec.Files
	if len(files) == 0 {
		return workerToolResult{}, fmt.Errorf("tool suggestion has no files")
	}
	if len(files) > 20 {
		return workerToolResult{}, fmt.Errorf("tool suggestion too many files (%d > 20)", len(files))
	}

	toolRoot := filepath.Join(workDir, "tools")
	if err := os.MkdirAll(toolRoot, 0o755); err != nil {
		return workerToolResult{}, fmt.Errorf("create tool root: %w", err)
	}

	var builder strings.Builder
	for _, file := range files {
		relPath := strings.TrimSpace(file.Path)
		if relPath == "" {
			continue
		}
		if filepath.IsAbs(relPath) {
			return workerToolResult{}, fmt.Errorf("tool file must be relative: %s", relPath)
		}
		dst := filepath.Clean(filepath.Join(toolRoot, relPath))
		if !pathWithinRoot(dst, toolRoot) {
			return workerToolResult{}, fmt.Errorf("tool file escapes tool root: %s", relPath)
		}
		if len(file.Content) > workerAssistWriteMaxBytes {
			return workerToolResult{}, fmt.Errorf("tool file too large: %s", relPath)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return workerToolResult{}, fmt.Errorf("create tool parent dir: %w", err)
		}
		if err := os.WriteFile(dst, []byte(file.Content), 0o644); err != nil {
			return workerToolResult{}, fmt.Errorf("write tool file: %w", err)
		}
		_, _ = fmt.Fprintf(&builder, "Wrote tool file: %s\n", dst)
	}

	args := normalizeArgs(spec.Run.Args)
	runCommand, args = normalizeWorkerAssistCommand(runCommand, args)
	args, _, _ = applyCommandTargetFallback(scopePolicy, task, runCommand, args)
	var adaptedNote string
	runCommand, args, adaptedNote, _ = adaptCommandForRuntime(scopePolicy, runCommand, args)
	adaptedCmd, adaptedArgs, msfNotes, adaptErr := msf.AdaptRuntimeCommand(runCommand, args, workDir)
	if adaptErr != nil {
		return workerToolResult{}, adaptErr
	}
	runCommand = adaptedCmd
	args = adaptedArgs
	for _, note := range msfNotes {
		if strings.TrimSpace(note) != "" {
			_, _ = fmt.Fprintf(&builder, "%s\n", note)
		}
	}
	if err := scopePolicy.ValidateCommandTargets(runCommand, args); err != nil {
		return workerToolResult{}, fmt.Errorf("%s: %w", WorkerFailureScopeDenied, err)
	}
	cmd := exec.Command(runCommand, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	output, runErr := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
	if adaptedNote != "" {
		_, _ = fmt.Fprintf(&builder, "Runtime adaptation: %s\n", adaptedNote)
	}
	builder.Write(output)
	return workerToolResult{
		command: runCommand,
		args:    args,
		output:  capBytes([]byte(builder.String()), workerAssistOutputLimit),
		runErr:  runErr,
	}, nil
}

func normalizeWorkerAssistCommand(command string, args []string) (string, []string) {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	if command == "" {
		return "", args
	}
	parts := strings.Fields(command)
	if len(parts) > 1 && len(args) == 0 {
		return parts[0], parts[1:]
	}
	return command, args
}

func isAssistInvalidToolSpecError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	if lower == "" {
		return false
	}
	invalidHints := []string{
		"tool suggestion missing spec",
		"tool suggestion has no files",
		"tool suggestion too many files",
		"tool suggestion missing run.command",
	}
	for _, hint := range invalidHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func applyCommandTargetFallback(scopePolicy *ScopePolicy, task TaskSpec, command string, args []string) ([]string, bool, string) {
	if scopePolicy == nil {
		return args, false, ""
	}
	command = strings.TrimSpace(command)
	if command == "" || !isNetworkSensitiveCommand(command) {
		return args, false, ""
	}
	if len(scopePolicy.extractTargets(command, args)) > 0 {
		return args, false, ""
	}
	fallback := firstTaskTarget(task.Targets)
	if fallback == "" {
		fallback = scopePolicy.FirstAllowedTarget()
	}
	if fallback == "" {
		return args, false, ""
	}
	updated := append(append([]string{}, args...), fallback)
	return updated, true, fallback
}

func firstTaskTarget(targets []string) string {
	for _, target := range targets {
		trimmed := strings.TrimSpace(target)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func writeWorkerActionLog(cfg WorkerRunConfig, filename string, output []byte) (string, error) {
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	if strings.TrimSpace(filename) == "" {
		filename = fmt.Sprintf("%s-a%d.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt)
	}
	path := filepath.Join(artifactDir, sanitizePathComponent(filename))
	if err := os.WriteFile(path, capBytes(output, workerAssistOutputLimit), 0o644); err != nil {
		return "", fmt.Errorf("write action log: %w", err)
	}
	return path, nil
}

func summarizeCommandResult(command string, args []string, runErr error, output []byte) string {
	joined := strings.TrimSpace(strings.Join(append([]string{strings.TrimSpace(command)}, args...), " "))
	if joined == "" {
		joined = "(command)"
	}
	if runErr != nil {
		return fmt.Sprintf("command failed: %s | %v | output: %s", joined, runErr, summarizeOutput(output))
	}
	return fmt.Sprintf("command ok: %s | output: %s", joined, summarizeOutput(output))
}

func summarizeSuggestion(suggestion assist.Suggestion) string {
	parts := []string{suggestion.Type}
	if s := strings.TrimSpace(suggestion.Summary); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(suggestion.Plan); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " | ")
}

func summarizeOutput(output []byte) string {
	text := strings.TrimSpace(string(capBytes(output, 800)))
	if text == "" {
		return "(no output)"
	}
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 300 {
		return text[:300] + "..."
	}
	return text
}

func appendObservation(observations []string, entry string) []string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return observations
	}
	observations = append(observations, entry)
	if len(observations) <= workerAssistObsLimit {
		return observations
	}
	return append([]string{}, observations[len(observations)-workerAssistObsLimit:]...)
}

func capBytes(data []byte, max int) []byte {
	if max <= 0 || len(data) <= max {
		return data
	}
	out := make([]byte, max)
	copy(out, data[:max])
	return out
}

func buildAssistActionKey(command string, args []string) string {
	parts := append([]string{strings.ToLower(strings.TrimSpace(command))}, args...)
	return strings.Join(parts, "\x1f")
}
