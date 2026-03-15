package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func TestAllowAssistNoNewEvidenceCompletionRejectsArchiveProofTask(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:    "T-07",
		Title:     "Validate proof of access",
		Goal:      "Use recovered password to validate minimal archive access",
		Strategy:  "proof_validation",
		Targets:   []string{"127.0.0.1"},
		DoneWhen:  []string{"proof token extracted"},
		FailWhen:  []string{"no proof file exists"},
		Budget:    TaskBudget{MaxSteps: 8, MaxToolCalls: 12},
		Action:    TaskAction{Type: "assist", Prompt: "validate proof"},
		Priority:  10,
		RiskLevel: string(RiskActiveProbe),
	}
	if allowAssistNoNewEvidenceCompletion(task) {
		t.Fatalf("expected no_new_evidence completion to be rejected for proof-sensitive archive task")
	}
}

func TestValidateAssistCompletionContractRequiresObjectiveEvidence(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:   "T-07",
		Title:    "Validate proof of access",
		Goal:     "Use recovered password to validate minimal archive access",
		Strategy: "proof_validation",
		Targets:  []string{"127.0.0.1"},
	}
	cfg := WorkerRunConfig{SessionsDir: t.TempDir(), RunID: "run-contract-missing-evidence"}
	paths, err := EnsureRunLayout(cfg.SessionsDir, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(paths.ArtifactDir, task.TaskID), 0o755); err != nil {
		t.Fatalf("MkdirAll task artifact dir: %v", err)
	}
	workDir := t.TempDir()
	trueVal := true
	suggestion := assist.Suggestion{
		Type:         "complete",
		ObjectiveMet: &trueVal,
		EvidenceRefs: []string{"john_show.txt"},
	}
	if err := validateAssistCompletionContract(task, cfg, workDir, suggestion); err == nil {
		t.Fatalf("expected completion contract failure when evidence refs do not resolve")
	}
}

func TestValidateAssistCompletionContractAcceptsRecoveredPasswordEvidence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := WorkerRunConfig{SessionsDir: root, RunID: "run-contract-evidence-ok"}
	paths, err := EnsureRunLayout(cfg.SessionsDir, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	task := TaskSpec{
		TaskID:   "T-07",
		Title:    "Validate proof of access",
		Goal:     "Use recovered password to validate minimal archive access",
		Strategy: "proof_validation",
		Targets:  []string{"127.0.0.1"},
	}
	taskArtifactDir := filepath.Join(paths.ArtifactDir, task.TaskID)
	if err := os.MkdirAll(taskArtifactDir, 0o755); err != nil {
		t.Fatalf("MkdirAll task artifact dir: %v", err)
	}
	passPath := filepath.Join(taskArtifactDir, "recovered_password.txt")
	if err := os.WriteFile(passPath, []byte("telefo01\n"), 0o644); err != nil {
		t.Fatalf("WriteFile recovered_password.txt: %v", err)
	}

	trueVal := true
	suggestion := assist.Suggestion{
		Type:         "complete",
		ObjectiveMet: &trueVal,
		EvidenceRefs: []string{"recovered_password.txt"},
	}
	if err := validateAssistCompletionContract(task, cfg, root, suggestion); err != nil {
		t.Fatalf("expected completion contract success, got %v", err)
	}
}

func TestValidateAssistCompletionContractRejectsToolSpecificShowOnlyEvidence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := WorkerRunConfig{SessionsDir: root, RunID: "run-contract-john-show-only"}
	paths, err := EnsureRunLayout(cfg.SessionsDir, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	task := TaskSpec{
		TaskID:   "T-07",
		Title:    "Validate proof of access",
		Goal:     "Use recovered password to validate minimal archive access",
		Strategy: "proof_validation",
		Targets:  []string{"127.0.0.1"},
	}
	taskArtifactDir := filepath.Join(paths.ArtifactDir, task.TaskID)
	if err := os.MkdirAll(taskArtifactDir, 0o755); err != nil {
		t.Fatalf("MkdirAll task artifact dir: %v", err)
	}
	showPath := filepath.Join(taskArtifactDir, "john_show.txt")
	showContent := "secret.zip/secret_text.txt:telefo01:secret_text.txt:secret.zip::secret.zip\n1 password hash cracked, 0 left\n"
	if err := os.WriteFile(showPath, []byte(showContent), 0o644); err != nil {
		t.Fatalf("WriteFile john_show.txt: %v", err)
	}

	trueVal := true
	suggestion := assist.Suggestion{
		Type:         "complete",
		ObjectiveMet: &trueVal,
		EvidenceRefs: []string{"john_show.txt"},
	}
	if err := validateAssistCompletionContract(task, cfg, root, suggestion); err == nil {
		t.Fatalf("expected completion contract failure for tool-specific show output without capability-level proof")
	}
}

func TestLocalWorkflowSensitiveArtifactName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "recovered_password.txt", want: true},
		{name: "password_found.log", want: true},
		{name: "proof_of_access.txt", want: true},
		{name: "password_attempt.log", want: false},
		{name: "attempt_status.log", want: false},
	}
	for _, tc := range tests {
		got := localWorkflowSensitiveArtifactName(tc.name)
		if got != tc.want {
			t.Fatalf("localWorkflowSensitiveArtifactName(%q)=%v want=%v", tc.name, got, tc.want)
		}
	}
}

func TestLocalWorkflowCriticalArtifactName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "recovered_password.txt", want: true},
		{name: "proof_of_access.txt", want: true},
		{name: "extraction_status.txt", want: true},
		{name: "extracted_files.txt", want: true},
		{name: "command.log", want: false},
	}
	for _, tc := range tests {
		got := localWorkflowCriticalArtifactName(tc.name)
		if got != tc.want {
			t.Fatalf("localWorkflowCriticalArtifactName(%q)=%v want=%v", tc.name, got, tc.want)
		}
	}
}

func TestExpectedArtifactRequiresConcreteSource_ArchiveWorkflow(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:   "T-03",
		Title:    "Bounded Password Cracking",
		Goal:     "Execute bounded password cracking strategies against extracted hash from local archive",
		Strategy: "bounded_cracking",
	}
	if expectedArtifactRequiresConcreteSource(task, "password_attempt.log") {
		t.Fatalf("did not expect password attempt log to require concrete source")
	}
	if !expectedArtifactRequiresConcreteSource(task, "recovered_password.txt") {
		t.Fatalf("expected recovered password artifact to require concrete source")
	}
	if !expectedArtifactRequiresConcreteSource(task, "extraction_status.txt") {
		t.Fatalf("expected extraction status artifact to require concrete source")
	}
}
