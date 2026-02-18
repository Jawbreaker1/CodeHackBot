//go:build !windows

package orchestrator

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func withWorkerSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}
