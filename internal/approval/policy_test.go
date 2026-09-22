package approval

import "testing"

func TestApprovalModes(t *testing.T) {
	low := Request{Summary: "Read the page title", Target: "local fixture", Impact: "Read only", Risk: "low"}
	for _, mode := range []Mode{"", EveryExecution, DangerousOnly, FullAccess, "unrecognized"} {
		for _, risk := range []string{"low", "dangerous", "unknown", ""} {
			r := low
			r.Risk = risk
			want := mode != FullAccess && !(mode == DangerousOnly && risk == "low")
			if got := mode.RequiresApproval(r); got != want {
				t.Errorf("mode %q risk %q: got %t, want %t", mode, risk, got, want)
			}
		}
	}
	for _, field := range []string{"summary", "target", "impact"} {
		r := low
		switch field {
		case "summary":
			r.Summary = ""
		case "target":
			r.Target = ""
		case "impact":
			r.Impact = ""
		}
		if !DangerousOnly.RequiresApproval(r) {
			t.Errorf("missing %s bypassed review", field)
		}
	}
}
