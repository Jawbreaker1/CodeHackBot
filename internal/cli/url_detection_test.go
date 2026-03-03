package cli

import "testing"

func TestExtractFirstURLDoesNotTreatZipAsURL(t *testing.T) {
	goal := "Please crack the password protected file secret.zip and tell me what is inside."
	if got := extractFirstURL(goal); got != "" {
		t.Fatalf("expected no URL for local file goal, got %q", got)
	}
	if shouldAutoBrowse(goal) {
		t.Fatalf("expected shouldAutoBrowse=false for local file goal")
	}
}

func TestExtractFirstURLDetectsWebHost(t *testing.T) {
	goal := "Can you go to www.systemverification.com and summarize it?"
	got := extractFirstURL(goal)
	if got != "www.systemverification.com" {
		t.Fatalf("unexpected URL detection: %q", got)
	}
	if !shouldAutoBrowse(goal) {
		t.Fatalf("expected shouldAutoBrowse=true for informational web request")
	}
}

func TestLooksLikeURLOrHostRejectsFilename(t *testing.T) {
	if looksLikeURLOrHost("secret.zip") {
		t.Fatalf("expected filename to be rejected as URL/host")
	}
	if !looksLikeURLOrHost("systemverification.com") {
		t.Fatalf("expected domain to be accepted as URL/host")
	}
}
