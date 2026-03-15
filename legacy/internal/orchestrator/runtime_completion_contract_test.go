package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCommandCompletionContractIncludesObjectiveProofFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := WorkerRunConfig{SessionsDir: root, RunID: "run-contract-fields"}
	paths, err := EnsureRunLayout(cfg.SessionsDir, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	task := TaskSpec{
		TaskID:   "t1",
		Goal:     "collect service evidence",
		Targets:  []string{"127.0.0.1"},
		Strategy: "service_enumeration",
	}
	artifactDir := filepath.Join(paths.ArtifactDir, task.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("MkdirAll artifactDir: %v", err)
	}
	logPath := filepath.Join(artifactDir, "worker.log")
	if err := os.WriteFile(logPath, []byte("nmap output\n"), 0o644); err != nil {
		t.Fatalf("WriteFile logPath: %v", err)
	}
	evidencePath := filepath.Join(artifactDir, "service_version_output.txt")
	if err := os.WriteFile(evidencePath, []byte("22/tcp open ssh OpenSSH 9.6\n"), 0o644); err != nil {
		t.Fatalf("WriteFile evidencePath: %v", err)
	}

	contract, err := buildCommandCompletionContract(
		task,
		cfg,
		root,
		"nmap",
		[]byte("22/tcp open ssh OpenSSH 9.6\n"),
		[]string{"service_version_output.txt"},
		[]string{logPath, evidencePath},
	)
	if err != nil {
		t.Fatalf("buildCommandCompletionContract: %v", err)
	}
	if objectiveMet, ok := contract["objective_met"].(bool); !ok || !objectiveMet {
		t.Fatalf("expected objective_met=true, got %#v", contract["objective_met"])
	}
	if whyMet := strings.TrimSpace(toString(contract["why_met"])); whyMet == "" {
		t.Fatalf("expected non-empty why_met")
	}
	evidenceRefs := compactStrings(sliceFromAny(contract["evidence_refs"]))
	if len(evidenceRefs) == 0 {
		t.Fatalf("expected evidence_refs")
	}
}

func TestBuildCommandCompletionContractRejectsSyntheticNoOutputEvidence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := WorkerRunConfig{SessionsDir: root, RunID: "run-contract-no-output"}
	paths, err := EnsureRunLayout(cfg.SessionsDir, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	task := TaskSpec{
		TaskID:   "t2",
		Goal:     "collect command evidence",
		Targets:  []string{"127.0.0.1"},
		Strategy: "local_validation",
	}
	artifactDir := filepath.Join(paths.ArtifactDir, task.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("MkdirAll artifactDir: %v", err)
	}
	logPath := filepath.Join(artifactDir, "worker.log")
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile logPath: %v", err)
	}
	derivedPath := filepath.Join(artifactDir, "derived.txt")
	if err := os.WriteFile(derivedPath, []byte("no command output captured\n"), 0o644); err != nil {
		t.Fatalf("WriteFile derivedPath: %v", err)
	}

	_, err = buildCommandCompletionContract(
		task,
		cfg,
		root,
		"sh",
		nil,
		nil,
		[]string{logPath, derivedPath},
	)
	if err == nil {
		t.Fatalf("expected completion contract rejection for synthetic no-output evidence")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no meaningful execution evidence") {
		t.Fatalf("unexpected error: %v", err)
	}
}
