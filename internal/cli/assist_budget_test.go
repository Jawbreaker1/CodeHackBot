package cli

import "testing"

func TestEstimateAssistBudgetExtraNetworkGoalIsHigher(t *testing.T) {
	network := estimateAssistBudgetExtra("scan local lan and identify hosts, ports, and services")
	simple := estimateAssistBudgetExtra("say hello")
	if network <= simple {
		t.Fatalf("expected network goal to have higher extra budget (%d <= %d)", network, simple)
	}
}

func TestAssistBudgetExtendsOnProgressWithinHardCap(t *testing.T) {
	b := newAssistBudget("scan network and summarize findings", 6)
	initialRemaining := b.remaining
	b.consume("step executed")
	if progressed, reason := b.trackProgress("list_dir\x1f.", "", "obs-a"); !progressed {
		t.Fatalf("expected progress, reason=%s", reason)
	}
	b.onProgress("new evidence")
	if b.remaining <= initialRemaining-1 {
		t.Fatalf("expected remaining budget extension, got %d (initial=%d)", b.remaining, initialRemaining)
	}
	if b.used+b.remaining > b.hardCap {
		t.Fatalf("budget exceeded hard cap")
	}
}

func TestAssistBudgetNoProgressMarksStall(t *testing.T) {
	b := newAssistBudget("check target", 4)
	b.consume("step executed")
	_, _ = b.trackProgress("list_dir\x1f.", "", "obs-a")
	if progressed, reason := b.trackProgress("list_dir\x1f.", "obs-a", "obs-a"); progressed {
		t.Fatalf("expected no progress, reason=%s", reason)
	}
	b.onStall("no new evidence")
	if b.stalls == 0 {
		t.Fatalf("expected stall count > 0")
	}
}
