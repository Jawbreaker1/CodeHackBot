package contextstats

import (
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestApproxTokens(t *testing.T) {
	if got := ApproxTokens(""); got != 0 {
		t.Fatalf("ApproxTokens(empty) = %d", got)
	}
	if got := ApproxTokens("abcd"); got != 1 {
		t.Fatalf("ApproxTokens(4 chars) = %d", got)
	}
	if got := ApproxTokens("abcde"); got != 2 {
		t.Fatalf("ApproxTokens(5 chars) = %d", got)
	}
}

func TestBuild(t *testing.T) {
	rendered := "[a]\nhello\n\n[b]\nworld\n"
	sections := []ctxpacket.RenderedSection{
		{Name: "a", Content: "hello"},
		{Name: "b", Content: "world"},
	}
	got := Build(rendered, sections)
	if got.TotalChars != len(rendered) {
		t.Fatalf("TotalChars = %d, want %d", got.TotalChars, len(rendered))
	}
	if got.ApproxTotalTokens != ApproxTokens(rendered) {
		t.Fatalf("ApproxTotalTokens = %d, want %d", got.ApproxTotalTokens, ApproxTokens(rendered))
	}
	if got.SectionCount != 2 {
		t.Fatalf("SectionCount = %d, want 2", got.SectionCount)
	}
	if len(got.Sections) != 2 || got.Sections[0].ApproxTokens == 0 {
		t.Fatalf("Sections = %#v", got.Sections)
	}
}
