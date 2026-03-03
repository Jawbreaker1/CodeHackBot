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
