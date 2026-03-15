package cli

import "testing"

func TestCanonicalAssistActionKeyTreatsListAliasesAsSameAction(t *testing.T) {
	a := canonicalAssistActionKey("ls", []string{"-la"})
	b := canonicalAssistActionKey("list_dir", []string{"."})
	if a != b {
		t.Fatalf("expected equivalent list actions, got %q vs %q", a, b)
	}
}

func TestCanonicalAssistActionKeyNormalizesBrowseTarget(t *testing.T) {
	a := canonicalAssistActionKey("browse", []string{"example.com"})
	b := canonicalAssistActionKey("browse", []string{"https://example.com"})
	if a != b {
		t.Fatalf("expected equivalent browse actions, got %q vs %q", a, b)
	}
}

func TestGuardAssistCommandLoopBlocksCanonicalRepeats(t *testing.T) {
	r := &Runner{}
	if err := r.guardAssistCommandLoop("ls", []string{"-la"}); err != nil {
		t.Fatalf("first command should pass: %v", err)
	}
	if err := r.guardAssistCommandLoop("list_dir", []string{"."}); err != nil {
		t.Fatalf("second equivalent command should still pass: %v", err)
	}
	if err := r.guardAssistCommandLoop("bash", []string{"-lc", "ls -la"}); err == nil {
		t.Fatalf("expected repeated equivalent command to be blocked")
	}
}
