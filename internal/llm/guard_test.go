package llm

import (
	"testing"
	"time"
)

func TestGuardDisablesAfterFailures(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	guard := NewGuard(2, 10*time.Minute)
	guard.now = func() time.Time { return now }

	if !guard.Allow() {
		t.Fatalf("expected guard to allow initially")
	}
	guard.RecordFailure()
	if !guard.Allow() {
		t.Fatalf("expected allow after first failure")
	}
	guard.RecordFailure()
	if guard.Allow() {
		t.Fatalf("expected guard to disable after max failures")
	}
	if guard.DisabledUntil().IsZero() {
		t.Fatalf("expected disabled until to be set")
	}
}

func TestGuardResetsOnSuccess(t *testing.T) {
	guard := NewGuard(1, time.Minute)
	guard.RecordFailure()
	if guard.Allow() {
		t.Fatalf("expected guard to disable")
	}
	guard.RecordSuccess()
	if !guard.Allow() {
		t.Fatalf("expected guard to allow after success")
	}
}
