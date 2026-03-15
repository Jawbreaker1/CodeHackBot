package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-command-repair-host"
	taskID := "t-repair-host"
	workerID := "worker-repair-host-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	toolDir := filepath.Join(base, "bin")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("MkdirAll toolDir: %v", err)
	}
	toolPath := filepath.Join(toolDir, "hostscan")
	toolScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  echo "hostscan -host <target>"
  exit 0
fi
if [[ "${1:-}" == "-host" && -n "${2:-}" ]]; then
  echo "scan-ok host=$2"
  exit 0
fi
echo "+ ERROR: No host (-host) specified"
echo "Options:"
echo "  -host+ target host"
exit 2
`
	if err := os.WriteFile(toolPath, []byte(toolScript), 0o755); err != nil {
		t.Fatalf("WriteFile tool script: %v", err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "run host scanner",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"scan complete"},
		FailWhen:          []string{"scan failed"},
		ExpectedArtifacts: []string{"scan.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "hostscan",
			Args:    []string{"127.0.0.1"},
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   30 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
		assistantBuilder: func() (string, string, workerAssistant, error) {
			return "", "", nil, fmt.Errorf("assistant intentionally unavailable for test")
		},
	}
	if err := RunWorkerTask(cfg); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	manager := NewManager(base)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if hasEventType(events, EventTypeTaskFailed) {
		t.Fatalf("did not expect task_failed after inferred command contract repair")
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed after inferred command contract repair")
	}

	progressFound := false
	var commandLogPath string
	for _, event := range events {
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if event.Type == EventTypeTaskProgress {
			msg := strings.ToLower(strings.TrimSpace(toString(payload["message"])))
			if strings.Contains(msg, "inferred required option -host") {
				progressFound = true
			}
		}
		if event.Type == EventTypeTaskArtifact && strings.TrimSpace(toString(payload["type"])) == "command_log" {
			commandLogPath = strings.TrimSpace(toString(payload["path"]))
		}
	}
	if !progressFound {
		t.Fatalf("expected inferred repair progress event")
	}
	if commandLogPath == "" {
		t.Fatalf("expected command log artifact path")
	}
	logData, err := os.ReadFile(commandLogPath)
	if err != nil {
		t.Fatalf("ReadFile command log: %v", err)
	}
	content := string(logData)
	if !strings.Contains(content, "No host (-host) specified") {
		t.Fatalf("expected original usage error in merged command log, got %q", content)
	}
	if !strings.Contains(content, "scan-ok host=127.0.0.1") {
		t.Fatalf("expected inferred repair success output in merged command log, got %q", content)
	}
}

func TestInferRequiredTargetOptionFromOutput(t *testing.T) {
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"127.0.0.1"}})
	task := TaskSpec{Targets: []string{"127.0.0.1"}}

	flag, target, ok := inferRequiredTargetOptionFromOutput(task, scopePolicy, "hostscan", []string{"127.0.0.1"}, []byte("+ ERROR: No host (-host) specified"))
	if !ok {
		t.Fatalf("expected target option inference")
	}
	if flag != "-host" {
		t.Fatalf("expected -host flag, got %q", flag)
	}
	if target != "127.0.0.1" {
		t.Fatalf("expected target 127.0.0.1, got %q", target)
	}

	if _, _, ok := inferRequiredTargetOptionFromOutput(task, scopePolicy, "filescan", []string{"sample.txt"}, []byte("ERROR: No file (-f) specified")); ok {
		t.Fatalf("did not expect target option inference for non-target option")
	}
}
