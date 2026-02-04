package exec

import (
	"bufio"
	"bytes"
	"context"
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

func TestRunCommandStreaming(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: echo may not be available as an external command")
	}
	var live bytes.Buffer
	temp := t.TempDir()
	runner := Runner{
		Permissions: PermissionAll,
		LogDir:      temp,
		LiveWriter:  &live,
	}
	result, err := runner.RunCommand("echo", "hello")
	if err != nil {
		t.Fatalf("RunCommand error: %v", err)
	}
	if !strings.Contains(live.String(), "hello") {
		t.Fatalf("expected live output, got %q", live.String())
	}
	if !strings.Contains(result.Output, "hello") {
		t.Fatalf("expected result output, got %q", result.Output)
	}
}

func TestRunCommandWithContextCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: sleep may not be available")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := Runner{
		Permissions: PermissionAll,
		Timeout:     time.Second,
	}
	_, err := runner.RunCommandWithContext(ctx, "sleep", "1")
	if err == nil {
		t.Fatalf("expected canceled error")
	}
}
