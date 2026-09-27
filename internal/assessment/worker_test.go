package assessment

import (
	"strings"
	"testing"
)

func TestChallengeHandoffUsesStructuredVerdictOverContradictoryNarrative(t *testing.T) {
	result := &VerificationResult{Verdict: "inconclusive", Reason: "policy intent was not established", AlternativeResult: "legacy use remains plausible"}
	summary := challengeSummary(result, "Challenge verdict: SUPPORTED. The issue is confirmed.")
	if !strings.Contains(summary, "Challenge verdict: inconclusive") || strings.Contains(summary, "SUPPORTED") || !strings.Contains(summary, result.AlternativeResult) {
		t.Fatalf("challenge handoff repeated a contradictory narrative: %q", summary)
	}
	if !strings.Contains(challengeSummary(nil, "reported success"), "requires review") {
		t.Fatal("missing structured verdict was presented without a review warning")
	}
}
