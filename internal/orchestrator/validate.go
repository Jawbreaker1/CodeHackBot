package orchestrator

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidPlan  = errors.New("invalid plan")
	ErrInvalidTask  = errors.New("invalid task")
	ErrInvalidEvent = errors.New("invalid event")
)

var validEventTypes = map[string]struct{}{
	EventTypeRunStarted:          {},
	EventTypeTaskLeased:          {},
	EventTypeTaskStarted:         {},
	EventTypeTaskProgress:        {},
	EventTypeTaskArtifact:        {},
	EventTypeTaskFinding:         {},
	EventTypeTaskFailed:          {},
	EventTypeTaskCompleted:       {},
	EventTypeWorkerStarted:       {},
	EventTypeWorkerHeartbeat:     {},
	EventTypeWorkerStopped:       {},
	EventTypeRunStopped:          {},
	EventTypeRunCompleted:        {},
	EventTypeRunStateUpdated:     {},
	EventTypeRunReplanRequested:  {},
	EventTypeApprovalRequested:   {},
	EventTypeApprovalGranted:     {},
	EventTypeApprovalDenied:      {},
	EventTypeApprovalExpired:     {},
	EventTypeWorkerStopRequested: {},
}

func ValidateRunPlan(plan RunPlan) error {
	if strings.TrimSpace(plan.RunID) == "" {
		return fmt.Errorf("%w: run_id is required", ErrInvalidPlan)
	}
	if len(plan.SuccessCriteria) == 0 {
		return fmt.Errorf("%w: success_criteria is required", ErrInvalidPlan)
	}
	if len(plan.StopCriteria) == 0 {
		return fmt.Errorf("%w: stop_criteria is required", ErrInvalidPlan)
	}
	if plan.MaxParallelism <= 0 {
		return fmt.Errorf("%w: max_parallelism must be > 0", ErrInvalidPlan)
	}
	if len(plan.Tasks) == 0 {
		return fmt.Errorf("%w: tasks is required", ErrInvalidPlan)
	}
	ids := map[string]struct{}{}
	for i, task := range plan.Tasks {
		if err := ValidateTaskSpec(task); err != nil {
			return fmt.Errorf("%w: task[%d]: %v", ErrInvalidPlan, i, err)
		}
		if _, seen := ids[task.TaskID]; seen {
			return fmt.Errorf("%w: duplicate task_id %q", ErrInvalidPlan, task.TaskID)
		}
		ids[task.TaskID] = struct{}{}
	}
	return nil
}

func ValidateTaskSpec(task TaskSpec) error {
	if strings.TrimSpace(task.TaskID) == "" {
		return fmt.Errorf("%w: task_id is required", ErrInvalidTask)
	}
	if strings.TrimSpace(task.Goal) == "" {
		return fmt.Errorf("%w: goal is required", ErrInvalidTask)
	}
	if len(task.DoneWhen) == 0 {
		return fmt.Errorf("%w: done_when is required", ErrInvalidTask)
	}
	if len(task.FailWhen) == 0 {
		return fmt.Errorf("%w: fail_when is required", ErrInvalidTask)
	}
	if len(task.ExpectedArtifacts) == 0 {
		return fmt.Errorf("%w: expected_artifacts is required", ErrInvalidTask)
	}
	if strings.TrimSpace(task.RiskLevel) == "" {
		return fmt.Errorf("%w: risk_level is required", ErrInvalidTask)
	}
	if _, err := ParseRiskTier(task.RiskLevel); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTask, err)
	}
	if task.Budget.MaxSteps <= 0 {
		return fmt.Errorf("%w: budget.max_steps must be > 0", ErrInvalidTask)
	}
	if task.Budget.MaxToolCalls <= 0 {
		return fmt.Errorf("%w: budget.max_tool_calls must be > 0", ErrInvalidTask)
	}
	if task.Budget.MaxRuntime <= 0 {
		return fmt.Errorf("%w: budget.max_runtime must be > 0", ErrInvalidTask)
	}
	return nil
}

func ValidateTaskLease(lease TaskLease) error {
	if strings.TrimSpace(lease.TaskID) == "" {
		return fmt.Errorf("%w: task_id is required", ErrInvalidTask)
	}
	if strings.TrimSpace(lease.LeaseID) == "" {
		return fmt.Errorf("%w: lease_id is required", ErrInvalidTask)
	}
	if strings.TrimSpace(lease.Status) == "" {
		return fmt.Errorf("%w: status is required", ErrInvalidTask)
	}
	switch lease.Status {
	case LeaseStatusQueued, LeaseStatusAwaitingApproval, LeaseStatusCompleted, LeaseStatusFailed, LeaseStatusBlocked, LeaseStatusCanceled:
		// WorkerID may be empty for terminal/requeued states.
	default:
		if strings.TrimSpace(lease.WorkerID) == "" {
			return fmt.Errorf("%w: worker_id is required", ErrInvalidTask)
		}
	}
	if lease.Attempt < 0 {
		return fmt.Errorf("%w: attempt must be >= 0", ErrInvalidTask)
	}
	if lease.StartedAt.IsZero() {
		return fmt.Errorf("%w: started_at is required", ErrInvalidTask)
	}
	if lease.Deadline.IsZero() {
		return fmt.Errorf("%w: deadline is required", ErrInvalidTask)
	}
	return nil
}

func ValidateEventEnvelope(event EventEnvelope) error {
	if strings.TrimSpace(event.EventID) == "" {
		return fmt.Errorf("%w: event_id is required", ErrInvalidEvent)
	}
	if strings.TrimSpace(event.RunID) == "" {
		return fmt.Errorf("%w: run_id is required", ErrInvalidEvent)
	}
	if strings.TrimSpace(event.WorkerID) == "" {
		return fmt.Errorf("%w: worker_id is required", ErrInvalidEvent)
	}
	if event.Seq <= 0 {
		return fmt.Errorf("%w: seq must be > 0", ErrInvalidEvent)
	}
	if event.TS.IsZero() {
		return fmt.Errorf("%w: ts is required", ErrInvalidEvent)
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		return fmt.Errorf("%w: type is required", ErrInvalidEvent)
	}
	if _, ok := validEventTypes[eventType]; !ok {
		return fmt.Errorf("%w: unknown type %q", ErrInvalidEvent, event.Type)
	}
	return nil
}
