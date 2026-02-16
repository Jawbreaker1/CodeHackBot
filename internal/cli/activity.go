package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	workingIndicatorInitialDelay = 2 * time.Second
	workingIndicatorInterval     = 5 * time.Second
)

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

func (w *activityWriter) LastWriteTime() time.Time {
	if w == nil {
		return time.Time{}
	}
	ns := w.lastWrite.Load()
	if ns <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (w *activityWriter) writeStatusLine(msg string) {
	if w == nil || w.base == nil {
		return
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	// Prefix with a line break so the status line doesn't get appended mid-line.
	_, _ = w.base.Write([]byte("\n" + msg + "\n"))
}

func (r *Runner) startWorkingIndicator(writer *activityWriter) func() {
	if writer == nil || !r.isTTY() {
		return func() {}
	}
	task := strings.TrimSpace(r.currentTask)
	started := time.Now()
	stop := make(chan struct{})
	var once sync.Once
	stopFn := func() {
		once.Do(func() {
			close(stop)
		})
	}
	go func() {
		timer := time.NewTimer(workingIndicatorInitialDelay)
		defer timer.Stop()
		ticker := time.NewTicker(workingIndicatorInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
				if msg := workingIndicatorMessage(task, started, time.Now(), writer.HasOutput(), writer.LastWriteTime()); msg != "" {
					writer.writeStatusLine(msg)
				}
			case <-ticker.C:
				if msg := workingIndicatorMessage(task, started, time.Now(), writer.HasOutput(), writer.LastWriteTime()); msg != "" {
					writer.writeStatusLine(msg)
				}
			}
		}
	}()
	return stopFn
}

func workingIndicatorMessage(task string, started, now time.Time, hasOutput bool, lastOutput time.Time) string {
	task = strings.TrimSpace(task)
	scope := ""
	if task != "" {
		scope = " on " + task
	}
	if now.IsZero() {
		now = time.Now()
	}
	if started.IsZero() {
		started = now
	}
	elapsed := formatElapsed(now.Sub(started))
	if !hasOutput {
		return fmt.Sprintf("... still working%s (%s elapsed, no output yet)", scope, elapsed)
	}
	if !lastOutput.IsZero() && now.Sub(lastOutput) >= workingIndicatorInterval {
		return fmt.Sprintf("... still working%s (%s elapsed, waiting for output)", scope, elapsed)
	}
	return ""
}
