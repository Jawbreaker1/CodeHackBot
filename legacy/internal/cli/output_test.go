package cli

import "testing"

func TestNormalizeConsoleNewlines(t *testing.T) {
	in := "line1\rline2\r\nline3\n"
	got := normalizeConsoleNewlines(in)
	want := "line1\r\nline2\r\nline3\r\n"
	if got != want {
		t.Fatalf("normalizeConsoleNewlines mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestNormalizeConsoleNewlinesEmpty(t *testing.T) {
	if got := normalizeConsoleNewlines(""); got != "" {
		t.Fatalf("expected empty output, got %q", got)
	}
}
