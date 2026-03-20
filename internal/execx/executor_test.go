package execx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutorRunArgv(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command: "printf",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExecutionMode != "argv" {
		t.Fatalf("ExecutionMode = %q", result.ExecutionMode)
	}
	if result.ExitStatus != 0 {
		t.Fatalf("ExitStatus = %d", result.ExitStatus)
	}
	if !strings.Contains(result.StdoutSummary, "hello") {
		t.Fatalf("StdoutSummary = %q", result.StdoutSummary)
	}
	if _, err := os.Stat(result.LogPath); err != nil {
		t.Fatalf("log file missing: %v", err)
	}
}

func TestExecutorRunShell(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command:  "printf shell-test > shell.txt",
		Cwd:      logDir,
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExecutionMode != "shell" {
		t.Fatalf("ExecutionMode = %q", result.ExecutionMode)
	}
	content, err := os.ReadFile(filepath.Join(logDir, "shell.txt"))
	if err != nil {
		t.Fatalf("read shell.txt: %v", err)
	}
	if string(content) != "shell-test" {
		t.Fatalf("shell.txt = %q", string(content))
	}
}

func TestExecutorAutoDetectsShellSyntax(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command: "printf auto-shell > auto.txt; cat auto.txt",
		Cwd:     logDir,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExecutionMode != "shell" {
		t.Fatalf("ExecutionMode = %q", result.ExecutionMode)
	}
	if !strings.Contains(result.StdoutSummary, "auto-shell") {
		t.Fatalf("StdoutSummary = %q", result.StdoutSummary)
	}
	content, err := os.ReadFile(filepath.Join(logDir, "auto.txt"))
	if err != nil {
		t.Fatalf("read auto.txt: %v", err)
	}
	if string(content) != "auto-shell" {
		t.Fatalf("auto.txt = %q", string(content))
	}
}

func TestExecutorRunFailureClassification(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command: "sh",
		Args:    []string{"-c", "exit 7"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if result.ExitStatus != 7 {
		t.Fatalf("ExitStatus = %d", result.ExitStatus)
	}
	if result.FailureClass != "command_failed" {
		t.Fatalf("FailureClass = %q", result.FailureClass)
	}
	if result.Assessment != "failed" {
		t.Fatalf("Assessment = %q", result.Assessment)
	}
}

func TestExecutorRunSuspiciousZeroExit(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command: "sh",
		Args:    []string{"-c", "echo incorrect password"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Assessment != "suspicious" {
		t.Fatalf("Assessment = %q", result.Assessment)
	}
	if !strings.Contains(strings.Join(result.Signals, ","), "incorrect_password") {
		t.Fatalf("Signals = %#v", result.Signals)
	}
}

func TestExecutorRunAmbiguousEmptyOutput(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command: "sh",
		Args:    []string{"-c", ":"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Assessment != "ambiguous" {
		t.Fatalf("Assessment = %q", result.Assessment)
	}
	if !strings.Contains(strings.Join(result.Signals, ","), "empty_output") {
		t.Fatalf("Signals = %#v", result.Signals)
	}
}

func TestExecutorRunSuspiciousReportedNonzeroExit(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command:  `printf "Exit: 2\n"; :`,
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Assessment != "suspicious" {
		t.Fatalf("Assessment = %q", result.Assessment)
	}
	if !strings.Contains(strings.Join(result.Signals, ","), "reported_nonzero_exit") {
		t.Fatalf("Signals = %#v", result.Signals)
	}
}

func TestExecutorRunAmbiguousNoEffect(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command:  `printf "No password hashes loaded (see FAQ)\n"`,
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Assessment != "ambiguous" {
		t.Fatalf("Assessment = %q", result.Assessment)
	}
	if !strings.Contains(strings.Join(result.Signals, ","), "no_effect") {
		t.Fatalf("Signals = %#v", result.Signals)
	}
}

func TestExecutorRunAmbiguousWarningText(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command:  `printf "caution: filename not matched\n"`,
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Assessment != "ambiguous" {
		t.Fatalf("Assessment = %q", result.Assessment)
	}
	if !strings.Contains(strings.Join(result.Signals, ","), "warning_text") {
		t.Fatalf("Signals = %#v", result.Signals)
	}
}

func TestExecutorRunSuspiciousUnableToGetPassword(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command:  `printf "skipping: treasure-note.txt       unable to get password\n"`,
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Assessment != "suspicious" {
		t.Fatalf("Assessment = %q", result.Assessment)
	}
	if !strings.Contains(strings.Join(result.Signals, ","), "incorrect_password") {
		t.Fatalf("Signals = %#v", result.Signals)
	}
}
