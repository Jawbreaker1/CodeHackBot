package workerdirect

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
