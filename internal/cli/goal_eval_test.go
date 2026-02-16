package cli

import "testing"

func TestParseGoalEvalResponseExtractsWrappedJSON(t *testing.T) {
	raw := "analysis...\n{\"done\":true,\"answer\":\"ok\",\"confidence\":\"high\"}\n"
	got, err := parseGoalEvalResponse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !got.Done {
		t.Fatalf("expected done=true")
	}
	if got.Answer != "ok" {
		t.Fatalf("unexpected answer: %q", got.Answer)
	}
}

func TestParseGoalEvalResponseNormalizesConfidence(t *testing.T) {
	raw := "{\"done\":false,\"answer\":\"\",\"confidence\":\"VERY\"}"
	got, err := parseGoalEvalResponse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got.Confidence != "low" {
		t.Fatalf("expected low confidence fallback, got %q", got.Confidence)
	}
}
