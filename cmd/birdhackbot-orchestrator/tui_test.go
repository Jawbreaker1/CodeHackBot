package main

import (
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
		"wat",
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

func hasEvent(events []orchestrator.EventEnvelope, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
