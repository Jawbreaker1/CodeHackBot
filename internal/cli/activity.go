package cli

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

const workingIndicatorDelay = 5 * time.Second

type activityWriter struct {
	base      io.Writer
	seen      atomic.Bool
	lastWrite atomic.Int64
}

func newActivityWriter(base io.Writer) *activityWriter {
	if base == nil {
		return nil
	}
	return &activityWriter{base: base}
}

func (w *activityWriter) Write(p []byte) (int, error) {
	w.seen.Store(true)
	w.lastWrite.Store(time.Now().UnixNano())
	return w.base.Write(p)
}

func (w *activityWriter) HasOutput() bool {
	if w == nil {
		return false
	}
	return w.seen.Load()
}

func (r *Runner) startWorkingIndicator(writer *activityWriter) func() {
	if writer == nil || !r.isTTY() {
		return func() {}
	}
	stop := make(chan struct{})
	var once sync.Once
	stopFn := func() {
		once.Do(func() {
			close(stop)
		})
	}
	go func() {
		timer := time.NewTimer(workingIndicatorDelay)
		defer timer.Stop()
		select {
		case <-stop:
			return
		case <-timer.C:
			if !writer.HasOutput() {
				fmt.Println("... working (no output yet)")
			}
		}
	}()
	return stopFn
}
