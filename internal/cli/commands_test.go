package cli

import (
	"io"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestHandleCommandExitReturnsEOF(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-exit", "", "")

	err := r.handleCommand("/exit")
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}
