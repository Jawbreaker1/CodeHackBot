package orchestrator

import "testing"

func TestBestArtifactCandidateForMissingPathWithFamilyUnknownAllowsExactBasename(t *testing.T) {
	t.Parallel()

	missingPath := "/tmp/evidence.bin"
	candidates := []string{
		"/tmp/run/orchestrator/artifact/T-01/evidence.bin",
		"/tmp/run/orchestrator/artifact/T-02/evidence-copy.bin",
	}

	got := bestArtifactCandidateForMissingPathWithFamily(missingPath, artifactFamilyUnknown, candidates, map[string]struct{}{})
	want := "/tmp/run/orchestrator/artifact/T-01/evidence.bin"
	if got != want {
		t.Fatalf("expected exact-basename replacement %q, got %q", want, got)
	}
}

func TestBestArtifactCandidateForMissingPathWithFamilyUnknownRejectsSemanticAlias(t *testing.T) {
	t.Parallel()

	missingPath := "/tmp/router_findings_snapshot.txt"
	candidates := []string{
		"/tmp/run/orchestrator/artifact/T-01/router-findings-summary.txt",
		"/tmp/run/orchestrator/artifact/T-01/router-findings.log",
	}

	got := bestArtifactCandidateForMissingPathWithFamily(missingPath, artifactFamilyUnknown, candidates, map[string]struct{}{})
	if got != "" {
		t.Fatalf("expected unknown-family semantic alias to be rejected, got %q", got)
	}
}

func TestBestArtifactCandidateForMissingPathWithFamilyRejectsFamilyMismatch(t *testing.T) {
	t.Parallel()

	missingPath := "/tmp/vuln_scan_results.log"
	candidates := []string{
		"/tmp/run/orchestrator/artifact/T-01/vuln_scan_results.hash",
	}

	got := bestArtifactCandidateForMissingPathWithFamily(missingPath, artifactFamilyAuxiliaryLog, candidates, map[string]struct{}{})
	if got != "" {
		t.Fatalf("expected family mismatch to be rejected, got %q", got)
	}
}

func TestInferExpectedRepairFamilyTaskIntentPrecedesPathHeuristic(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		Goal:     "Synthesize OWASP report from validated evidence",
		Strategy: "report_synthesis",
	}
	arg := "/tmp/secret.zip"
	got := inferExpectedRepairFamily(task, "read_file", []string{arg}, 0, arg)
	if got != artifactFamilyValidationInput {
		t.Fatalf("expected report task input to prefer validation_input, got %q", got)
	}
}
