package cli

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestFragileShellPipelineBlockedInAssist(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-pipe", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	s := assist.Suggestion{
		Type:    "command",
		Command: "bash",
		Args:    []string{"-lc", "curl -s https://example.com | grep -i server"},
	}
	if err := r.executeAssistSuggestion(s, true); err == nil {
		t.Fatalf("expected pipeline to be blocked")
	}
}
