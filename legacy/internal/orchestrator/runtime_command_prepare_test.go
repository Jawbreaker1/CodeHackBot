package orchestrator

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRuntimeCommandCapturesInitialInjection(t *testing.T) {
	t.Parallel()

	policy := NewScopePolicy(Scope{Networks: []string{"192.168.50.0/24"}})
	task := TaskSpec{Targets: []string{"192.168.50.0/24"}}

	prepared := prepareRuntimeCommand(policy, task, "nmap", []string{"-sV"})

	if !prepared.InjectedInitialTarget {
		t.Fatalf("expected initial target injection")
	}
	if prepared.InitialTarget != "192.168.50.0/24" {
		t.Fatalf("unexpected initial target: %q", prepared.InitialTarget)
	}
	if prepared.InjectedFollowupTarget {
		t.Fatalf("did not expect follow-up target injection")
	}
	if len(prepared.Args) == 0 || prepared.Args[len(prepared.Args)-1] != "192.168.50.0/24" {
		t.Fatalf("unexpected args: %#v", prepared.Args)
	}
}

func TestPrepareRuntimeCommandCapturesFollowupInjectionAfterAdaptation(t *testing.T) {
	t.Parallel()

	policy := NewScopePolicy(Scope{Networks: []string{"127.0.0.0/8"}})
	task := TaskSpec{Targets: []string{"127.0.0.1"}}
	missingList := filepath.Join(t.TempDir(), "targets.txt")

	prepared := prepareRuntimeCommand(policy, task, "nmap", []string{
		"-sn", "-n", "--disable-arp-ping", "-oN", "nmap_scan_results.nmap", "-iL", missingList,
	})

	if prepared.InjectedInitialTarget {
		t.Fatalf("did not expect initial target injection with -iL present")
	}
	if !prepared.Adapted {
		t.Fatalf("expected adaptation for missing -iL file")
	}
	if !strings.Contains(prepared.AdaptationNote, "removed missing nmap -iL file") {
		t.Fatalf("unexpected adaptation note: %q", prepared.AdaptationNote)
	}
	if !prepared.InjectedFollowupTarget {
		t.Fatalf("expected follow-up target injection")
	}
	if prepared.FollowupTarget != "127.0.0.1" {
		t.Fatalf("unexpected follow-up target: %q", prepared.FollowupTarget)
	}
	if err := policy.ValidateCommandTargets(prepared.Command, prepared.Args); err != nil {
		t.Fatalf("expected prepared command to pass scope validation: %v", err)
	}
}

func TestRuntimePreparationMessagesOrder(t *testing.T) {
	t.Parallel()

	prepared := runtimeCommandPreparation{
		Command:                "nmap",
		InjectedInitialTarget:  true,
		InitialTarget:          "127.0.0.1",
		Adapted:                true,
		AdaptationNote:         "removed missing nmap -iL file: /tmp/targets.txt",
		InjectedFollowupTarget: true,
		FollowupTarget:         "127.0.0.1",
	}

	got := runtimePreparationMessages(prepared)
	want := []string{
		"auto-injected target 127.0.0.1 for command nmap",
		"removed missing nmap -iL file: /tmp/targets.txt",
		"auto-injected target 127.0.0.1 for command nmap after runtime adaptation",
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected message count: got=%d want=%d messages=%v", len(got), len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("unexpected message[%d]: got=%q want=%q (all=%v)", idx, got[idx], want[idx], got)
		}
	}
}

func TestEmitRuntimePreparationProgressPayloadFields(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-runtime-prepare-progress"
	taskID := "task-prepare-progress"
	workerID := "signal-worker-prepare-progress"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	manager := NewManager(base)

	emitRuntimePreparationProgress(manager, runID, workerID, taskID, 0, 0, 0, "", "", "nmap", []string{"127.0.0.1"}, []string{"msg-default"})
	emitRuntimePreparationProgress(manager, runID, workerID, taskID, 1, 2, 3, "tool_calls", "execute-step", "nmap", []string{"127.0.0.1"}, []string{"msg-assist"})

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	progressEvents := make([]EventEnvelope, 0, 2)
	for _, event := range events {
		if event.Type == EventTypeTaskProgress {
			progressEvents = append(progressEvents, event)
		}
	}
	if len(progressEvents) != 2 {
		t.Fatalf("expected 2 task_progress events, got %d", len(progressEvents))
	}

	defaultPayload := map[string]any{}
	if err := json.Unmarshal(progressEvents[0].Payload, &defaultPayload); err != nil {
		t.Fatalf("unmarshal default payload: %v", err)
	}
	if _, ok := defaultPayload["tool_call"]; !ok {
		t.Fatalf("expected default payload to contain tool_call: %#v", defaultPayload)
	}
	if _, ok := defaultPayload["tool_calls"]; ok {
		t.Fatalf("did not expect default payload to contain tool_calls: %#v", defaultPayload)
	}
	if _, ok := defaultPayload["turn"]; ok {
		t.Fatalf("did not expect default payload to contain turn: %#v", defaultPayload)
	}
	if _, ok := defaultPayload["mode"]; ok {
		t.Fatalf("did not expect default payload to contain mode: %#v", defaultPayload)
	}

	assistPayload := map[string]any{}
	if err := json.Unmarshal(progressEvents[1].Payload, &assistPayload); err != nil {
		t.Fatalf("unmarshal assist payload: %v", err)
	}
	if _, ok := assistPayload["tool_calls"]; !ok {
		t.Fatalf("expected assist payload to contain tool_calls: %#v", assistPayload)
	}
	if _, ok := assistPayload["tool_call"]; ok {
		t.Fatalf("did not expect assist payload to contain tool_call: %#v", assistPayload)
	}
	if got := toString(assistPayload["mode"]); got != "execute-step" {
		t.Fatalf("unexpected mode: %q", got)
	}
	turn, ok := assistPayload["turn"].(float64)
	if !ok || int(turn) != 2 {
		t.Fatalf("unexpected turn payload: %#v", assistPayload["turn"])
	}
}
