package cli

import (
	"context"
	"testing"
	"time"
)

func TestNotifyInterruptDoesNotBlockWhenChannelFull(t *testing.T) {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	done := make(chan struct{})
	go func() {
		notifyInterrupt(ch)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("notifyInterrupt blocked on full channel")
	}
}

func TestBindInterruptCancelMarksInterruptedAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan struct{}, 1)
	isInterrupted := bindInterruptCancel(cancel, ch)
	if isInterrupted() {
		t.Fatalf("expected interrupted=false before signal")
	}
	ch <- struct{}{}
	select {
	case <-ctx.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("expected context cancellation after interrupt")
	}
	if !isInterrupted() {
		t.Fatalf("expected interrupted=true after signal")
	}
}

func TestBindInterruptCancelNilChannelAlwaysFalse(t *testing.T) {
	isInterrupted := bindInterruptCancel(func() {}, nil)
	if isInterrupted() {
		t.Fatalf("expected interrupted=false for nil channel")
	}
}
