package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

type sequenceWorkerAssistant struct {
	seq   []assist.Suggestion
	index int
}

func (s *sequenceWorkerAssistant) Suggest(_ context.Context, _ assist.Input) (assist.Suggestion, workerAssistantTurnMeta, error) {
	if len(s.seq) == 0 {
		return assist.Suggestion{Type: "complete", Final: "done"}, workerAssistantTurnMeta{}, nil
	}
	if s.index >= len(s.seq) {
		return s.seq[len(s.seq)-1], workerAssistantTurnMeta{}, nil
	}
	out := s.seq[s.index]
	s.index++
	return out, workerAssistantTurnMeta{}, nil
}

func TestRunWorkerTaskAssistCommandScopeValidationDoesNotInjectTargetForAssistTask(t *testing.T) {
	base := t.TempDir()
	runID := "run-assist-scope-sync"
	taskID := "T-SCOPE-SYNC"
	workerID := "worker-T-SCOPE-SYNC-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{
		Networks: []string{"127.0.0.0/8"},
		Targets:  []string{"127.0.0.1"},
	})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "discover local host quickly",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "run host discovery",
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   15 * time.Second,
		},
	})

	binDir := filepath.Join(base, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll binDir: %v", err)
	}
	nmapPath := filepath.Join(binDir, "nmap")
	nmapScript := "#!/usr/bin/env bash\nset -euo pipefail\necho \"nmap_stub args:$*\"\nexit 0\n"
	if err := os.WriteFile(nmapPath, []byte(nmapScript), 0o755); err != nil {
		t.Fatalf("WriteFile nmap script: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	missingList := filepath.Join(BuildRunPaths(base, runID).Root, "workers", workerID, "targets.txt")
	assistant := &sequenceWorkerAssistant{
		seq: []assist.Suggestion{
			{
				Type:    "command",
				Command: "nmap",
				Args:    []string{"-sn", "-n", "--disable-arp-ping", "-iL", missingList},
				Summary: "perform host discovery scan",
			},
			{
				Type:  "complete",
				Final: "done",
			},
		},
	}

	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
		assistantBuilder: func() (string, string, workerAssistant, error) {
			return "test-model", "strict", assistant, nil
		},
	}
	if err := RunWorkerTask(cfg); err == nil {
		t.Fatalf("expected scope validation failure for assist task without injected target")
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	scopeDenied := false
	progressFound := false
	executedCommand := false
	for _, event := range events {
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if event.Type == EventTypeTaskFailed {
			reason := strings.TrimSpace(toString(payload["reason"]))
			if reason == WorkerFailureScopeDenied {
				scopeDenied = true
			}
		}
		if event.Type == EventTypeTaskProgress {
			msg := strings.ToLower(strings.TrimSpace(toString(payload["message"])))
			if strings.Contains(msg, "auto-injected target 127.0.0.1 for command nmap after runtime adaptation") {
				progressFound = true
			}
		}
		if event.Type == EventTypeTaskArtifact && strings.TrimSpace(toString(payload["type"])) == "command_log" {
			command := strings.TrimSpace(toString(payload["command"]))
			if command != "nmap" {
				continue
			}
			executedCommand = true
		}
	}
	if !scopeDenied {
		t.Fatalf("expected assist task to fail scope validation without injected target")
	}
	if progressFound {
		t.Fatalf("did not expect runtime adaptation progress event for reinjected target")
	}
	if executedCommand {
		t.Fatalf("did not expect nmap command to execute after scope validation failure")
	}
}
