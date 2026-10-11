package workergoal

import "testing"

func TestParseDirectEvaluation(t *testing.T) {
	got, err := Parse(`{"status":"satisfied","reason":"file listing is complete","summary":"listed files"}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Status != StatusSatisfied || got.Summary != "listed files" {
		t.Fatalf("unexpected eval: %#v", got)
	}
}

func TestParseEvaluationPreservesStructuredLeadHints(t *testing.T) {
	got, err := Parse(`{"status":"satisfied","reason":"observation complete","lead_hints":["A recorded lead",{"hint":"A second recorded lead"},{"lead":"A third recorded lead"}]}`)
	if err != nil || len(got.LeadHints) != 3 || got.LeadHints[0] != "A recorded lead" || got.LeadHints[1] != "A second recorded lead" || got.LeadHints[2] != "A third recorded lead" {
		t.Fatalf("lead handoff lost during structured parsing: %+v err=%v", got.LeadHints, err)
	}
}

func TestValidateDirectEvaluation(t *testing.T) {
	report := Validate(Evaluation{Status: StatusInProgress, Reason: "need one more action"})
	if !report.Valid() {
		t.Fatalf("Validate() issues = %#v", report.Issues)
	}

	bad := Validate(Evaluation{Status: "weird"})
	if bad.Valid() {
		t.Fatal("expected invalid report")
	}
}
