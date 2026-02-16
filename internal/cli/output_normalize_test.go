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
