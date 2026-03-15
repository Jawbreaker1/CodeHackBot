package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWorkerTaskValidatorRoleEmitsVerdicts(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-validator-role"
	taskID := "validator-001"
	workerID := "worker-validator-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	manager := NewManager(base)

	discoveryLog := filepath.Join(base, "sessions", runID, "logs", "discovery.log")
	if err := os.MkdirAll(filepath.Dir(discoveryLog), 0o755); err != nil {
		t.Fatalf("MkdirAll discovery log dir: %v", err)
	}
	if err := os.WriteFile(discoveryLog, []byte("service evidence\n"), 0o644); err != nil {
		t.Fatalf("WriteFile discovery log: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-discovery", "t-discovery", EventTypeTaskArtifact, map[string]any{
		"type":  "log",
		"title": "discovery log",
		"path":  discoveryLog,
	}); err != nil {
		t.Fatalf("EmitEvent artifact: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-discovery", "t-discovery", EventTypeTaskFinding, map[string]any{
		"target":       "127.0.0.1",
		"finding_type": "service_exposure",
		"title":        "HTTP service exposed",
		"location":     "80/tcp",
		"state":        FindingStateCandidate,
		"severity":     "medium",
		"confidence":   "medium",
		"source":       "discoverer",
		"evidence":     []string{"http open"},
	}); err != nil {
		t.Fatalf("EmitEvent candidate finding with evidence: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-discovery", "t-noevidence", EventTypeTaskFinding, map[string]any{
		"target":       "127.0.0.1",
		"finding_type": "tls_issue",
		"title":        "Potential weak TLS",
		"location":     "443/tcp",
		"state":        FindingStateCandidate,
		"severity":     "low",
		"confidence":   "low",
		"source":       "discoverer",
		"evidence":     []string{"weak hint"},
	}); err != nil {
		t.Fatalf("EmitEvent candidate finding without linked artifact: %v", err)
	}
	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence pre-validator: %v", err)
	}

	task := TaskSpec{
		TaskID:            taskID,
		Title:             "Read-only validator lane",
		Goal:              "Validate candidate findings from existing artifacts",
		Strategy:          "validator_readonly",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"validator completed"},
		FailWhen:          []string{"validator failed"},
		ExpectedArtifacts: []string{"validator_verdicts.json"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "definitely-should-not-run",
			Args:    []string{"--bad"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask validator: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	foundValidatorFinding := false
	for _, event := range events {
		if event.Type != EventTypeTaskFinding || event.TaskID != taskID {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if strings.TrimSpace(toString(payload["source"])) != "validator_worker" {
			continue
		}
		meta, _ := payload["metadata"].(map[string]any)
		if strings.TrimSpace(toString(meta["validator_verdict"])) == "" {
			continue
		}
		foundValidatorFinding = true
		break
	}
	if !foundValidatorFinding {
		t.Fatalf("expected validator verdict finding events")
	}

	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence post-validator: %v", err)
	}
	findings, err := manager.ListFindings(runID)
	if err != nil {
		t.Fatalf("ListFindings: %v", err)
	}
	stateByTitle := map[string]string{}
	verdictByTitle := map[string]string{}
	for _, finding := range findings {
		title := strings.TrimSpace(finding.Title)
		stateByTitle[title] = normalizeFindingState(finding.State)
		verdictByTitle[title] = strings.TrimSpace(finding.Metadata["validator_verdict"])
	}
	if stateByTitle["HTTP service exposed"] != FindingStateVerified {
		t.Fatalf("expected HTTP service finding verified by validator, got %q", stateByTitle["HTTP service exposed"])
	}
	if strings.ToLower(verdictByTitle["HTTP service exposed"]) != "verified" {
		t.Fatalf("expected validator verdict=verified, got %q", verdictByTitle["HTTP service exposed"])
	}
	if stateByTitle["Potential weak TLS"] != FindingStateRejected {
		t.Fatalf("expected weak TLS finding rejected by validator, got %q", stateByTitle["Potential weak TLS"])
	}
	if strings.ToLower(verdictByTitle["Potential weak TLS"]) != "rejected" {
		t.Fatalf("expected validator verdict=rejected, got %q", verdictByTitle["Potential weak TLS"])
	}
}

func TestRunWorkerTaskValidatorRoleRequiresReadOnlyRisk(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-validator-risk"
	taskID := "validator-002"
	workerID := "worker-validator-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "validator must be read-only",
		Strategy:          "validator_readonly",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"validator completed"},
		FailWhen:          []string{"validator failed"},
		ExpectedArtifacts: []string{"validator_verdicts.json"},
		RiskLevel:         string(RiskActiveProbe),
		Action: TaskAction{
			Type:    "command",
			Command: "echo",
			Args:    []string{"test"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err == nil {
		t.Fatalf("expected validator task with non-read-only risk to fail")
	}

	events, evErr := NewManager(base).Events(runID, 0)
	if evErr != nil {
		t.Fatalf("Events: %v", evErr)
	}
	failEvent, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failEvent.Payload) > 0 {
		_ = json.Unmarshal(failEvent.Payload, &payload)
	}
	if got := strings.TrimSpace(toString(payload["reason"])); got != WorkerFailurePolicyInvalid {
		t.Fatalf("expected failure reason %s, got %q", WorkerFailurePolicyInvalid, got)
	}
}
