package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExtractReportCandidateCVEClaims(t *testing.T) {
	t.Parallel()

	report := `## Findings
### Candidate Findings
### CVE-2010-0738 (candidate)
- Claim state: candidate
- Claim reason: missing_phase_requirements: lookup_source, execute_bounded_validation
- Phase gate: discover_signal=yes lookup_source=no derive_verification_steps=yes execute_bounded_validation=no target_applicability=yes
- Evidence file: sessions/run/a.log
- Evidence excerpt: host appears vulnerable
`
	claims := extractReportCandidateCVEClaims([]byte(report))
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	claim := claims[0]
	if claim.CVE != "CVE-2010-0738" {
		t.Fatalf("expected CVE-2010-0738, got %q", claim.CVE)
	}
	if !strings.Contains(claim.Reason, "missing_phase_requirements") {
		t.Fatalf("expected reason to include missing phases, got %q", claim.Reason)
	}
	missing := strings.Join(claim.MissingPhases, ",")
	if !strings.Contains(missing, "lookup_source") || !strings.Contains(missing, "execute_bounded_validation") {
		t.Fatalf("expected missing phases from phase gate, got %q", missing)
	}
	if len(claim.Evidence) != 1 || claim.Evidence[0] != "sessions/run/a.log" {
		t.Fatalf("expected evidence path from report, got %#v", claim.Evidence)
	}
}

func TestEmitReportCandidateFindings(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-report-candidate-finding"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.1"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "T-04-report-generation",
				Title:             "Generate report",
				Goal:              "Generate OWASP report from prior artifacts",
				Targets:           []string{"192.168.50.1"},
				DependsOn:         []string{"T-01"},
				DoneWhen:          []string{"report_generated"},
				FailWhen:          []string{"report_failed"},
				ExpectedArtifacts: []string{"owasp_report.md"},
				RiskLevel:         string(RiskReconReadonly),
				Action: TaskAction{
					Type:    "command",
					Command: "echo",
					Args:    []string{"ok"},
				},
				Budget: TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Minute,
				},
			},
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	cfg := WorkerRunConfig{
		RunID:       runID,
		TaskID:      "T-04-report-generation",
		WorkerID:    "worker-T-04-report-generation-a1",
		Attempt:     1,
		SessionsDir: base,
	}
	report := `## Findings
### Candidate Findings
### CVE-2010-0738 (candidate)
- Claim state: candidate
- Claim reason: missing_phase_requirements: lookup_source
- Phase gate: discover_signal=yes lookup_source=no derive_verification_steps=yes execute_bounded_validation=yes target_applicability=yes
- Evidence file: sessions/run-router/logs/nmap.log
`
	if err := emitReportCandidateFindings(manager, cfg, plan.Tasks[0], []byte(report), []string{"sessions/run-router/report.log"}); err != nil {
		t.Fatalf("emitReportCandidateFindings: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var found bool
	for _, event := range events {
		if event.Type != EventTypeTaskFinding || event.TaskID != "T-04-report-generation" {
			continue
		}
		payload := map[string]any{}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if strings.TrimSpace(toString(payload["finding_type"])) != findingTypeCVECandidateClaim {
			continue
		}
		found = true
		if strings.TrimSpace(toString(payload["title"])) == "" {
			t.Fatalf("expected title in payload")
		}
		meta, _ := payload["metadata"].(map[string]any)
		if strings.TrimSpace(toString(meta["cve"])) != "CVE-2010-0738" {
			t.Fatalf("expected cve metadata, got %#v", payload["metadata"])
		}
		if strings.TrimSpace(toString(meta["missing_phases"])) != "lookup_source" {
			t.Fatalf("expected missing phase metadata, got %#v", payload["metadata"])
		}
	}
	if !found {
		t.Fatalf("expected cve_candidate_claim finding event")
	}
}
