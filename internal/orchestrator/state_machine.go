package orchestrator

import (
	"fmt"
	"strings"
)

type TaskState string

const (
	TaskStateQueued           TaskState = "queued"
	TaskStateLeased           TaskState = "leased"
	TaskStateRunning          TaskState = "running"
	TaskStateAwaitingApproval TaskState = "awaiting_approval"
	TaskStateCompleted        TaskState = "completed"
	TaskStateFailed           TaskState = "failed"
	TaskStateBlocked          TaskState = "blocked"
	TaskStateCanceled         TaskState = "canceled"
)

var blockedReasons = map[string]struct{}{
	"approval_timeout": {},
	"approval_denied":  {},
	"missing_prereq":   {},
	"scope_denied":     {},
}

var allowedTransitions = map[TaskState]map[TaskState]struct{}{
	TaskStateQueued: {
		TaskStateLeased:   {},
		TaskStateCanceled: {},
	},
	TaskStateLeased: {
		TaskStateRunning:          {},
		TaskStateAwaitingApproval: {},
		TaskStateQueued:           {},
		TaskStateFailed:           {},
		TaskStateBlocked:          {},
		TaskStateCanceled:         {},
	},
	TaskStateRunning: {
		TaskStateAwaitingApproval: {},
		TaskStateCompleted:        {},
		TaskStateFailed:           {},
		TaskStateBlocked:          {},
		TaskStateCanceled:         {},
	},
	TaskStateAwaitingApproval: {
		TaskStateRunning:  {},
		TaskStateQueued:   {},
		TaskStateBlocked:  {},
		TaskStateCanceled: {},
	},
	TaskStateFailed: {
		TaskStateQueued: {},
	},
	TaskStateBlocked: {
		TaskStateQueued:   {},
		TaskStateCanceled: {},
	},
	TaskStateCompleted: {},
	TaskStateCanceled:  {},
}

func ValidateTaskState(state TaskState) error {
	if _, ok := allowedTransitions[state]; !ok {
		return fmt.Errorf("invalid task state: %q", state)
	}
	return nil
}

func ValidateTransition(from, to TaskState) error {
	if err := ValidateTaskState(from); err != nil {
		return err
	}
	if err := ValidateTaskState(to); err != nil {
		return err
	}
	if _, ok := allowedTransitions[from][to]; !ok {
		return fmt.Errorf("invalid task transition: %s -> %s", from, to)
	}
	return nil
}

func MapFailureReasonToState(reason string) TaskState {
	reason = strings.TrimSpace(strings.ToLower(reason))
	if _, ok := blockedReasons[reason]; ok {
		return TaskStateBlocked
	}
	return TaskStateFailed
}

func RetryFromFailed(attempt, maxAttempts int, retryable bool) (TaskState, int, error) {
	if !retryable {
		return "", attempt, fmt.Errorf("task is not retryable")
	}
	if maxAttempts <= 0 {
		return "", attempt, fmt.Errorf("max attempts must be > 0")
	}
	if attempt >= maxAttempts {
		return "", attempt, fmt.Errorf("retry attempts exhausted: %d/%d", attempt, maxAttempts)
	}
	if err := ValidateTransition(TaskStateFailed, TaskStateQueued); err != nil {
		return "", attempt, err
	}
	return TaskStateQueued, attempt + 1, nil
}
