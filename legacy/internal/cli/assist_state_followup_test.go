package cli

import "testing"

func TestShouldUseDefaultChoiceMatchesQuestionContext(t *testing.T) {
	t.Parallel()

	if !shouldUseDefaultChoice("go ahead with default", "Should I use the default wordlist option?") {
		t.Fatalf("expected default-choice detection to match")
	}
	if shouldUseDefaultChoice("go ahead", "What target host should we scan?") {
		t.Fatalf("did not expect default-choice detection for non-option question")
	}
}

func TestLooksLikePathOrFilenameDetectsCommonReportOutputs(t *testing.T) {
	t.Parallel()

	if !looksLikePathOrFilename("findings.md") {
		t.Fatalf("expected markdown filename to be treated as path-like")
	}
	if looksLikePathOrFilename("just words no path") {
		t.Fatalf("did not expect plain phrase to be treated as path-like")
	}
}
