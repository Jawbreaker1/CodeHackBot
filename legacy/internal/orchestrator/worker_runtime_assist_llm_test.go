package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestWorkerAssistCallTimeoutCapFromEnv(t *testing.T) {
	t.Setenv(workerLLMTimeoutSeconds, "10")
	if got := workerAssistCallTimeoutCap(); got != 10*time.Second {
		t.Fatalf("expected call timeout cap 10s, got %s", got)
	}
}

func TestNewAssistCallContextUsesConfiguredTimeoutCap(t *testing.T) {
	t.Setenv(workerLLMTimeoutSeconds, "10")
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	t.Cleanup(cancel)

	callCtx, callCancel, callTimeout, remaining, err := newAssistCallContext(ctx)
	defer callCancel()
	defer callCtx.Done()
	if err != nil {
		t.Fatalf("newAssistCallContext returned error: %v", err)
	}
	if callTimeout != 10*time.Second {
		t.Fatalf("expected call timeout 10s, got %s", callTimeout)
	}
	if remaining < 59*time.Minute {
		t.Fatalf("expected remaining budget near 1h, got %s", remaining)
	}
}
