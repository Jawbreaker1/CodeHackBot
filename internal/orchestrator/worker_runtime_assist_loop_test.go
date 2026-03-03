package orchestrator

import "testing"

func TestResolveRecoverPivotBasisUsesUnknownFromSummary(t *testing.T) {
	t.Parallel()

	basis, source, kind := resolveRecoverPivotBasis("Unknown under test: whether /tmp/secret.zip uses weak password", "", nil)
	if basis == "" {
		t.Fatalf("expected non-empty basis")
	}
	if source != "summary" {
		t.Fatalf("expected summary source, got %q", source)
	}
	if kind != "unknown" {
		t.Fatalf("expected unknown kind, got %q", kind)
	}
}

func TestResolveRecoverPivotBasisFallsBackToRecoverHint(t *testing.T) {
	t.Parallel()

	basis, source, kind := resolveRecoverPivotBasis("pivot command", "previous command failed: no such file /tmp/wordlist.txt", nil)
	if basis == "" {
		t.Fatalf("expected non-empty basis")
	}
	if source != "recover_hint" {
		t.Fatalf("expected recover_hint source, got %q", source)
	}
	if kind != "evidence" {
		t.Fatalf("expected evidence kind, got %q", kind)
	}
}

func TestResolveRecoverPivotBasisFallsBackToRecentObservation(t *testing.T) {
	t.Parallel()

	observations := []string{
		"cmd: list_dir . => ok",
		"cmd: bash tools/check.sh => failed (exit code 1)",
	}
	basis, source, kind := resolveRecoverPivotBasis("pivot to alternate command", "", observations)
	if basis == "" {
		t.Fatalf("expected non-empty basis")
	}
	if source != "observation" {
		t.Fatalf("expected observation source, got %q", source)
	}
	if kind != "evidence" {
		t.Fatalf("expected evidence kind, got %q", kind)
	}
}
