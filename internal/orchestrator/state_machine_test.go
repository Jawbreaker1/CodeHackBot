package orchestrator

import "testing"

func TestValidateTransition_ValidMatrix(t *testing.T) {
	t.Parallel()

	valid := [][2]TaskState{
		{TaskStateQueued, TaskStateLeased},
		{TaskStateLeased, TaskStateAwaitingApproval},
		{TaskStateLeased, TaskStateRunning},
		{TaskStateRunning, TaskStateAwaitingApproval},
		{TaskStateAwaitingApproval, TaskStateQueued},
		{TaskStateAwaitingApproval, TaskStateRunning},
		{TaskStateRunning, TaskStateCompleted},
		{TaskStateRunning, TaskStateFailed},
		{TaskStateRunning, TaskStateBlocked},
		{TaskStateFailed, TaskStateQueued},
		{TaskStateBlocked, TaskStateQueued},
	}
	for _, pair := range valid {
		if err := ValidateTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("expected valid transition %s->%s, got %v", pair[0], pair[1], err)
		}
	}
}

func TestValidateTransition_InvalidTransitions(t *testing.T) {
	t.Parallel()

	invalid := [][2]TaskState{
		{TaskStateQueued, TaskStateCompleted},
		{TaskStateBlocked, TaskStateRunning},
		{TaskStateCompleted, TaskStateQueued},
	}
	for _, pair := range invalid {
		if err := ValidateTransition(pair[0], pair[1]); err == nil {
			t.Fatalf("expected invalid transition %s->%s", pair[0], pair[1])
		}
	}
}

func TestMapFailureReasonToState(t *testing.T) {
	t.Parallel()

	if got := MapFailureReasonToState("approval_timeout"); got != TaskStateBlocked {
		t.Fatalf("approval_timeout should map to blocked, got %s", got)
	}
	if got := MapFailureReasonToState("scope_denied"); got != TaskStateBlocked {
		t.Fatalf("scope_denied should map to blocked, got %s", got)
	}
	if got := MapFailureReasonToState("tool_exit_1"); got != TaskStateFailed {
		t.Fatalf("tool_exit_1 should map to failed, got %s", got)
	}
}

func TestRetryFromFailedPolicy(t *testing.T) {
	t.Parallel()

	next, attempt, err := RetryFromFailed(1, 3, true)
	if err != nil {
		t.Fatalf("expected retry allowed, got %v", err)
	}
	if next != TaskStateQueued {
		t.Fatalf("expected queued, got %s", next)
	}
	if attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", attempt)
	}

	if _, _, err := RetryFromFailed(3, 3, true); err == nil {
		t.Fatalf("expected retry exhaustion error")
	}
	if _, _, err := RetryFromFailed(1, 3, false); err == nil {
		t.Fatalf("expected non-retryable error")
	}
}
