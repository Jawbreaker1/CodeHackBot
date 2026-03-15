package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func TestExtractOutOfScopeTarget(t *testing.T) {
	msg := "scope violation: out of scope target www.systemverification.com; denied target 8.8.8.8"
	got := extractOutOfScopeTarget(msg)
	if got != "www.systemverification.com" {
		t.Fatalf("unexpected target: %q", got)
	}
}

func TestAssistFailureHintScopeViolationIncludesScopeCommand(t *testing.T) {
	s := assist.Suggestion{Type: "command", Command: "nmap"}
	err := fmt.Errorf("scope violation: out of scope target www.systemverification.com")
	hint := assistFailureHint(s, commandError{Err: err})
	if !strings.Contains(hint, "/scope add-target www.systemverification.com") {
		t.Fatalf("expected scope guidance hint, got: %q", hint)
	}
}

