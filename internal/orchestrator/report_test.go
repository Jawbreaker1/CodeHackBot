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
			"state":        FindingStateVerified,
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
			"state":        FindingStateVerified,
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
	if !strings.Contains(content, "artifact/e-artifact.json") {
		t.Fatalf("missing artifact record path:\n%s", content)
	}
}

func TestAssembleRunReportMarksUnverifiedFindings(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-report-unverified"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.77"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"report_done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t-network",
				Goal:              "network scan",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"network.log"},
				RiskLevel:         "recon_readonly",
				Budget: TaskBudget{
					MaxSteps:     3,
					MaxToolCalls: 3,
					MaxRuntime:   time.Second,
				},
			},
			{
				TaskID:            "t-web",
				Goal:              "web recon",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"web.log"},
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
		EventID:  "e-artifact-network",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "t-network",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskArtifact,
		Payload: mustJSONRaw(map[string]any{
			"type":  "log",
			"title": "network scan log",
			"path":  "sessions/run-report-unverified/logs/network.log",
		}),
	}); err != nil {
		t.Fatalf("append artifact: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-finding-network",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "t-network",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.77",
			"finding_type": "open_port",
			"title":        "SSH open",
			"state":        FindingStateVerified,
			"location":     "22/tcp",
			"severity":     "low",
			"confidence":   "high",
			"source":       "nmap",
			"evidence":     []any{"22/tcp open"},
		}),
	}); err != nil {
		t.Fatalf("append network finding: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-finding-web",
		RunID:    runID,
		WorkerID: "worker-2",
		TaskID:   "t-web",
		Seq:      1,
		TS:       now.Add(2 * time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.77",
			"finding_type": "web_recon",
			"title":        "Potential exposed admin endpoint",
			"location":     "/admin",
			"severity":     "medium",
			"confidence":   "medium",
			"source":       "httpx",
			"evidence":     []any{"status=401 path=/admin"},
		}),
	}); err != nil {
		t.Fatalf("append web finding: %v", err)
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
	if !strings.Contains(content, "[VERIFIED] SSH open") {
		t.Fatalf("expected verified finding label:\n%s", content)
	}
	if !strings.Contains(content, "[UNVERIFIED] Potential exposed admin endpoint") {
		t.Fatalf("expected unverified finding label:\n%s", content)
	}
	if !strings.Contains(content, "Linked artifact/log evidence: `UNVERIFIED`") {
		t.Fatalf("expected explicit unverified evidence marker:\n%s", content)
	}
	if !strings.Contains(content, "sessions/run-report-unverified/logs/network.log") {
		t.Fatalf("expected linked network artifact path:\n%s", content)
	}
	if !strings.Contains(content, "- Verified findings: 1") || !strings.Contains(content, "- Unverified findings: 1") {
		t.Fatalf("expected verified/unverified summary counts:\n%s", content)
	}
	if !strings.Contains(content, "- Claim truth gate: `PASS`") {
		t.Fatalf("expected claim truth gate pass in mixed verified/unverified report:\n%s", content)
	}
}

func TestAssembleRunReportTruthGateFailsOnUnverifiedHighImpact(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-report-truth-gate-fail"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.91"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"report_done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t-web",
				Goal:              "web scan",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"web.log"},
				RiskLevel:         "recon_readonly",
				Budget:            TaskBudget{MaxSteps: 3, MaxToolCalls: 3, MaxRuntime: time.Second},
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
		EventID:  "e-find-high",
		RunID:    runID,
		WorkerID: "worker-web",
		TaskID:   "t-web",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.91",
			"finding_type": "web_vuln",
			"title":        "Potential RCE claim",
			"location":     "/cgi-bin/admin",
			"severity":     "high",
			"confidence":   "medium",
			"source":       "scanner",
			"evidence":     []any{"response signature matched"},
		}),
	}); err != nil {
		t.Fatalf("append high finding: %v", err)
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
	if !strings.Contains(content, "- Claim truth gate: `FAIL`") {
		t.Fatalf("expected claim truth gate fail:\n%s", content)
	}
	if !strings.Contains(content, "- High-impact unverified claims: 1") {
		t.Fatalf("expected high-impact unverified claim count:\n%s", content)
	}
}

