package orchestrator

import (
	"encoding/json"
	"fmt"
	"time"
)

type PendingApprovalView struct {
	ApprovalID   string    `json:"approval_id"`
	TaskID       string    `json:"task_id"`
	TaskTitle    string    `json:"task_title,omitempty"`
	TaskGoal     string    `json:"task_goal,omitempty"`
	TaskStrategy string    `json:"task_strategy,omitempty"`
	TaskTargets  []string  `json:"task_targets,omitempty"`
	RiskTier     string    `json:"risk_tier"`
	Reason       string    `json:"reason"`
	Requested    time.Time `json:"requested"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (m *Manager) PendingApprovals(runID string) ([]PendingApprovalView, error) {
	events, err := m.Events(runID, 0)
	if err != nil {
		return nil, err
	}
	taskByID := map[string]TaskSpec{}
	if plan, planErr := m.LoadRunPlan(runID); planErr == nil {
		for _, task := range plan.Tasks {
			taskByID[task.TaskID] = task
		}
	}
	pending := map[string]PendingApprovalView{}
	for _, event := range events {
		switch event.Type {
		case EventTypeApprovalRequested:
			payload := map[string]any{}
			if len(event.Payload) > 0 {
				_ = json.Unmarshal(event.Payload, &payload)
			}
			id, _ := payload["approval_id"].(string)
			if id == "" {
				continue
			}
			expires := event.TS
			if raw, ok := payload["expires_at"].(string); ok {
				if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
					expires = parsed
				}
			}
			if raw, ok := payload["expires_at"].(time.Time); ok {
				expires = raw
			}
			view := PendingApprovalView{
				ApprovalID: id,
				TaskID:     event.TaskID,
				RiskTier:   toString(payload["tier"]),
				Reason:     toString(payload["reason"]),
				Requested:  event.TS,
				ExpiresAt:  expires,
			}
			if task, ok := taskByID[event.TaskID]; ok {
				view.TaskTitle = task.Title
				view.TaskGoal = task.Goal
				view.TaskStrategy = task.Strategy
				view.TaskTargets = append([]string{}, task.Targets...)
			}
			pending[id] = view
		case EventTypeApprovalGranted, EventTypeApprovalDenied, EventTypeApprovalExpired:
			payload := map[string]any{}
			if len(event.Payload) > 0 {
				_ = json.Unmarshal(event.Payload, &payload)
			}
			id, _ := payload["approval_id"].(string)
			delete(pending, id)
		}
	}
	out := make([]PendingApprovalView, 0, len(pending))
	for _, req := range pending {
		out = append(out, req)
	}
	return out, nil
}

func (m *Manager) SubmitApprovalDecision(runID, approvalID string, approve bool, scope, actor, reason string, expiresIn time.Duration) error {
	if approvalID == "" {
		return fmt.Errorf("approval id is required")
	}
	eventType := EventTypeApprovalDenied
	if approve {
		eventType = EventTypeApprovalGranted
	}
	payload := map[string]any{
		"approval_id": approvalID,
		"actor":       actor,
		"reason":      reason,
		"scope":       scope,
		"source":      "cli",
	}
	if expiresIn > 0 {
		payload["expires_in_seconds"] = int(expiresIn.Seconds())
	}
	workerID := fmt.Sprintf("operator-approval-%d", m.Now().UnixNano())
	return m.EmitEvent(runID, workerID, "", eventType, payload)
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}
