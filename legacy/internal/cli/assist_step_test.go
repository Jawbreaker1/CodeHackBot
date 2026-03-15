package cli

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func TestAssistStepDescriptionPrefersSummary(t *testing.T) {
	got := assistStepDescription(assist.Suggestion{
		Type:    "command",
		Command: "nmap",
		Args:    []string{"-sV", "10.0.0.5"},
		Summary: "Collect service/version information from the target.",
	})
	if got == "" || !strings.HasPrefix(got, "Collect ") {
		t.Fatalf("unexpected summary description: %q", got)
	}
}

func TestAssistStepDescriptionCommandFallback(t *testing.T) {
	got := assistStepDescription(assist.Suggestion{
		Type:    "command",
		Command: "ls",
		Args:    []string{"-la"},
	})
	if got != "running: ls -la" {
		t.Fatalf("unexpected command description: %q", got)
	}
}
