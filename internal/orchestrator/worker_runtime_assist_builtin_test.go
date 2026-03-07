package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteWorkerAssistCommandBuiltinReportWritesRequestedFile(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	base := t.TempDir()
	runID := "run-assist-builtin-report"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	depID := "T-001"
	depDir := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, depID)
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("MkdirAll depDir: %v", err)
	}
	depLog := filepath.Join(depDir, "scan.log")
	if err := os.WriteFile(depLog, []byte("127.0.0.1 appears vulnerable to CVE-2024-12345\n"), 0o644); err != nil {
		t.Fatalf("WriteFile depLog: %v", err)
	}

	task := TaskSpec{
		TaskID:            "T-REPORT",
		Goal:              "Generate an OWASP-style report from dependency artifacts",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{depID},
		Strategy:          "summarize_and_replan",
		RiskLevel:         string(RiskReconReadonly),
		DoneWhen:          []string{"report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
	}
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      task.TaskID,
		WorkerID:    "worker-report-a1",
		Attempt:     1,
	}
	workDir := t.TempDir()

	result := executeWorkerAssistCommand(context.Background(), cfg, task, "report", []string{"security_report.md"}, workDir)
	if result.runErr != nil {
		t.Fatalf("executeWorkerAssistCommand(report): %v\noutput:\n%s", result.runErr, string(result.output))
	}
	output := string(result.output)
	if !strings.Contains(output, "# OWASP-Style Security Assessment Report") {
		t.Fatalf("expected report markdown heading in output, got %q", output)
	}
	reportPath := filepath.Join(workDir, "security_report.md")
	reportBody, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("ReadFile reportPath: %v", err)
	}
	if !strings.Contains(string(reportBody), "CVE-2024-12345") {
		t.Fatalf("expected synthesized report to include CVE evidence, got %q", string(reportBody))
	}
}

func TestExecuteWorkerAssistCommandBuiltinReadFileRepairsDependencyArtifactPath(t *testing.T) {
	base := t.TempDir()
	runID := "run-assist-builtin-read"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	depID := "T-001"
	depDir := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, depID)
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("MkdirAll depDir: %v", err)
	}
	depArtifact := filepath.Join(depDir, "scope_inventory.txt")
	if err := os.WriteFile(depArtifact, []byte("archive=secret.zip\n"), 0o644); err != nil {
		t.Fatalf("WriteFile depArtifact: %v", err)
	}

	task := TaskSpec{
		TaskID:            "T-READ",
		Goal:              "Read dependency evidence",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{depID},
		RiskLevel:         string(RiskReconReadonly),
		DoneWhen:          []string{"evidence read"},
		FailWhen:          []string{"evidence unavailable"},
		ExpectedArtifacts: []string{"read.log"},
	}
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      task.TaskID,
		WorkerID:    "worker-read-a1",
		Attempt:     1,
	}
	workDir := t.TempDir()

	result := executeWorkerAssistCommandWithOptions(
		context.Background(),
		cfg,
		task,
		"read_file",
		[]string{"scope_inventory.txt"},
		workDir,
		assistCommandExecOptions{skipRuntimeMutation: true},
	)
	if result.runErr != nil {
		t.Fatalf("executeWorkerAssistCommandWithOptions(read_file): %v\noutput:\n%s", result.runErr, string(result.output))
	}
	if got := string(result.output); !strings.Contains(got, "archive=secret.zip") {
		t.Fatalf("expected repaired dependency artifact content, got %q", got)
	}
	if got := strings.Join(result.args, " "); !strings.Contains(got, depArtifact) {
		t.Fatalf("expected args rewritten to dependency artifact path, got %v", result.args)
	}
}

func TestApplyAssistExternalCommandEnvAddsArchiveRuntimeEnvForJohn(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	task := TaskSpec{
		TaskID:    "T-JOHN",
		Goal:      "Crack the password for secret.zip and validate access",
		Targets:   []string{"127.0.0.1"},
		Strategy:  "local archive recovery",
		RiskLevel: string(RiskReconReadonly),
	}

	env, notes, err := applyAssistExternalCommandEnv([]string{"PATH=/usr/bin"}, task, "john", workDir)
	if err != nil {
		t.Fatalf("applyAssistExternalCommandEnv: %v", err)
	}
	if got := envValue(env, "HOME"); got != workDir {
		t.Fatalf("expected HOME=%q, got %q", workDir, got)
	}
	expectedJohnDir := filepath.Join(workDir, ".john")
	if got := envValue(env, "JOHN"); got != expectedJohnDir {
		t.Fatalf("expected JOHN=%q, got %q", expectedJohnDir, got)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "archive runtime guardrail") {
		t.Fatalf("expected archive runtime guardrail note, got %v", notes)
	}
}
