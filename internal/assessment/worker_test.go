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

func TestParallelWorkerGetsDistinctSiblingBoundary(t *testing.T) {
	plan := Decision{
		Tasks: []Task{
			{ID: "source", Goal: "Trace the local service implementation"},
			{ID: "behavior", Goal: "Request the local service and record its response"},
			{ID: "omitted", Goal: "Inspect unrelated fixture state"},
		},
		ApprovedTaskIDs: []string{"source", "behavior"},
		SkippedTaskIDs:  []string{"omitted"},
	}
	boundaries := strings.Join(siblingTaskBoundaries(plan, "source"), "\n")
	if !strings.Contains(boundaries, "Parallel worker \"behavior\" owns") || !strings.Contains(boundaries, "avoid repeating") || !strings.Contains(boundaries, "did not select sibling task \"omitted\"") || strings.Contains(boundaries, "Parallel worker \"source\"") {
		t.Fatalf("worker boundaries did not separate selected and skipped work: %s", boundaries)
	}
}
