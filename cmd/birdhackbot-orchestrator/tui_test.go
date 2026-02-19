package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestParseTUICommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		name string
	}{
		{in: "", name: "refresh"},
		{in: "help", name: "help"},
		{in: "plan", name: "plan"},
		{in: "tasks", name: "tasks"},
		{in: "instruct investigate target", name: "instruct"},
		{in: "Hello orchestrator", name: "instruct"},
		{in: "q", name: "quit"},
		{in: "events 25", name: "events"},
		{in: "approve apr-1 task ok", name: "approve"},
		{in: "deny apr-2 nope", name: "deny"},
		{in: "stop", name: "stop"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			cmd, err := parseTUICommand(tc.in)
			if err != nil {
				t.Fatalf("parseTUICommand returned error: %v", err)
			}
			if cmd.name != tc.name {
				t.Fatalf("unexpected name: got %s want %s", cmd.name, tc.name)
			}
		})
	}
}

func TestParseTUICommandRejectsInvalid(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"events nope",
		"approve",
		"deny",
		"instruct",
	}
	for _, in := range invalid {
		in := in
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			if _, err := parseTUICommand(in); err == nil {
				t.Fatalf("expected parse error for %q", in)
			}
		})
	}
}

func TestExecuteTUICommandApproveDenyStop(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-exec"
	manager := orchestrator.NewManager(base)
	if _, err := orchestrator.EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunStarted, map[string]any{
		"source": "test",
	}); err != nil {
		t.Fatalf("EmitEvent run_started: %v", err)
	}

	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "approve", approval: "apr-1", scope: "task", reason: "ok"}); done || log == "" {
		t.Fatalf("expected approve command to continue with log, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "deny", approval: "apr-2", reason: "no"}); done || log == "" {
		t.Fatalf("expected deny command to continue with log, got done=%v log=%q", done, log)
	}
	if done, _ := executeTUICommand(manager, runID, nil, tuiCommand{name: "stop"}); done {
		t.Fatalf("expected stop to keep tui open")
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: "enumerate open ports"}); done || !strings.Contains(log, "instruction queued") {
		t.Fatalf("expected instruct command to queue instruction, got done=%v log=%q", done, log)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEvent(events, orchestrator.EventTypeApprovalGranted) {
		t.Fatalf("expected approval_granted event")
	}
	if !hasEvent(events, orchestrator.EventTypeApprovalDenied) {
		t.Fatalf("expected approval_denied event")
	}
	if !hasEvent(events, orchestrator.EventTypeRunStopped) {
		t.Fatalf("expected run_stopped event")
	}
	if !hasEvent(events, orchestrator.EventTypeOperatorInstruction) {
		t.Fatalf("expected operator_instruction event")
	}
}

func TestWaitForApprovalID(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-approval-wait"
	manager := orchestrator.NewManager(base)
	if _, err := orchestrator.EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = manager.EmitEvent(runID, "orchestrator", "t1", orchestrator.EventTypeApprovalRequested, map[string]any{
			"approval_id": "apr-wait-1",
			"tier":        "active_probe",
			"reason":      "test",
			"expires_at":  time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		})
	}()

	id := waitForApprovalID(t, manager, runID, 2*time.Second)
	if id != "apr-wait-1" {
		t.Fatalf("unexpected approval id: %s", id)
	}
}

func TestLatestFailureFromEvents_TaskFailed(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []orchestrator.EventEnvelope{
		{
			EventID:  "e1",
			RunID:    "run-1",
			WorkerID: "worker-a",
			TaskID:   "task-a",
			Seq:      1,
			TS:       now,
			Type:     orchestrator.EventTypeTaskFailed,
			Payload:  mustPayload(t, map[string]any{"reason": "assist_budget_exhausted", "error": "exhausted max steps", "log_path": "/tmp/worker.log"}),
		},
	}

	failure := latestFailureFromEvents(events)
	if failure == nil {
		t.Fatalf("expected failure")
	}
	if failure.Reason != "assist_budget_exhausted" {
		t.Fatalf("unexpected reason: %s", failure.Reason)
	}
	if failure.Error != "exhausted max steps" {
		t.Fatalf("unexpected error: %s", failure.Error)
	}
	if failure.LogPath != "/tmp/worker.log" {
		t.Fatalf("unexpected log path: %s", failure.LogPath)
	}
}

func TestLatestFailureFromEvents_WorkerStoppedFallback(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []orchestrator.EventEnvelope{
		{
			EventID:  "e1",
			RunID:    "run-1",
			WorkerID: "worker-a",
			Seq:      2,
			TS:       now,
			Type:     orchestrator.EventTypeWorkerStopped,
			Payload:  mustPayload(t, map[string]any{"status": "failed", "error": "exit status 1", "log_path": "/tmp/worker.log"}),
		},
	}

	failure := latestFailureFromEvents(events)
	if failure == nil {
		t.Fatalf("expected failure")
	}
	if failure.Reason != "worker_stopped_failed" {
		t.Fatalf("unexpected reason: %s", failure.Reason)
	}
	if failure.Error != "exit status 1" {
		t.Fatalf("unexpected error: %s", failure.Error)
	}
}

func TestLatestTaskProgressByTask(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []orchestrator.EventEnvelope{
		{
			EventID:  "e1",
			RunID:    "run-1",
			WorkerID: "worker-a",
			TaskID:   "task-1",
			Seq:      1,
			TS:       now,
			Type:     orchestrator.EventTypeTaskStarted,
			Payload:  mustPayload(t, map[string]any{"goal": "seed recon"}),
		},
		{
			EventID:  "e2",
			RunID:    "run-1",
			WorkerID: "worker-a",
			TaskID:   "task-1",
			Seq:      2,
			TS:       now.Add(2 * time.Second),
			Type:     orchestrator.EventTypeTaskProgress,
			Payload:  mustPayload(t, map[string]any{"step": 2, "tool_calls": 3, "message": "running nmap"}),
		},
		{
			EventID:  "e3",
			RunID:    "run-1",
			WorkerID: "worker-b",
			TaskID:   "task-2",
			Seq:      1,
			TS:       now.Add(time.Second),
			Type:     orchestrator.EventTypeTaskProgress,
			Payload:  mustPayload(t, map[string]any{"steps": 1, "tool_call": 1, "type": "plan"}),
		},
	}

	progress := latestTaskProgressByTask(events)
	task1, ok := progress["task-1"]
	if !ok {
		t.Fatalf("expected task-1 progress")
	}
	if task1.Step != 2 || task1.ToolCalls != 3 || task1.Message != "running nmap" {
		t.Fatalf("unexpected task-1 progress: %#v", task1)
	}

	task2, ok := progress["task-2"]
	if !ok {
		t.Fatalf("expected task-2 progress")
	}
	if task2.Step != 1 || task2.ToolCalls != 1 || task2.Message != "plan" {
		t.Fatalf("unexpected task-2 progress: %#v", task2)
	}
}

func TestFormatProgressSummary(t *testing.T) {
	t.Parallel()

	got := formatProgressSummary(tuiTaskProgress{
		Step:      3,
		ToolCalls: 5,
		Message:   "enumerating services",
	})
	want := "step 3 | enumerating services | tools:5"
	if got != want {
		t.Fatalf("unexpected summary: got %q want %q", got, want)
	}
}

func mustPayload(t *testing.T, payload map[string]any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}

func hasEvent(events []orchestrator.EventEnvelope, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
