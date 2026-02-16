package cli

import "testing"

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
