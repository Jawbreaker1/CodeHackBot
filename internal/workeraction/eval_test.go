package workeraction

import "testing"

func TestParseReview(t *testing.T) {
	got, err := Parse(`{"decision":"revise","reason":"action is broader than the active step"}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Decision != DecisionRevise {
		t.Fatalf("decision = %q", got.Decision)
	}
	if got.Reason != "action is broader than the active step" {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestValidateReview(t *testing.T) {
	if report := Validate(Review{Decision: DecisionExecute, Reason: "fits active step"}); !report.Valid() {
		t.Fatalf("valid report unexpectedly invalid: %#v", report)
	}
	report := Validate(Review{Decision: "maybe", Reason: ""})
	if report.Valid() {
		t.Fatalf("invalid report unexpectedly valid")
	}
	if len(report.Issues) != 2 {
		t.Fatalf("issue count = %d", len(report.Issues))
	}
}
