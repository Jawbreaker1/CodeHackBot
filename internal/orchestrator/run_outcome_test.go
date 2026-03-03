package orchestrator

import "testing"

func TestNormalizeRunOutcome(t *testing.T) {
	tests := []struct {
		in   string
		want RunOutcome
	}{
		{in: "success", want: RunOutcomeSuccess},
		{in: "FAILED", want: RunOutcomeFailed},
		{in: " aborted ", want: RunOutcomeAborted},
		{in: "partial", want: RunOutcomePartial},
		{in: "unknown", want: ""},
	}
	for _, tc := range tests {
		if got := NormalizeRunOutcome(tc.in); got != tc.want {
			t.Fatalf("NormalizeRunOutcome(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateAndParseRunOutcome(t *testing.T) {
	valid := []string{"success", "failed", "aborted", "partial"}
	for _, raw := range valid {
		if err := ValidateRunOutcome(raw); err != nil {
			t.Fatalf("ValidateRunOutcome(%q): %v", raw, err)
		}
		outcome, err := ParseRunOutcome(raw)
		if err != nil {
			t.Fatalf("ParseRunOutcome(%q): %v", raw, err)
		}
		if string(outcome) != raw {
			t.Fatalf("ParseRunOutcome(%q)=%q", raw, outcome)
		}
	}
	if err := ValidateRunOutcome("n/a"); err == nil {
		t.Fatalf("expected ValidateRunOutcome to fail for invalid input")
	}
	if _, err := ParseRunOutcome("n/a"); err == nil {
		t.Fatalf("expected ParseRunOutcome to fail for invalid input")
	}
}
