package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRuntimeCommandPipelineInjectsArchiveInputAndStageNote(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}
	zipPath := filepath.Join(workDir, "secret.zip")
	if err := os.WriteFile(zipPath, []byte("PK\x03\x04dummy"), 0o644); err != nil {
		t.Fatalf("WriteFile zipPath: %v", err)
	}

	cfg := WorkerRunConfig{
		SessionsDir: filepath.Join(base, "sessions"),
		RunID:       "run-pipeline-archive",
	}
	task := TaskSpec{
		TaskID:    "t1",
		Goal:      "extract secret.zip and identify password",
		Targets:   []string{"127.0.0.1"},
		Strategy:  "local archive recovery",
		RiskLevel: string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "zip2john",
		},
	}

	policy := NewScopePolicy(Scope{Targets: []string{"127.0.0.1"}})
	result, err := applyRuntimeCommandPipeline(cfg, task, policy, "zip2john", nil, targetAttribution{}, task.Goal, workDir)
	if err != nil {
		t.Fatalf("applyRuntimeCommandPipeline: %v", err)
	}
	if result.Command != "zip2john" {
		t.Fatalf("unexpected command: %q", result.Command)
	}
	if len(result.Args) == 0 || filepath.Clean(result.Args[len(result.Args)-1]) != filepath.Clean(zipPath) {
		t.Fatalf("expected zip input arg %q, got %#v", zipPath, result.Args)
	}
	if !pipelineHasStage(result.Notes, "adapt_archive_workflow") {
		t.Fatalf("expected adapt_archive_workflow stage note, got %#v", result.Notes)
	}
}

func TestApplyRuntimeCommandPipelineDoesNotInjectArchiveInputForAssistTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}
	zipPath := filepath.Join(workDir, "secret.zip")
	if err := os.WriteFile(zipPath, []byte("PK\x03\x04dummy"), 0o644); err != nil {
		t.Fatalf("WriteFile zipPath: %v", err)
	}

	cfg := WorkerRunConfig{
		SessionsDir: filepath.Join(base, "sessions"),
		RunID:       "run-pipeline-archive-assist",
	}
	task := TaskSpec{
		TaskID:    "t1",
		Goal:      "extract secret.zip and identify password",
		Targets:   []string{"127.0.0.1"},
		Strategy:  "local archive recovery",
		RiskLevel: string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "assist",
			Command: "zip2john",
		},
	}

	policy := NewScopePolicy(Scope{Targets: []string{"127.0.0.1"}})
	result, err := applyRuntimeCommandPipeline(cfg, task, policy, "zip2john", nil, targetAttribution{}, task.Goal, workDir)
	if err != nil {
		t.Fatalf("applyRuntimeCommandPipeline: %v", err)
	}
	if result.Command != "zip2john" {
		t.Fatalf("unexpected command: %q", result.Command)
	}
	if len(result.Args) != 0 {
		t.Fatalf("expected assist task args to remain unchanged, got %#v", result.Args)
	}
	if pipelineHasStage(result.Notes, "adapt_archive_workflow") {
		t.Fatalf("did not expect adapt_archive_workflow stage note for assist task, got %#v", result.Notes)
	}
}

func TestApplyRuntimeCommandPipelineIncludesPrepareStageForNetworkTargetFallback(t *testing.T) {
	t.Parallel()

	cfg := WorkerRunConfig{
		SessionsDir: t.TempDir(),
		RunID:       "run-pipeline-prepare",
	}
	task := TaskSpec{
		TaskID:    "t1",
		Goal:      "scan localhost ports",
		Targets:   []string{"127.0.0.1"},
		RiskLevel: string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "nmap",
			Args:    []string{"-sV"},
		},
	}
	policy := NewScopePolicy(Scope{Targets: []string{"127.0.0.1"}})
	result, err := applyRuntimeCommandPipeline(cfg, task, policy, "nmap", []string{"-sV"}, targetAttribution{}, task.Goal, "")
	if err != nil {
		t.Fatalf("applyRuntimeCommandPipeline: %v", err)
	}
	if len(result.Args) == 0 || result.Args[len(result.Args)-1] != "127.0.0.1" {
		t.Fatalf("expected injected target arg, got %#v", result.Args)
	}
	if !pipelineHasStage(result.Notes, "prepare_runtime_command") {
		t.Fatalf("expected prepare_runtime_command stage note, got %#v", result.Notes)
	}
}

