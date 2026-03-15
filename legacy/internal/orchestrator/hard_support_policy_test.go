package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdaptWeakReportActionBlockedByHardSupportPolicyEnvNone(t *testing.T) {
	base := t.TempDir()
	runID := "run-hard-support-report-none"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	t.Setenv(hardSupportPolicyEnv, "none")

	cfg := WorkerRunConfig{SessionsDir: base, RunID: runID}
	task := TaskSpec{
		TaskID:            "T-04",
		Goal:              "Produce a final OWASP assessment report",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-02"},
		DoneWhen:          []string{"owasp report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"127.0.0.1"}})
	cmd, args, note, rewritten := adaptWeakReportAction(cfg, task, scopePolicy, "cat", []string{"scan.log"}, targetAttribution{})
	if rewritten {
		t.Fatalf("expected report rewrite to be blocked by %s=none", hardSupportPolicyEnv)
	}
	if cmd != "cat" || note != "" || len(args) != 1 {
		t.Fatalf("unexpected rewrite output when blocked: cmd=%q args=%v note=%q", cmd, args, note)
	}
}

func TestEnsureVulnerabilityEvidenceBlockedByHardSupportPolicyEnvNone(t *testing.T) {
	t.Setenv(hardSupportPolicyEnv, "none")
	task := TaskSpec{
		TaskID:    "T-03",
		Title:     "CVE mapping",
		Goal:      "Map services to CVEs",
		Strategy:  "vuln_mapping",
		Targets:   []string{"127.0.0.1"},
		DependsOn: []string{"T-02"},
	}
	args, note, rewritten := ensureVulnerabilityEvidenceActionWithGoal(task, "", "nmap", []string{"-sV", "127.0.0.1"})
	if rewritten {
		t.Fatalf("expected vulnerability evidence rewrite to be blocked by %s=none", hardSupportPolicyEnv)
	}
	if note != "" || len(args) != 2 || args[0] != "-sV" {
		t.Fatalf("unexpected rewrite output when blocked: args=%v note=%q", args, note)
	}
}

func TestAdaptArchiveWorkflowCommandBlockedByHardSupportPolicyEnvNone(t *testing.T) {
	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-hard-support-archive-none"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	realZip := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realZip, []byte("PK\x03\x04archive-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile real zip: %v", err)
	}
	t.Setenv(hardSupportPolicyEnv, "none")

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:            "T-003",
		Title:             "Extract hash material",
		Goal:              "Generate crackable hash material from secret.zip",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-001"},
		ExpectedArtifacts: []string{"zip.hash"},
	}
	workDir := filepath.Join(paths.ArtifactDir, "T-003")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}
	cmd, args, notes, changed, err := adaptArchiveWorkflowCommand(cfg, task, "zip2john", nil, workDir)
	if err != nil {
		t.Fatalf("adaptArchiveWorkflowCommand: %v", err)
	}
	if changed {
		t.Fatalf("expected archive adaptation to be blocked by %s=none", hardSupportPolicyEnv)
	}
	if cmd != "zip2john" || len(args) != 0 || len(notes) != 0 {
		t.Fatalf("unexpected adaptation output when blocked: cmd=%q args=%v notes=%v", cmd, args, notes)
	}
}

func TestAdaptWeakVulnerabilityActionAnnotatesHardSupportPolicy(t *testing.T) {
	task := TaskSpec{
		TaskID:            "T-03",
		Goal:              "Map discovered services and versions to known CVEs",
		Strategy:          "vuln_mapping",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-02"},
		ExpectedArtifacts: []string{"vuln_mapping.json"},
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"127.0.0.1"}})
	cmd, args, note, rewritten := adaptWeakVulnerabilityAction(task, scopePolicy, "cat", []string{"scan.log"})
	if !rewritten {
		t.Fatalf("expected vulnerability rewrite")
	}
	if cmd != "nmap" || len(args) == 0 {
		t.Fatalf("unexpected vulnerability rewrite output: cmd=%q args=%v", cmd, args)
	}
	if !strings.Contains(note, "hard-support capability=vulnerability_evidence") {
		t.Fatalf("expected hard-support annotation in note, got %q", note)
	}
	if !strings.Contains(note, hardSupportPolicyRef) {
		t.Fatalf("expected policy reference in note, got %q", note)
	}
}