func TestAssembleRunReportQualityNetworkAndWebArtifacts(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-report-quality"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.77"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"report_done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t-net",
				Goal:              "network scan",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"nmap.log"},
				RiskLevel:         "recon_readonly",
				Budget:            TaskBudget{MaxSteps: 3, MaxToolCalls: 3, MaxRuntime: time.Second},
			},
			{
				TaskID:            "t-web",
				Goal:              "web recon",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"web.log"},
				RiskLevel:         "recon_readonly",
				Budget:            TaskBudget{MaxSteps: 3, MaxToolCalls: 3, MaxRuntime: time.Second},
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
		EventID:  "e-art-net",
		RunID:    runID,
		WorkerID: "worker-net",
		TaskID:   "t-net",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskArtifact,
		Payload: mustJSONRaw(map[string]any{
			"type":  "log",
			"title": "nmap scan log",
			"path":  "sessions/run-report-quality/logs/nmap.log",
		}),
	}); err != nil {
		t.Fatalf("append net artifact: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-art-web",
		RunID:    runID,
		WorkerID: "worker-web",
		TaskID:   "t-web",
		Seq:      1,
		TS:       now.Add(100 * time.Millisecond),
		Type:     EventTypeTaskArtifact,
		Payload: mustJSONRaw(map[string]any{
			"type":  "log",
			"title": "web recon log",
			"path":  "sessions/run-report-quality/logs/web.log",
		}),
	}); err != nil {
		t.Fatalf("append web artifact: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-find-net",
		RunID:    runID,
		WorkerID: "worker-net",
		TaskID:   "t-net",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.77",
			"finding_type": "open_port",
			"title":        "SSH open",
			"state":        FindingStateVerified,
			"location":     "22/tcp",
			"severity":     "low",
			"confidence":   "high",
			"source":       "nmap",
			"evidence":     []any{"22/tcp open"},
		}),
	}); err != nil {
		t.Fatalf("append net finding: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-find-web",
		RunID:    runID,
		WorkerID: "worker-web",
		TaskID:   "t-web",
		Seq:      2,
		TS:       now.Add(2 * time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.77",
			"finding_type": "web_recon",
			"title":        "Login panel discovered",
			"state":        FindingStateVerified,
			"location":     "/login",
			"severity":     "info",
			"confidence":   "high",
			"source":       "ffuf",
			"evidence":     []any{"status=200 path=/login"},
		}),
	}); err != nil {
		t.Fatalf("append web finding: %v", err)
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
	if !strings.Contains(content, "[VERIFIED] SSH open") || !strings.Contains(content, "[VERIFIED] Login panel discovered") {
		t.Fatalf("expected verified labels for both network and web findings:\n%s", content)
	}
	if !strings.Contains(content, "sessions/run-report-quality/logs/nmap.log") || !strings.Contains(content, "sessions/run-report-quality/logs/web.log") {
		t.Fatalf("expected network + web log links in report:\n%s", content)
	}
}

