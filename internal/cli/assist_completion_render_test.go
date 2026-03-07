package cli

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func TestRenderCompletionMessageFallsBackWhenInternalLeakDetected(t *testing.T) {
	trueVal := true
	s := assist.Suggestion{
		ObjectiveMet: &trueVal,
		WhyMet:       "Password recovered and file content verified in evidence logs.",
	}
	leaky := `Summary:** Mentions recent actions and tools.
Task Foundation: Goal marked met from prior context.
Analyze the request: ...`
	got := renderCompletionMessage(s, leaky)
	if strings.Contains(strings.ToLower(got), "analyze the request") || strings.Contains(strings.ToLower(got), "task foundation") {
		t.Fatalf("expected internal leak removed, got %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "password recovered") {
		t.Fatalf("expected why_met fallback, got %q", got)
	}
}

func TestRenderCompletionMessageKeepsValidFinal(t *testing.T) {
	trueVal := true
	s := assist.Suggestion{
		ObjectiveMet: &trueVal,
		WhyMet:       "evidence verified",
	}
	final := "Successfully cracked the archive and extracted secret_text.txt."
	got := renderCompletionMessage(s, final)
	if got != final {
		t.Fatalf("expected unchanged final message, got %q", got)
	}
}

