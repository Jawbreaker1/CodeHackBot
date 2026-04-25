package execx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if result.ActualExec != "printf hello" {
		t.Fatalf("ActualExec = %q", result.ActualExec)
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
	logData, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	if !strings.Contains(string(logData), "actual_invocation: printf hello") {
		t.Fatalf("log missing actual_invocation:\n%s", string(logData))
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
	if !strings.Contains(result.ActualExec, `/bin/sh -lc "printf shell-test > shell.txt"`) {
		t.Fatalf("ActualExec = %q", result.ActualExec)
	}
	content, err := os.ReadFile(filepath.Join(logDir, "shell.txt"))
	if err != nil {
		t.Fatalf("read shell.txt: %v", err)
	}
	if string(content) != "shell-test" {
		t.Fatalf("shell.txt = %q", string(content))
	}
}

func TestBuildCommandUsesNonInteractiveInput(t *testing.T) {
	cmd := buildCommand(context.Background(), Action{
		Command: "printf hello",
	})
	if cmd.Stdin == nil {
		t.Fatal("expected non-nil stdin to prevent interactive reads")
	}
}

func TestExecutorRunTreatsStdinAsClosed(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	result, err := exec.Run(context.Background(), Action{
		Command: "sh",
		Args:    []string{"-c", `cat >/dev/null; printf stdin-closed`},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(result.StdoutSummary, "stdin-closed") {
		t.Fatalf("StdoutSummary = %q", result.StdoutSummary)
	}
}

func TestExecutorPlanAndInitialLog(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}

	plan, err := exec.Plan(Action{
		Command:  "printf hello > out.txt",
		Cwd:      logDir,
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if err := writeLogStart(plan.LogPath, plan, time.Unix(1, 0).UTC()); err != nil {
		t.Fatalf("writeLogStart() error = %v", err)
	}
	data, err := os.ReadFile(plan.LogPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"action: printf hello > out.txt",
		"actual_invocation: /bin/sh -lc",
		"status: running",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("initial log missing %q in:\n%s", want, text)
		}
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

func TestExecutorRunInterruptedClassification(t *testing.T) {
	logDir := t.TempDir()
	exec := Executor{LogDir: logDir}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	result, err := exec.Run(ctx, Action{
		Command:  "sleep 1",
		UseShell: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if result.FailureClass != "execution_interrupted" {
		t.Fatalf("FailureClass = %q", result.FailureClass)
	}
	if result.Assessment != "ambiguous" {
		t.Fatalf("Assessment = %q", result.Assessment)
	}
	if !strings.Contains(strings.Join(result.Signals, ","), "execution_timeout") {
		t.Fatalf("Signals = %#v", result.Signals)
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