func TestAssembleRunReportIncludesExecutionNarrative(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-report-narrative"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"report_done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  2,
		Metadata: PlanMetadata{
			Goal: "Recover password from local encrypted archive and report outcome",
		},
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Title:             "Prepare archive metadata",
				Goal:              "Collect ZIP metadata.",
				DoneWhen:          []string{"metadata captured"},
				FailWhen:          []string{"metadata collection failed"},
				ExpectedArtifacts: []string{"zipinfo_verbose.txt"},
				RiskLevel:         "recon_readonly",
				Action: TaskAction{
					Type:    "command",
					Command: "bash",
					Args:    []string{"-lc", "zipinfo -v secret.zip > zipinfo_verbose.txt"},
				},
				Budget: TaskBudget{MaxSteps: 2, MaxToolCalls: 2, MaxRuntime: time.Minute},
			},
			{
				TaskID:            "t2",
				Title:             "Attempt password recovery",
				Goal:              "Recover ZIP password.",
				DependsOn:         []string{"t1"},
				DoneWhen:          []string{"password recovered"},
				FailWhen:          []string{"password recovery failed"},
				ExpectedArtifacts: []string{"recovered_password.txt"},
				RiskLevel:         "active_probe",
				Action: TaskAction{
					Type:    "command",
					Command: "bash",
					Args:    []string{"-lc", "echo attempt > recovered_password.txt"},
				},
				Budget: TaskBudget{MaxSteps: 2, MaxToolCalls: 2, MaxRuntime: time.Minute},
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
	append := func(event EventEnvelope) {
		if err := AppendEventJSONL(manager.eventPath(runID), event); err != nil {
			t.Fatalf("append event %s: %v", event.Type, err)
		}
	}
	append(EventEnvelope{
		EventID:  "e-t1-start",
		RunID:    runID,
		WorkerID: "worker-t1",
		TaskID:   "t1",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskStarted,
		Payload:  mustJSONRaw(map[string]any{"attempt": 1, "worker_id": "worker-t1", "goal": "Collect ZIP metadata."}),
	})
	append(EventEnvelope{
		EventID:  "e-t1-progress",
		RunID:    runID,
		WorkerID: "worker-t1",
		TaskID:   "t1",
		Seq:      2,
		TS:       now.Add(20 * time.Millisecond),
		Type:     EventTypeTaskProgress,
		Payload: mustJSONRaw(map[string]any{
			"message": "executing action command",
			"command": "bash",
			"args":    []any{"-lc", "zipinfo -v secret.zip > zipinfo_verbose.txt"},
		}),
	})
	append(EventEnvelope{
		EventID:  "e-t1-art",
		RunID:    runID,
		WorkerID: "worker-t1",
		TaskID:   "t1",
		Seq:      3,
		TS:       now.Add(30 * time.Millisecond),
		Type:     EventTypeTaskArtifact,
		Payload: mustJSONRaw(map[string]any{
			"type":  "derived_command_output",
			"title": "zip metadata",
			"path":  "sessions/run-report-narrative/artifacts/zipinfo_verbose.txt",
		}),
	})
	append(EventEnvelope{
		EventID:  "e-t1-complete",
		RunID:    runID,
		WorkerID: "worker-t1",
		TaskID:   "t1",
		Seq:      4,
		TS:       now.Add(40 * time.Millisecond),
		Type:     EventTypeTaskCompleted,
		Payload: mustJSONRaw(map[string]any{
			"attempt":   1,
			"worker_id": "worker-t1",
			"reason":    "action_completed",
			"completion_contract": map[string]any{
				"produced_artifacts": []any{"sessions/run-report-narrative/artifacts/zipinfo_verbose.txt"},
			},
		}),
	})
	append(EventEnvelope{
		EventID:  "e-t2-start",
		RunID:    runID,
		WorkerID: "worker-t2",
		TaskID:   "t2",
		Seq:      1,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskStarted,
		Payload:  mustJSONRaw(map[string]any{"attempt": 1, "worker_id": "worker-t2"}),
	})
	append(EventEnvelope{
		EventID:  "e-t2-fail",
		RunID:    runID,
		WorkerID: "worker-t2",
		TaskID:   "t2",
		Seq:      2,
		TS:       now.Add(1200 * time.Millisecond),
		Type:     EventTypeTaskFailed,
		Payload: mustJSONRaw(map[string]any{
			"attempt":   1,
			"worker_id": "worker-t2",
			"reason":    WorkerFailureCommandFailed,
			"error":     "command exited with status 1",
		}),
	})

	reportPath, err := manager.AssembleRunReport(runID, "")
	if err != nil {
		t.Fatalf("AssembleRunReport: %v", err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "## Plan Overview") {
		t.Fatalf("expected plan overview section:\n%s", content)
	}
	if !strings.Contains(content, "## Execution Narrative") {
		t.Fatalf("expected execution narrative section:\n%s", content)
	}
	if !strings.Contains(content, "Recover password from local encrypted archive and report outcome") {
		t.Fatalf("expected objective in report:\n%s", content)
	}
	if !strings.Contains(content, "### `t1` Prepare archive metadata") {
		t.Fatalf("expected t1 narrative section:\n%s", content)
	}
	if !strings.Contains(content, "Outcome: `completed (action_completed)`") {
		t.Fatalf("expected completed task outcome in narrative:\n%s", content)
	}
	if !strings.Contains(content, "### `t2` Attempt password recovery") {
		t.Fatalf("expected t2 narrative section:\n%s", content)
	}
	if !strings.Contains(content, "Outcome: `command_failed: command exited with status 1`") {
		t.Fatalf("expected failed task outcome in narrative:\n%s", content)
	}
}

func TestResolveRunReportPathUsesDefaultAndGeneratedEvent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-report-path"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scan",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"scan.log"},
				RiskLevel:         "recon_readonly",
				Budget: TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
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
	defaultPath := filepath.Join(BuildRunPaths(base, runID).Root, "report.md")
	path, ready, err := manager.ResolveRunReportPath(runID)
	if err != nil {
		t.Fatalf("ResolveRunReportPath: %v", err)
	}
	if path != defaultPath || ready {
		t.Fatalf("expected unresolved default report path, got path=%s ready=%v", path, ready)
	}
	if err := os.WriteFile(defaultPath, []byte("# default report\n"), 0o644); err != nil {
		t.Fatalf("write default report: %v", err)
	}
	path, ready, err = manager.ResolveRunReportPath(runID)
	if err != nil {
		t.Fatalf("ResolveRunReportPath second call: %v", err)
	}
	if path != defaultPath || !ready {
		t.Fatalf("expected ready default report path, got path=%s ready=%v", path, ready)
	}
	customPath := filepath.Join(base, "custom-report.md")
	if err := os.WriteFile(customPath, []byte("# custom report\n"), 0o644); err != nil {
		t.Fatalf("write custom report: %v", err)
	}
	if err := manager.EmitEvent(runID, orchestratorWorkerID, "", EventTypeRunReportGenerated, map[string]any{
		"path": customPath,
	}); err != nil {
		t.Fatalf("emit run_report_generated: %v", err)
	}
	path, ready, err = manager.ResolveRunReportPath(runID)
	if err != nil {
		t.Fatalf("ResolveRunReportPath third call: %v", err)
	}
	if path != customPath || !ready {
		t.Fatalf("expected custom report path from event, got path=%s ready=%v", path, ready)
	}
}
