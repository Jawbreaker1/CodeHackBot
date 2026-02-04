package cli

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestHandleSummarizeReadonly(t *testing.T) {
	cfg := config.Config{}
	cfg.Permissions.Level = "readonly"
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = "http://localhost:1234/v1"
	runner := NewRunner(cfg, "session-1", "", "")
	if err := runner.handleSummarize(nil); err == nil {
		t.Fatalf("expected readonly error")
	}
}
