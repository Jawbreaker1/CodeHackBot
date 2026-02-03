package exec

import (
	"bufio"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCommandApprovalDenied(t *testing.T) {
	runner := Runner{
		Permissions:     PermissionDefault,
		RequireApproval: true,
		Reader:          bufio.NewReader(strings.NewReader("n\n")),
		Timeout:         time.Second,
	}
	_, err := runner.RunCommand("echo", "hi")
	if err == nil {
		t.Fatalf("expected approval error")
	}
}

func TestRunCommandReadonly(t *testing.T) {
	runner := Runner{
		Permissions: PermissionReadOnly,
	}
	_, err := runner.RunCommand("echo", "hi")
	if err == nil {
		t.Fatalf("expected readonly error")
	}
}

func TestRunCommandWritesLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: echo may not be available as an external command")
	}
	temp := t.TempDir()
	fixed := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	runner := Runner{
		Permissions: PermissionAll,
		LogDir:      temp,
		Now:         func() time.Time { return fixed },
	}
	result, err := runner.RunCommand("echo", "hello")
	if err != nil {
		t.Fatalf("RunCommand error: %v", err)
	}
	if result.LogPath == "" {
		t.Fatalf("expected log path")
	}
	data, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "$ echo hello") {
		t.Fatalf("missing command in log")
	}
	if !strings.Contains(content, "hello") {
		t.Fatalf("missing output in log")
	}
}
