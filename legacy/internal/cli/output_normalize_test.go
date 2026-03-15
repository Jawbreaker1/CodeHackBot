package cli

import (
	"strings"
	"testing"
)

func TestNormalizeAssistantOutput(t *testing.T) {
	in := "  line1\rline2\r\nline3\n"
	got := normalizeAssistantOutput(in)
	want := "line1\nline2\nline3"
	if got != want {
		t.Fatalf("normalizeAssistantOutput mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestWrapTextForTerminal(t *testing.T) {
	in := "one two three four five six"
	got := wrapTextForTerminal(in, 10)
	want := "one two\nthree\nfour five\nsix"
	if got != want {
		t.Fatalf("wrap mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestNormalizeAssistantOutputRemovesANSI(t *testing.T) {
	in := "\x1b[31mhello\x1b[0m"
	got := normalizeAssistantOutput(in)
	if got != "hello" {
		t.Fatalf("expected ansi stripped output, got %q", got)
	}
}

func TestNormalizeAssistantOutputStripsReasoningTail(t *testing.T) {
	in := "Result: no password recovered.\nThinking Process:\n1. analyze...\n2. explain..."
	got := normalizeAssistantOutput(in)
	if got != "Result: no password recovered." {
		t.Fatalf("expected reasoning tail stripped, got %q", got)
	}
}

func TestNormalizeAssistantOutputStripsInlineReasoningMarker(t *testing.T) {
	in := "Status update. Thinking Process: step by step hidden"
	got := normalizeAssistantOutput(in)
	if got != "Status update." {
		t.Fatalf("expected inline reasoning marker stripped, got %q", got)
	}
}

func TestNormalizeAssistantOutputStripsThinkTags(t *testing.T) {
	in := "<think>private chain of thought</think>\nFinal answer here."
	got := normalizeAssistantOutput(in)
	if got != "Final answer here." {
		t.Fatalf("expected think block stripped, got %q", got)
	}
}

func TestNormalizeAssistantOutputDoesNotBlankMarkerLeadingText(t *testing.T) {
	in := "Thinking Process: long reasoning without clean delimiter"
	got := normalizeAssistantOutput(in)
	if got == "" {
		t.Fatalf("expected non-empty output")
	}
	if got == in {
		t.Fatalf("expected reasoning text to be sanitized, got %q", got)
	}
	if got == "Thinking Process: long reasoning without clean delimiter" {
		t.Fatalf("reasoning marker leaked to output: %q", got)
	}
}

func TestNormalizeAssistantOutputExtractsSummaryFromReasoningLeak(t *testing.T) {
	in := "Thinking Process:\n1) analyze\n2) plan\n\n*Summary:* cracked using john\n*Findings:* password recovered"
	got := normalizeAssistantOutput(in)
	if got == "" {
		t.Fatalf("expected extracted user-facing section")
	}
	if containsIgnoreCase(got, "thinking process") {
		t.Fatalf("reasoning marker leaked: %q", got)
	}
	if !containsIgnoreCase(got, "cracked using john") || !containsIgnoreCase(got, "password recovered") {
		t.Fatalf("expected extracted findings, got: %q", got)
	}
}

func TestNormalizeAssistantOutputStripsStructuredReasoningOutline(t *testing.T) {
	in := `Summary: Password recovered.
5.  **Refine based on Constraints:**
    * Keep only provided artifacts.
6.  **Final Review against Constraints:**
    * No extra assumptions.
Possible next steps:
1) Extract file
2) Report findings`
	got := normalizeAssistantOutput(in)
	if containsIgnoreCase(got, "refine based on constraints") || containsIgnoreCase(got, "final review against constraints") {
		t.Fatalf("structured reasoning leaked: %q", got)
	}
	if !containsIgnoreCase(got, "Password recovered") {
		t.Fatalf("expected summary to remain, got %q", got)
	}
}

func containsIgnoreCase(s, needle string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(needle))
}
