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
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string
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
	FailureClass  string
}

// Executor runs exact actions and records the result log.
type Executor struct {
	LogDir string
}

// Run executes the action exactly as requested, with minimal shell use only when requested.
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
		StdoutSummary: summarize(stdoutBuf.String()),
		StderrSummary: summarize(stderrBuf.String()),
		LogPath:       logPath,
		ArtifactRefs:  nil,
		FailureClass:  failureClass,
	}
	return result, runErr
}

func buildCommand(ctx context.Context, action Action) *exec.Cmd {
	if action.UseShell {
		full := strings.TrimSpace(strings.Join(append([]string{action.Command}, action.Args...), " "))
		return exec.CommandContext(ctx, "/bin/sh", "-lc", full)
	}
	return exec.CommandContext(ctx, action.Command, action.Args...)
}

func executionMode(action Action) string {
	if action.UseShell {
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
