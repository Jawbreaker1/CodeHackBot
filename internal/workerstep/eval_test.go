package workerstep

import "testing"

func TestParseEvaluation(t *testing.T) {
	got, err := Parse(`{"status":"satisfied","reason":"archive metadata collected","summary":"metadata verified"}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Status != StatusSatisfied {
		t.Fatalf("status = %q", got.Status)
	}
	if got.Reason != "archive metadata collected" {
		t.Fatalf("reason = %q", got.Reason)
	}
	if got.Summary != "metadata verified" {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestValidateEvaluation(t *testing.T) {
	if report := Validate(Evaluation{Status: StatusInProgress, Reason: "need more evidence"}); !report.Valid() {
		t.Fatalf("valid report unexpectedly invalid: %#v", report)
	}
	report := Validate(Evaluation{Status: "weird", Reason: ""})
	if report.Valid() {
		t.Fatalf("invalid report unexpectedly valid")
	}
	if len(report.Issues) != 2 {
		t.Fatalf("issue count = %d", len(report.Issues))
	}
}
