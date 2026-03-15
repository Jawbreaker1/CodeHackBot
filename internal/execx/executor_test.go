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
		Command: "printf shell-test > shell.txt",
		Cwd:     logDir,
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
}
