package main

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestTUIAttentionTrackerApprovalMessages(t *testing.T) {
	t.Parallel()

	tracker := newTUIAttentionTracker("run-123", "sessions")
	snap := tuiSnapshot{
		approvals: []orchestrator.PendingApprovalView{
			{
				ApprovalID: "apr-1",
				TaskID:     "task-h01",
				TaskTitle:  "Validate web exposure",
				RiskTier:   string(orchestrator.RiskActiveProbe),
				Reason:     "requires approval",
			},
		},
	}

	lines, added := tracker.appendActionMessages(nil, snap)
	if !added {
		t.Fatalf("expected approval action message")
	}
	log := strings.Join(lines, "\n")
	if !strings.Contains(log, "approval required: id=apr-1 task=task-h01 (Validate web exposure)") {
		t.Fatalf("expected task context in approval log, got %q", log)
	}
	if !strings.Contains(log, "why=\"requires approval (active probing requires operator approval in default permission mode)\"") {
		t.Fatalf("expected approval reason in log, got %q", log)
	}

	lines, added = tracker.appendActionMessages(lines, snap)
	if added {
		t.Fatalf("expected no duplicate approval notifications")
	}

	lines, added = tracker.appendActionMessages(lines, tuiSnapshot{})
	if !added {
		t.Fatalf("expected approvals cleared notification")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "pending approvals cleared; run can continue.") {
		t.Fatalf("expected approvals cleared guidance")
	}
}

func TestTUIAttentionTrackerPlanningPhaseMessages(t *testing.T) {
	t.Parallel()

	tracker := newTUIAttentionTracker("run-456", "sessions")
	snapPlanning := tuiSnapshot{}
	snapPlanning.plan.Metadata.RunPhase = orchestrator.RunPhasePlanning

	lines, added := tracker.appendActionMessages(nil, snapPlanning)
	if !added {
		t.Fatalf("expected planning phase action guidance")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "run is in planning phase") {
		t.Fatalf("expected planning guidance in command log")
	}

	lines, added = tracker.appendActionMessages(lines, snapPlanning)
	if added {
		t.Fatalf("expected no repeated planning guidance")
	}

	snapReview := tuiSnapshot{}
	snapReview.plan.Metadata.RunPhase = orchestrator.RunPhaseReview
	lines, added = tracker.appendActionMessages(lines, snapReview)
	if !added {
		t.Fatalf("expected review phase guidance")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "waiting in review phase") {
		t.Fatalf("expected review guidance in command log")
	}
}
