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

func executeWorkerAssistCommand(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, command string, args []string, workDir string) workerToolResult {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	runtimeNotes := []string{}
	if cfg.Diagnostic {
		runtimeNotes = append(runtimeNotes, "diagnostic mode: skipped runtime input repair")
	} else {
		if repairedArgs, repairNotes, repaired, repairErr := repairMissingCommandInputPaths(cfg, task, command, args); repairErr != nil {
			runtimeNotes = append(runtimeNotes, fmt.Sprintf("runtime input repair skipped: %v", repairErr))
		} else if repaired {
			args = repairedArgs
			runtimeNotes = append(runtimeNotes, repairNotes...)
		}
	}

	if isBuiltinListDir(command) {
		output, err := builtinListDir(args, workDir)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "list_dir", args: args, output: output, runErr: err}
	}
	if isBuiltinReadFile(command) {
		output, err := builtinReadFile(args, workDir)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "read_file", args: args, output: output, runErr: err}
	}
	if isBuiltinWriteFile(command) {
		output, err := builtinWriteFile(args, workDir)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "write_file", args: args, output: output, runErr: err}
	}
	if isBuiltinBrowse(command) {
		output, err := builtinBrowse(ctx, args)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "browse", args: args, output: output, runErr: err}
	}
	if isBuiltinCrawl(command) {
		output, err := builtinCrawl(ctx, args)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "crawl", args: args, output: output, runErr: err}
	}
	if isBuiltinReport(command) {
		output, err := builtinReport(ctx, cfg, task, args, workDir)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "report", args: args, output: output, runErr: err}
	}
	msfNotes := []string{}
	if !cfg.Diagnostic {
		adaptedCmd, adaptedArgs, nextNotes, adaptErr := msf.AdaptRuntimeCommand(command, args, workDir)
		if adaptErr != nil {
			output := prependWorkerOutputNotes([]byte(adaptErr.Error()+"\n"), runtimeNotes)
			return workerToolResult{command: command, args: args, output: output, runErr: adaptErr}
		}
		command = adaptedCmd
		args = adaptedArgs
		msfNotes = nextNotes
	} else {
		runtimeNotes = append(runtimeNotes, "diagnostic mode: skipped runtime command adaptation")
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	env, guardrailNotes, envErr := applyWorkerRuntimeGuardrails(os.Environ(), workDir)
	if envErr != nil {
		return workerToolResult{command: command, args: args, output: []byte(envErr.Error() + "\n"), runErr: envErr}
	}
	cmd.Env = env
	output, err := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
	noteLines := append([]string{}, runtimeNotes...)
	noteLines = append(noteLines, guardrailNotes...)
	noteLines = append(noteLines, msfNotes...)
	output = prependWorkerOutputNotes(output, noteLines)
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
	if !cfg.Diagnostic {
		runCommand, args = normalizeWorkerAssistCommand(runCommand, args)
	} else {
		_, _ = fmt.Fprintf(&builder, "Diagnostic mode: skipped shell command normalization\n")
	}
	if isWorkerBuiltinCommand(runCommand) {
		if !cfg.Diagnostic {
			var injected bool
			var target string
			args, injected, target = applyCommandTargetFallback(scopePolicy, task, runCommand, args)
			if injected {
				_, _ = fmt.Fprintf(&builder, "Runtime adaptation: auto-injected target %s for command %s\n", target, runCommand)
			}
		} else {
			_, _ = fmt.Fprintf(&builder, "Diagnostic mode: skipped target auto-injection for builtin command\n")
		}
		if requiresCommandScopeValidation(runCommand, args) {
			if err := scopePolicy.ValidateCommandTargets(runCommand, args); err != nil {
				return workerToolResult{}, fmt.Errorf("%s: %w", WorkerFailureScopeDenied, err)
			}
		}
		result := executeWorkerAssistCommand(ctx, cfg, task, runCommand, args, workDir)
		builder.Write(result.output)
		return workerToolResult{
			command: result.command,
			args:    result.args,
			output:  capBytes([]byte(builder.String()), workerAssistOutputLimit),
			runErr:  result.runErr,
		}, nil
	}
	var adaptedNote string
	msfNotes := []string{}
	if !cfg.Diagnostic {
		prepared := prepareRuntimeCommand(scopePolicy, task, runCommand, args)
		runCommand = prepared.Command
		args = prepared.Args
		adaptedNote = prepared.AdaptationNote
		if prepared.InjectedFollowupTarget {
			_, _ = fmt.Fprintf(&builder, "Runtime adaptation follow-up: auto-injected target %s for command %s\n", prepared.FollowupTarget, runCommand)
		}
		adaptedCmd, adaptedArgs, nextNotes, adaptErr := msf.AdaptRuntimeCommand(runCommand, args, workDir)
		if adaptErr != nil {
			return workerToolResult{}, adaptErr
		}
		runCommand = adaptedCmd
		args = adaptedArgs
		msfNotes = nextNotes
	} else {
		_, _ = fmt.Fprintf(&builder, "Diagnostic mode: skipped target auto-injection and runtime command adaptation\n")
	}
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
	env, guardrailNotes, envErr := applyWorkerRuntimeGuardrails(os.Environ(), workDir)
	if envErr != nil {
		return workerToolResult{}, envErr
	}
	cmd.Env = env
	for _, note := range guardrailNotes {
		if strings.TrimSpace(note) != "" {
			_, _ = fmt.Fprintf(&builder, "%s\n", note)
		}
	}
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

func prependWorkerOutputNotes(output []byte, notes []string) []byte {
	lines := make([]string, 0, len(notes))
	for _, note := range notes {
		trimmed := strings.TrimSpace(note)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	if len(lines) == 0 {
		return output
	}
	prefix := strings.Join(lines, "\n")
	if prefix != "" {
		prefix += "\n"
	}
	return append([]byte(prefix), output...)
}

func normalizeWorkerAssistCommand(command string, args []string) (string, []string) {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	if command == "" {
		return "", args
	}
	parts := strings.Fields(command)
	if len(parts) > 1 && len(args) == 0 {
		command = parts[0]
		args = parts[1:]
	}
	base := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	if base == "bash" || base == "sh" || base == "zsh" {
		if normalized, rewritten := normalizeSingleArgShellCommand(args); rewritten {
			return command, normalized
		}
	}
	return command, args
}

func normalizeSingleArgShellCommand(args []string) ([]string, bool) {
	if len(args) != 1 {
		return args, false
	}
	payload := strings.TrimSpace(args[0])
	if payload == "" || strings.HasPrefix(payload, "-") {
		return args, false
	}
	if !strings.ContainsAny(payload, " \t\r\n;&|<>`$()") {
		return args, false
	}
	lower := strings.ToLower(payload)
	if strings.Contains(payload, "/") ||
		strings.HasSuffix(lower, ".sh") ||
		strings.HasSuffix(lower, ".bash") ||
		strings.HasSuffix(lower, ".zsh") {
		return args, false
	}
	return []string{"-lc", payload}, true
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
	summary, _ := summarizeCommandResultWithMeta(command, args, runErr, output)
	return summary
}

type workerAssistCommandSummaryMeta struct {
	OutputBytes        int  `json:"output_bytes"`
	OutputByteCapped   bool `json:"output_byte_capped"`
	OutputCharCapped   bool `json:"output_char_capped"`
	OutputWasEmptyText bool `json:"output_was_empty_text"`
}

func summarizeCommandResultWithMeta(command string, args []string, runErr error, output []byte) (string, workerAssistCommandSummaryMeta) {
	joined := strings.TrimSpace(strings.Join(append([]string{strings.TrimSpace(command)}, args...), " "))
	if joined == "" {
		joined = "(command)"
	}
	outputSummary, meta := summarizeOutputWithMeta(output)
	if runErr != nil {
		return fmt.Sprintf("command failed: %s | %v | output: %s", joined, runErr, outputSummary), meta
	}
	return fmt.Sprintf("command ok: %s | output: %s", joined, outputSummary), meta
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
	text, _ := summarizeOutputWithMeta(output)
	return text
}

func summarizeOutputWithMeta(output []byte) (string, workerAssistCommandSummaryMeta) {
	meta := workerAssistCommandSummaryMeta{
		OutputBytes:      len(output),
		OutputByteCapped: len(output) > 800,
	}
	text := strings.TrimSpace(string(capBytes(output, 800)))
	if text == "" {
		meta.OutputWasEmptyText = true
		return "(no output)", meta
	}
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 300 {
		meta.OutputCharCapped = true
		return text[:300] + "...", meta
	}
	return text, meta
}
