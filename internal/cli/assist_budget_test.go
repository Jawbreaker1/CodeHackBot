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

func TestAssistBudgetLowValueNewActionRequiresEvidence(t *testing.T) {
	b := newAssistBudget("check target", 4)
	b.consume("step executed")

	// Low-value read/list actions should not count as progress when they do not
	// produce any new evidence signature.
	if progressed, reason := b.trackProgress("list_dir\x1f./tmp", "obs-a", "obs-a"); progressed {
		t.Fatalf("expected no progress for low-value action without evidence, reason=%s", reason)
	} else if reason != "low-value action without new evidence" {
		t.Fatalf("unexpected no-progress reason: %s", reason)
	}

	if progressed, reason := b.trackProgress("read_file\x1f./notes.txt", "obs-a", "obs-a"); progressed {
		t.Fatalf("expected no progress for low-value action without evidence, reason=%s", reason)
	} else if reason != "low-value action without new evidence" {
		t.Fatalf("unexpected no-progress reason: %s", reason)
	}

	if progressed, reason := b.trackProgress("nmap\x1f-sV\x1f192.168.50.1", "obs-a", "obs-a"); !progressed {
		t.Fatalf("expected non-low-value action to count as progress, reason=%s", reason)
	}
}

func TestAssistBudgetOpenLikeLongHorizonExtension(t *testing.T) {
	b := newAssistBudget("scan network and recover credentials", 4)
	originalCap := b.currentCap
	originalHard := b.hardCap
	b.enableLongHorizon()
	if b.currentCap <= originalCap {
		t.Fatalf("expected current cap increase, got %d <= %d", b.currentCap, originalCap)
	}
	if b.hardCap <= originalHard {
		t.Fatalf("expected hard cap increase, got %d <= %d", b.hardCap, originalHard)
	}
	b.used = b.currentCap
	b.remaining = 0
	if ok := b.extendForPersistence(3, "retry"); !ok {
		t.Fatalf("expected persistence extension")
	}
	if b.remaining <= 0 {
		t.Fatalf("expected remaining > 0 after persistence extension")
	}
}