func TestApplyRuntimeCommandPipelineSkipsPrepareStageForAssistNmapTask(t *testing.T) {
	t.Parallel()

	cfg := WorkerRunConfig{
		SessionsDir: t.TempDir(),
		RunID:       "run-pipeline-prepare-assist",
	}
	task := TaskSpec{
		TaskID:    "t1",
		Goal:      "scan localhost ports",
		Targets:   []string{"127.0.0.1"},
		RiskLevel: string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "assist",
			Command: "nmap",
			Args:    []string{"-sV"},
		},
	}
	policy := NewScopePolicy(Scope{Targets: []string{"127.0.0.1"}})
	result, err := applyRuntimeCommandPipeline(cfg, task, policy, "nmap", []string{"-sV"}, targetAttribution{}, task.Goal, t.TempDir())
	if err != nil {
		t.Fatalf("applyRuntimeCommandPipeline: %v", err)
	}
	if pipelineHasStage(result.Notes, "prepare_runtime_command") {
		t.Fatalf("did not expect prepare_runtime_command stage for assist task, got %#v", result.Notes)
	}
	if len(result.Args) != 1 || result.Args[0] != "-sV" {
		t.Fatalf("expected assist args to remain unchanged, got %#v", result.Args)
	}
}

func TestApplyRuntimeCommandPipelineSkipsWeakVulnerabilityRewriteForAssistTask(t *testing.T) {
	t.Parallel()

	cfg := WorkerRunConfig{
		SessionsDir: t.TempDir(),
		RunID:       "run-pipeline-vuln-assist",
	}
	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "Map likely vulnerabilities on router 192.168.50.1",
		Targets:           []string{"192.168.50.1"},
		DependsOn:         []string{"T-01"},
		ExpectedArtifacts: []string{"vulnerability mapping"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "assist",
			Command: "cat",
			Args:    []string{"scan.log"},
		},
	}
	policy := NewScopePolicy(Scope{Targets: []string{"192.168.50.1"}})
	result, err := applyRuntimeCommandPipeline(cfg, task, policy, "cat", []string{"scan.log"}, targetAttribution{}, task.Goal, t.TempDir())
	if err != nil {
		t.Fatalf("applyRuntimeCommandPipeline: %v", err)
	}
	if result.Command != "cat" {
		t.Fatalf("expected command to remain unchanged, got %q", result.Command)
	}
	if pipelineHasStage(result.Notes, "adapt_weak_vulnerability_action") || pipelineHasStage(result.Notes, "enforce_vulnerability_evidence") {
		t.Fatalf("did not expect vulnerability rewrite stages for assist task, got %#v", result.Notes)
	}
}

func TestApplyRuntimeCommandPipelineSkipsWeakReportRewriteForAssistTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-pipeline-report-assist"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
	}
	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "Produce a final OWASP assessment report with findings and remediation",
		Targets:           []string{"192.168.50.1"},
		DependsOn:         []string{"T-01"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "assist",
			Command: "cat",
			Args:    []string{"scan.log"},
		},
	}
	policy := NewScopePolicy(Scope{Targets: []string{"192.168.50.1"}})
	result, err := applyRuntimeCommandPipeline(cfg, task, policy, "cat", []string{"scan.log"}, targetAttribution{}, task.Goal, t.TempDir())
	if err != nil {
		t.Fatalf("applyRuntimeCommandPipeline: %v", err)
	}
	if result.Command != "cat" {
		t.Fatalf("expected command to remain unchanged, got %q", result.Command)
	}
	if pipelineHasStage(result.Notes, "adapt_weak_report_action") {
		t.Fatalf("did not expect report rewrite stage for assist task, got %#v", result.Notes)
	}
}

func pipelineHasStage(notes []runtimeMutationNote, stage string) bool {
	for _, note := range notes {
		if note.Stage == stage {
			return true
		}
	}
	return false
}
