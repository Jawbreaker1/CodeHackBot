package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func TestChatHistoryAppendAndRead(t *testing.T) {
	cfg := config.Config{}
	cfg.Context.ChatHistoryLines = 3
	cfg.Session.LogDir = t.TempDir()
	runner := NewRunner(cfg, "session-1", "", "")

	sessionDir, err := runner.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold error: %v", err)
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		t.Fatalf("EnsureArtifacts error: %v", err)
	}

	runner.appendChatHistory(artifacts.ChatPath, "User", "hello")
	runner.appendChatHistory(artifacts.ChatPath, "Assistant", "hi there")
	runner.appendChatHistory(artifacts.ChatPath, "User", "my name is johan")
	runner.appendChatHistory(artifacts.ChatPath, "Assistant", "nice to meet you")

	history := runner.readChatHistory(artifacts.ChatPath)
	lines := strings.Split(strings.TrimSpace(history), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), history)
	}
	if !strings.HasPrefix(lines[0], "Assistant:") || !strings.Contains(lines[2], "Assistant:") {
		t.Fatalf("unexpected history ordering: %q", history)
	}
	if !strings.Contains(history, "nice to meet you") {
		t.Fatalf("expected last assistant line, got %q", history)
	}

	if !strings.HasSuffix(artifacts.ChatPath, filepath.Join("session-1", memory.ChatFilename)) {
		t.Fatalf("unexpected chat path: %s", artifacts.ChatPath)
	}
}
