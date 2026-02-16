package cli

import (
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
