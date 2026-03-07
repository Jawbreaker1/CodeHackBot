package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyRequiredArtifactsConcreteFilenameMatch(t *testing.T) {
	t.Parallel()

	required := []string{"zip.hash", "password_found.txt"}
	verified := []string{
		"/tmp/run/artifact/T-003/zip.hash",
		"/tmp/run/artifact/T-003/worker.log",
	}
	missing := verifyRequiredArtifacts(required, verified)
	if len(missing) != 1 || missing[0] != "password_found.txt" {
		t.Fatalf("expected only password_found.txt missing, got %v", missing)
	}
}

func TestVerifyRequiredArtifactsGenericFallback(t *testing.T) {
	t.Parallel()

	required := []string{"command log"}
	verified := []string{"/tmp/run/artifact/T-001/worker.log"}
	missing := verifyRequiredArtifacts(required, verified)
	if len(missing) != 0 {
		t.Fatalf("expected no missing generic artifacts, got %v", missing)
	}
}

func TestVerifyRequiredArtifactsRelativePathMatch(t *testing.T) {
	t.Parallel()

	required := []string{"artifact/T-003/zip.hash"}
	verified := []string{"/home/user/sessions/run-1/orchestrator/artifact/T-003/zip.hash"}
	missing := verifyRequiredArtifacts(required, verified)
	if len(missing) != 0 {
		t.Fatalf("expected relative path requirement to match absolute artifact path, got %v", missing)
	}
}

func TestValidateTaskCompletionContract_ProofWorkflowRequiresObjectiveMet(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-completion-contract-objective"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	artifactPath := filepath.Join(base, "proof.log")
	if err := os.WriteFile(artifactPath, []byte("attempted\n"), 0o644); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}

	c := &Coordinator{
		runID:   runID,
		manager: manager,
	}
	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "Identify the password and extract contents from archive",
		Targets:           []string{"127.0.0.1"},
		ExpectedArtifacts: []string{"proof.log"},
		Strategy:          "local_archive_recovery",
	}
	check, err := c.validateTaskCompletionContract(
		task,
		"t1",
		WorkerSignalID("worker-t1-a1"),
		nil,
		map[string]any{
			"log_path": artifactPath,
			"completion_contract": map[string]any{
				"verification_status":             "reported_by_worker",
				"required_artifacts":              []string{"proof.log"},
				"produced_artifacts":              []string{artifactPath},
				"allow_fallback_without_findings": true,
				"objective_met":                   false,
			},
		},
	)
	if err != nil {
		t.Fatalf("validateTaskCompletionContract: %v", err)
	}
	if check.Status != "failed" {
		t.Fatalf("expected failed status, got %q", check.Status)
	}
	if check.Reason != TaskFailureReasonObjectiveNotMet {
		t.Fatalf("expected reason=%s, got %q", TaskFailureReasonObjectiveNotMet, check.Reason)
	}
}
