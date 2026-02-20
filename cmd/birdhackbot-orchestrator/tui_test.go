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
		{in: "ask what is current status", name: "ask"},
		{in: "instruct investigate target", name: "instruct"},
		{in: "Hello orchestrator", name: "instruct"},
		{in: "how many workers are running?", name: "ask"},
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
		"ask",
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

func TestLooksLikeQuestion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want bool
	}{
		{in: "how many workers are running?", want: true},
		{in: "What is the current plan", want: true},
		{in: "can you summarize", want: true},
		{in: "scan the network now", want: false},
		{in: "resume execution", want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeQuestion(tc.in); got != tc.want {
				t.Fatalf("looksLikeQuestion(%q)=%v want %v", tc.in, got, tc.want)
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

func TestLatestWorkerDebugByWorker(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []orchestrator.EventEnvelope{
		{
			EventID:  "e1",
			RunID:    "run-1",
			WorkerID: "signal-worker-a",
			TaskID:   "task-1",
			Seq:      1,
			TS:       now,
			Type:     orchestrator.EventTypeTaskProgress,
			Payload:  mustPayload(t, map[string]any{"message": "running nmap", "step": 2, "tool_calls": 1}),
		},
		{
			EventID:  "e2",
			RunID:    "run-1",
			WorkerID: "signal-worker-a",
			TaskID:   "task-1",
			Seq:      2,
			TS:       now.Add(time.Second),
			Type:     orchestrator.EventTypeTaskArtifact,
			Payload:  mustPayload(t, map[string]any{"command": "nmap", "args": []string{"-sV", "192.168.50.10"}, "path": "sessions/run-1/logs/nmap.log"}),
		},
	}

	debug := latestWorkerDebugByWorker(events)
	snapshot, ok := debug["worker-a"]
	if !ok {
		t.Fatalf("expected normalized worker key")
	}
	if snapshot.Command != "nmap" {
		t.Fatalf("unexpected command: %s", snapshot.Command)
	}
	if len(snapshot.Args) != 2 || snapshot.Args[0] != "-sV" {
		t.Fatalf("unexpected args: %#v", snapshot.Args)
	}
	if snapshot.LogPath != "sessions/run-1/logs/nmap.log" {
		t.Fatalf("unexpected log path: %s", snapshot.LogPath)
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

func TestClampTUIBodyLinesKeepsTopBar(t *testing.T) {
	t.Parallel()

	lines := []string{"bar", "updated", "", "plan", "workers", "events", "log"}
	clamped := clampTUIBodyLines(lines, 5)
	if len(clamped) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(clamped))
	}
	if clamped[0] != "bar" || clamped[1] != "updated" || clamped[2] != "" {
		t.Fatalf("expected top header to be preserved, got %#v", clamped[:3])
	}
}

func TestRenderTwoPaneWide(t *testing.T) {
	t.Parallel()

	layout := computePaneLayout(140)
	if !layout.enabled {
		t.Fatalf("expected two-pane layout to be enabled")
	}
	left := []string{"Plan:", "  - Goal: demo"}
	right := []string{"Worker Debug:", "+-box-+"}
	lines := renderTwoPane(left, right, 140, layout)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Plan:") || !strings.Contains(lines[0], "Worker Debug:") {
		t.Fatalf("expected left/right content on same row, got %q", lines[0])
	}
}

func TestRenderTwoPaneNarrowFallsBackToStack(t *testing.T) {
	t.Parallel()

	layout := computePaneLayout(80)
	if layout.enabled {
		t.Fatalf("expected two-pane layout to be disabled for narrow widths")
	}
	left := []string{"Plan:"}
	right := []string{"Worker Debug:"}
	lines := renderTwoPane(left, right, 80, layout)
	if len(lines) < 3 {
		t.Fatalf("expected stacked output with spacer, got %d lines", len(lines))
	}
	if !strings.Contains(lines[0], "Plan:") {
		t.Fatalf("expected left content first, got %q", lines[0])
	}
	if !strings.Contains(lines[2], "Worker Debug:") {
		t.Fatalf("expected right content after spacer, got %q", lines[2])
	}
}

func TestRenderTwoPaneCapsRightPaneHeight(t *testing.T) {
	t.Parallel()

	layout := computePaneLayout(140)
	if !layout.enabled {
		t.Fatalf("expected two-pane layout to be enabled")
	}
	left := []string{"Plan:", "Execution:", "Task Board:"}
	right := []string{"Worker Debug:", "box1", "box2", "box3", "box4"}
	lines := renderTwoPane(left, right, 140, layout)
	if len(lines) != len(left) {
		t.Fatalf("expected lines to match left pane height (%d), got %d", len(left), len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "...") {
		t.Fatalf("expected truncated marker in last row, got %q", lines[len(lines)-1])
	}
}

func TestRenderWorkerBoxesOrdersActiveBeforeStopped(t *testing.T) {
	t.Parallel()

	workers := []orchestrator.WorkerStatus{
		{
			WorkerID:  "worker-stopped",
			State:     "stopped",
			LastEvent: time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		},
		{
			WorkerID:  "worker-active",
			State:     "active",
			LastEvent: time.Date(2026, 2, 20, 10, 1, 0, 0, time.UTC),
		},
	}

	lines := renderWorkerBoxes(workers, map[string]tuiWorkerDebug{}, 60)
	idxActive := -1
	idxStopped := -1
	for i, line := range lines {
		if strings.Contains(line, "Worker worker-active") {
			idxActive = i
		}
		if strings.Contains(line, "Worker worker-stopped") {
			idxStopped = i
		}
	}
	if idxActive < 0 || idxStopped < 0 {
		t.Fatalf("expected both worker titles in output")
	}
	if idxActive >= idxStopped {
		t.Fatalf("expected active worker box before stopped worker box (active=%d stopped=%d)", idxActive, idxStopped)
	}
}

func TestAnswerTUIQuestionPlan(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-ask-plan"
	manager := orchestrator.NewManager(base)
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Networks: []string{"192.168.50.0/24"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "task-1",
				Title:             "Seed reconnaissance",
				Goal:              "collect baseline",
				DoneWhen:          []string{"baseline"},
				FailWhen:          []string{"failed"},
				ExpectedArtifacts: []string{"seed.log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget:            orchestrator.TaskBudget{MaxSteps: 2, MaxToolCalls: 2, MaxRuntime: time.Minute},
			},
		},
		Metadata: orchestrator.PlanMetadata{
			Goal: "Map network",
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	answer := answerTUIQuestion(manager, runID, "what is the complete plan?")
	if !strings.Contains(answer, "plan (1 steps):") {
		t.Fatalf("unexpected answer: %s", answer)
	}
	if !strings.Contains(answer, "1)Seed reconnaissance") {
		t.Fatalf("expected step title in answer: %s", answer)
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
