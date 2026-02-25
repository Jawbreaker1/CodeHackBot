package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

type tuiAttentionTracker struct {
	runID             string
	sessionsDir       string
	pendingApprovalID map[string]struct{}
	hadPending        bool
	lastPlanPhase     string
}

func newTUIAttentionTracker(runID, sessionsDir string) *tuiAttentionTracker {
	return &tuiAttentionTracker{
		runID:             strings.TrimSpace(runID),
		sessionsDir:       strings.TrimSpace(sessionsDir),
		pendingApprovalID: map[string]struct{}{},
	}
}

func (t *tuiAttentionTracker) appendActionMessages(lines []string, snap tuiSnapshot) ([]string, bool) {
	out := append([]string{}, lines...)
	added := false
	if phaseLine := t.planActionLine(snap); phaseLine != "" {
		out = appendLogLines(out, phaseLine)
		added = true
	}
	approvalLines := t.approvalActionLines(snap.approvals)
	for _, line := range approvalLines {
		out = appendLogLines(out, line)
		added = true
	}
	return out, added
}

func (t *tuiAttentionTracker) planActionLine(snap tuiSnapshot) string {
	phase := orchestrator.NormalizeRunPhase(snap.plan.Metadata.RunPhase)
	switch phase {
	case orchestrator.RunPhasePlanning:
		if t.lastPlanPhase == phase {
			return ""
		}
		t.lastPlanPhase = phase
		return "action required: run is in planning phase. Use `instruct <text>` or `task ...` to shape the plan, then `execute`."
	case orchestrator.RunPhaseReview:
		if t.lastPlanPhase == phase {
			return ""
		}
		t.lastPlanPhase = phase
		return "action required: run is waiting in review phase. Use `execute` to launch, or `regenerate` / `discard` / `task ...` to revise."
	default:
		t.lastPlanPhase = ""
		return ""
	}
}

func (t *tuiAttentionTracker) approvalActionLines(pending []orchestrator.PendingApprovalView) []string {
	if len(pending) == 0 {
		t.pendingApprovalID = map[string]struct{}{}
		if t.hadPending {
			t.hadPending = false
			return []string{"pending approvals cleared; run can continue."}
		}
		return nil
	}

	lines := make([]string, 0, len(pending))
	ids := make([]string, 0, len(pending))
	viewByID := make(map[string]orchestrator.PendingApprovalView, len(pending))
	nextPending := make(map[string]struct{}, len(pending))
	for _, req := range pending {
		id := strings.TrimSpace(req.ApprovalID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
		viewByID[id] = req
		nextPending[id] = struct{}{}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, seen := t.pendingApprovalID[id]; seen {
			continue
		}
		req := viewByID[id]
		taskLabel := strings.TrimSpace(req.TaskID)
		if title := strings.TrimSpace(req.TaskTitle); title != "" {
			taskLabel = fmt.Sprintf("%s (%s)", taskLabel, title)
		}
		goal := strings.TrimSpace(req.TaskGoal)
		if goal == "" {
			goal = "-"
		}
		lines = append(lines, fmt.Sprintf(
			"approval required: id=%s task=%s risk=%s goal=%q why=%q",
			id,
			taskLabel,
			strings.TrimSpace(req.RiskTier),
			truncateApprovalText(goal, 120),
			truncateApprovalText(approvalWhyText(req), 140),
		))
	}
	t.pendingApprovalID = nextPending
	t.hadPending = true
	return lines
}

func truncateApprovalText(value string, maxLen int) string {
	trimmed := strings.TrimSpace(value)
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxLen {
		return trimmed
	}
	if maxLen <= 1 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-1]) + "…"
}
