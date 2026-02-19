package orchestrator

import "testing"

func TestGenerateHypotheses_BoundedAndRanked(t *testing.T) {
	t.Parallel()

	goal := "scan network services and review web auth session controls"
	scope := Scope{
		Targets: []string{"192.168.50.10"},
	}
	out := GenerateHypotheses(goal, scope, 3)
	if len(out) != 3 {
		t.Fatalf("expected 3 hypotheses, got %d", len(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].Score < out[i].Score {
			t.Fatalf("hypotheses not sorted by score desc: %d < %d", out[i-1].Score, out[i].Score)
		}
	}
}

func TestGenerateHypotheses_MapsSignalsAndEvidence(t *testing.T) {
	t.Parallel()

	out := GenerateHypotheses("inspect web application login", Scope{Targets: []string{"app.internal"}}, 5)
	if len(out) == 0 {
		t.Fatalf("expected hypotheses")
	}
	found := false
	for _, hypothesis := range out {
		if len(hypothesis.SuccessSignals) == 0 || len(hypothesis.FailSignals) == 0 || len(hypothesis.EvidenceRequired) == 0 {
			t.Fatalf("expected signals/evidence for hypothesis %s", hypothesis.ID)
		}
		if hypothesis.Statement == "" {
			t.Fatalf("empty statement for hypothesis %s", hypothesis.ID)
		}
		if hypothesis.Confidence != "" && hypothesis.Impact != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected confidence and impact values")
	}
}

func TestGenerateHypotheses_FallbackWhenNoKeywordMatch(t *testing.T) {
	t.Parallel()

	out := GenerateHypotheses("inventory the target", Scope{Networks: []string{"192.168.50.0/24"}}, 5)
	if len(out) == 0 {
		t.Fatalf("expected at least fallback hypothesis")
	}
	if out[0].Statement == "" {
		t.Fatalf("expected non-empty fallback statement")
	}
}
