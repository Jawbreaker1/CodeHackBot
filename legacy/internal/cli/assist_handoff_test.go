package cli

import (
	"strings"
	"testing"
)

func TestFormatAssistQuestionForUser_WithNumberedChoices(t *testing.T) {
	question := "Would you like me to: (1) Try a wordlist attack, or (2) Check another archive path?"
	out := formatAssistQuestionForUser(question, "Need a direction before running another attempt.")
	if !strings.Contains(out, "Input required:") {
		t.Fatalf("expected input-required preface, got %q", out)
	}
	if !strings.Contains(out, "1. Try a wordlist attack") {
		t.Fatalf("expected option 1 in output, got %q", out)
	}
	if !strings.Contains(out, "2. Check another archive path?") {
		t.Fatalf("expected option 2 in output, got %q", out)
	}
	if !strings.Contains(out, "Reply with the option number") {
		t.Fatalf("expected reply hint, got %q", out)
	}
}

func TestFormatAssistQuestionForUser_NoChoices(t *testing.T) {
	question := "Which file path should I use?"
	out := formatAssistQuestionForUser(question, "")
	if !strings.Contains(out, "Input required:") {
		t.Fatalf("expected input-required preface, got %q", out)
	}
	if !strings.Contains(out, question) {
		t.Fatalf("expected raw question retained, got %q", out)
	}
	if strings.Contains(out, "Options:") {
		t.Fatalf("did not expect options block, got %q", out)
	}
}
