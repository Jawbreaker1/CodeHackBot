package replay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
)

func TestReplayRunSteps(t *testing.T) {
	temp := t.TempDir()
	sessionDir := filepath.Join(temp, "session-1")
	if err := os.MkdirAll(filepath.Join(sessionDir, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	replayPath := filepath.Join(sessionDir, "replay.txt")
	if err := os.WriteFile(replayPath, []byte("echo hello\n"), 0o644); err != nil {
		t.Fatalf("write replay: %v", err)
	}

	steps, err := LoadSteps(replayPath)
	if err != nil {
		t.Fatalf("LoadSteps error: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}

	runner := exec.Runner{
		Permissions:     exec.PermissionAll,
		RequireApproval: false,
		LogDir:          filepath.Join(sessionDir, "logs"),
	}
	results, err := RunSteps(runner, steps)
	if err != nil {
		t.Fatalf("RunSteps error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].LogPath == "" {
		t.Fatalf("expected log path")
	}
	if _, err := os.Stat(results[0].LogPath); err != nil {
		t.Fatalf("log file missing: %v", err)
	}
}
