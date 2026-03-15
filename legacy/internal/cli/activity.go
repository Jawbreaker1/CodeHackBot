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
	workingIndicatorTaskMaxRunes = 96
)

type activityWriter struct {
	base      io.Writer
	mu        sync.Mutex
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
	w.mu.Lock()
	defer w.mu.Unlock()
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
	w.mu.Lock()
	defer w.mu.Unlock()
	// Prefix with a line break so the status line doesn't get appended mid-line.
	_, _ = w.base.Write([]byte("\n" + msg + "\n"))
}

func (r *Runner) startWorkingIndicator(writer *activityWriter) func() {
	return r.startWorkingIndicatorWithTask(writer, r.currentTask)
}

func (r *Runner) startWorkingIndicatorWithTask(writer *activityWriter, taskLabel string) func() {
	if writer == nil || !r.isTTY() {
		return func() {}
	}
	task := strings.TrimSpace(taskLabel)
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

func formatWorkingCommandTask(prefix, command string, args []string) string {
	parts := make([]string, 0, 2+len(args))
	prefix = strings.TrimSpace(prefix)
	if prefix != "" {
		parts = append(parts, prefix)
	}
	command = strings.TrimSpace(command)
	if command != "" {
		parts = append(parts, command)
	}
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		parts = append(parts, arg)
	}
	task := strings.TrimSpace(strings.Join(parts, " "))
	if task == "" {
		return ""
	}
	runes := []rune(task)
	if len(runes) <= workingIndicatorTaskMaxRunes {
		return task
	}
	if workingIndicatorTaskMaxRunes <= 3 {
		return string(runes[:workingIndicatorTaskMaxRunes])
	}
	return string(runes[:workingIndicatorTaskMaxRunes-3]) + "..."
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
