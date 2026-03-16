package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Action is the minimal executable action for the rebuild root.
type Action struct {
	Command  string
	Args     []string
	Cwd      string
	Env      map[string]string
	UseShell bool
}

// Result is the structured execution result used as canonical truth.
type Result struct {
	Action        string
	ExecutionMode string
	Cwd           string
	EnvDelta      map[string]string
	StartedAt     time.Time
	FinishedAt    time.Time
	ExitStatus    int
	StdoutSummary string
	StderrSummary string
	LogPath       string
	ArtifactRefs  []string
	Assessment    string
	Signals       []string
	FailureClass  string
}

// Executor runs exact actions and records the result log.
type Executor struct {
	LogDir string
}

// Run executes the action exactly as requested, with minimal shell use only when needed.
func (e Executor) Run(ctx context.Context, action Action) (Result, error) {
	if strings.TrimSpace(action.Command) == "" {
		return Result{}, fmt.Errorf("command is required")
	}
	if strings.TrimSpace(e.LogDir) == "" {
		return Result{}, fmt.Errorf("log dir is required")
	}
	if err := os.MkdirAll(e.LogDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create log dir: %w", err)
	}

	started := time.Now().UTC()
	logPath := filepath.Join(e.LogDir, fmt.Sprintf("cmd-%s.log", started.Format("20060102-150405.000000000")))

	cmd := buildCommand(ctx, action)
	if strings.TrimSpace(action.Cwd) != "" {
		cmd.Dir = action.Cwd
	}
	cmd.Env = append(os.Environ(), flattenEnv(action.Env)...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	finished := time.Now().UTC()
	exitStatus := exitCode(runErr)
	failureClass := ""
	if runErr != nil {
		failureClass = "command_failed"
	}
	stdoutSummary := summarize(stdoutBuf.String())
	stderrSummary := summarize(stderrBuf.String())
	assessment, signals := assessResult(exitStatus, stdoutSummary, stderrSummary)

	if err := writeLog(logPath, action, started, finished, exitStatus, stdoutBuf.String(), stderrBuf.String()); err != nil {
		return Result{}, fmt.Errorf("write log: %w", err)
	}

	result := Result{
		Action:        renderAction(action),
		ExecutionMode: executionMode(action),
		Cwd:           action.Cwd,
		EnvDelta:      cloneEnv(action.Env),
		StartedAt:     started,
		FinishedAt:    finished,
		ExitStatus:    exitStatus,
		StdoutSummary: stdoutSummary,
		StderrSummary: stderrSummary,
		LogPath:       logPath,
		ArtifactRefs:  nil,
		Assessment:    assessment,
		Signals:       signals,
		FailureClass:  failureClass,
	}
	return result, runErr
}

func buildCommand(ctx context.Context, action Action) *exec.Cmd {
	if needsShell(action) {
		full := strings.TrimSpace(strings.Join(append([]string{action.Command}, action.Args...), " "))
		return exec.CommandContext(ctx, "/bin/sh", "-lc", full)
	}
	return exec.CommandContext(ctx, action.Command, action.Args...)
}

func executionMode(action Action) string {
	if needsShell(action) {
		return "shell"
	}
	return "argv"
}

func renderAction(action Action) string {
	if action.UseShell {
		return strings.TrimSpace(strings.Join(append([]string{action.Command}, action.Args...), " "))
	}
	return strings.TrimSpace(strings.Join(append([]string{action.Command}, action.Args...), " "))
}

func needsShell(action Action) bool {
	if action.UseShell {
		return true
	}
	return commandNeedsShell(strings.TrimSpace(strings.Join(append([]string{action.Command}, action.Args...), " ")))
}

func commandNeedsShell(command string) bool {
	if strings.TrimSpace(command) == "" {
		return false
	}
	for _, marker := range []string{
		"||",
		"&&",
		"|",
		";",
		">",
		"<",
		"$(",
		"`",
		"*",
		"?",
	} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
}

func flattenEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func cloneEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

func summarize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(none)"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 5 {
		lines = lines[:5]
	}
	return strings.Join(lines, "\n")
}

func assessResult(exitStatus int, stdoutSummary, stderrSummary string) (string, []string) {
	signals := make([]string, 0, 4)
	combined := strings.ToLower(strings.TrimSpace(strings.Join([]string{stdoutSummary, stderrSummary}, "\n")))

	if exitStatus != 0 {
		signals = append(signals, "nonzero_exit")
	}
	if strings.TrimSpace(stdoutSummary) == "" || strings.TrimSpace(stdoutSummary) == "(none)" {
		if strings.TrimSpace(stderrSummary) == "" || strings.TrimSpace(stderrSummary) == "(none)" {
			signals = append(signals, "empty_output")
		}
	}
	for _, marker := range []struct {
		phrase string
		signal string
	}{
		{"permission denied", "permission_denied"},
		{"no such file or directory", "missing_path"},
		{"cannot find", "missing_path"},
		{"not found", "not_found_text"},
		{"incorrect password", "incorrect_password"},
		{"syntax error", "syntax_error"},
		{"usage:", "usage_text"},
		{"failed", "failure_text"},
		{"error", "error_text"},
	} {
		if strings.Contains(combined, marker.phrase) {
			signals = appendSignal(signals, marker.signal)
		}
	}

	switch {
	case exitStatus != 0:
		return "failed", signals
	case hasSignal(signals, "permission_denied") || hasSignal(signals, "missing_path") || hasSignal(signals, "incorrect_password") || hasSignal(signals, "syntax_error") || hasSignal(signals, "failure_text") || hasSignal(signals, "error_text"):
		return "suspicious", signals
	case hasSignal(signals, "empty_output") || hasSignal(signals, "usage_text"):
		return "ambiguous", signals
	default:
		return "success", signals
	}
}

func appendSignal(signals []string, signal string) []string {
	if hasSignal(signals, signal) {
		return signals
	}
	return append(signals, signal)
}

func hasSignal(signals []string, want string) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}

func writeLog(path string, action Action, started, finished time.Time, exitStatus int, stdout, stderr string) error {
	content := strings.Join([]string{
		"action: " + renderAction(action),
		"execution_mode: " + executionMode(action),
		"cwd: " + blankOrNone(action.Cwd),
		"started_at: " + started.Format(time.RFC3339Nano),
		"finished_at: " + finished.Format(time.RFC3339Nano),
		fmt.Sprintf("exit_status: %d", exitStatus),
		"",
		"[stdout]",
		strings.TrimRight(stdout, "\n"),
		"",
		"[stderr]",
		strings.TrimRight(stderr, "\n"),
		"",
	}, "\n")
	return os.WriteFile(path, []byte(content), 0o644)
}

func blankOrNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(none)"
	}
	return v
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errorsAs(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// errorsAs is a small indirection to keep tests simple and explicit.
var errorsAs = func(err error, target any) bool {
	return errors.As(err, target)
}
