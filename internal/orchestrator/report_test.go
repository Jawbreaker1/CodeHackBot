package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssembleRunReportIncludesFindingsAndArtifactLinks(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-report-1"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Networks: []string{"192.168.50.0/24"}, Targets: []string{"192.168.50.77"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"report_done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scan",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"nmap.log"},
				RiskLevel:         "recon_readonly",
				Budget: TaskBudget{
					MaxSteps:     3,
					MaxToolCalls: 3,
					MaxRuntime:   time.Second,
				},
			},
		},
	}
	if err := WriteJSONAtomic(planPath, plan); err != nil {
		t.Fatalf("WriteJSONAtomic plan: %v", err)
	}
	if _, err := manager.Start(planPath, ""); err != nil {
		t.Fatalf("Start: %v", err)
	}

	now := time.Now().UTC()
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-artifact",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "t1",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskArtifact,
		Payload: mustJSONRaw(map[string]any{
			"type":  "log",
			"title": "nmap output",
			"path":  "sessions/run-report-1/logs/nmap.log",
		}),
	}); err != nil {
		t.Fatalf("append artifact: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-finding-a",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "t1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.77",
			"finding_type": "open_port",
			"title":        "SSH open",
			"location":     "22/tcp",
			"severity":     "low",
			"confidence":   "high",
			"source":       "nmap",
			"evidence":     []any{"22/tcp open"},
		}),
	}); err != nil {
		t.Fatalf("append finding1: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-finding-b",
		RunID:    runID,
		WorkerID: "worker-2",
		TaskID:   "t1",
		Seq:      1,
		TS:       now.Add(2 * time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.77",
			"finding_type": "open_port",
			"title":        "SSH open",
			"location":     "22/tcp",
			"severity":     "medium",
			"confidence":   "medium",
			"source":       "service-probe",
			"evidence":     []any{"banner: OpenSSH_9.6"},
		}),
	}); err != nil {
		t.Fatalf("append finding2: %v", err)
	}

	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	reportPath, err := manager.AssembleRunReport(runID, "")
	if err != nil {
		t.Fatalf("AssembleRunReport: %v", err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Orchestrator Run Report") {
		t.Fatalf("missing report title:\n%s", content)
	}
	if !strings.Contains(content, "SSH open") {
		t.Fatalf("missing finding title:\n%s", content)
	}
	if !strings.Contains(content, "22/tcp open") || !strings.Contains(content, "banner: OpenSSH_9.6") {
		t.Fatalf("missing merged evidence:\n%s", content)
	}
	if !strings.Contains(content, "nmap output") || !strings.Contains(content, "sessions/run-report-1/logs/nmap.log") {
		t.Fatalf("missing artifact info:\n%s", content)
	}
	if !strings.Contains(content, "orchestrator/artifact/e-artifact.json") {
		t.Fatalf("missing artifact record path:\n%s", content)
	}
}
