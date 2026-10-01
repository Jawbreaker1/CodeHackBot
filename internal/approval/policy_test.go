package approval

import "testing"

func TestApprovalModes(t *testing.T) {
	low := Request{Summary: "Read the page title", Target: "local fixture", Impact: "Read only", Risk: "low"}
	for _, mode := range []Mode{"", EveryExecution, DangerousOnly, FullAccess, "unrecognized"} {
		for _, risk := range []string{"low", "dangerous", "unknown", ""} {
			r := low
			r.Risk = risk
			want := mode != FullAccess
			if got := mode.RequiresApproval(r); got != want {
				t.Errorf("mode %q risk %q: got %t, want %t", mode, risk, got, want)
			}
		}
	}
	observation := ReadOnlyObservationRequest(`{"name":"host_system"}`, "/workspace", "Read host metadata", "local host", "Read only")
	if !EveryExecution.RequiresApproval(observation) || DangerousOnly.RequiresApproval(observation) || FullAccess.RequiresApproval(observation) {
		t.Fatal("built-in observation did not follow the selected approval mode")
	}
	for _, risk := range []string{"dangerous", "unknown", ""} {
		r := observation
		r.Risk = risk
		if !DangerousOnly.RequiresApproval(r) {
			t.Errorf("trusted observation with risk %q bypassed review", risk)
		}
	}
	for _, field := range []string{"summary", "target", "impact"} {
		r := observation
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
	for _, command := range []string{"uname -a", "python3 helper.py", "bash -c 'echo ok'"} {
		r := low
		r.Command = command
		if !DangerousOnly.RequiresApproval(r) {
			t.Errorf("worker command %q bypassed review from model risk label", command)
		}
	}
	reviewed := ReviewedLowRiskRequest(low, "A bounded read-only check")
	if DangerousOnly.RequiresApproval(reviewed) || !EveryExecution.RequiresApproval(reviewed) {
		t.Fatal("independently reviewed low-risk action did not follow the session mode")
	}
	reviewed.Risk = "dangerous"
	if !DangerousOnly.RequiresApproval(reviewed) {
		t.Fatal("dangerous action bypassed review despite an earlier low-risk mark")
	}
}
