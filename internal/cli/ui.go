package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
)

const (
	llmIndicatorDelay    = 700 * time.Millisecond
	llmIndicatorInterval = 200 * time.Millisecond
)

func (r *Runner) confirm(prompt string) (bool, error) {
	for {
		line, err := r.readLine(fmt.Sprintf("%s [y/N]: ", prompt))
		if err != nil && err != io.EOF {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" || answer == "n" || answer == "no" {
			return false, nil
		}
		if answer == "y" || answer == "yes" {
			return true, nil
		}
		if err == io.EOF {
			return false, nil
		}
	}
}

func (r *Runner) prompt() string {
	if r.currentTask == "" {
		if r.currentMode != "" {
			return fmt.Sprintf("BirdHackBot[%s]> ", r.currentMode)
		}
		return "BirdHackBot> "
	}
	elapsed := formatElapsed(time.Since(r.currentTaskStart))
	return fmt.Sprintf("BirdHackBot[%s %s]> ", r.currentTask, elapsed)
}

func (r *Runner) setTask(task string) {
	r.currentTask = task
	r.currentTaskStart = time.Now()
	if r.cfg.UI.Verbose {
		r.logger.Printf("Task: %s", task)
	}
}

func (r *Runner) clearTask() {
	r.currentTask = ""
	r.currentTaskStart = time.Time{}
}

func (r *Runner) setMode(mode string) {
	r.currentMode = mode
}

func (r *Runner) clearMode() {
	r.currentMode = ""
}

func formatElapsed(d time.Duration) string {
	totalSeconds := int(d.Seconds())
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

type commandError struct {
	Result exec.CommandResult
	Err    error
}

func (e commandError) Error() string {
	if e.Err == nil {
		return "command failed"
	}
	return e.Err.Error()
}

func (e commandError) Unwrap() error {
	return e.Err
}

func (r *Runner) startLLMIndicator(label string) func() {
	r.setLLMStatus(label)
	if !r.isTTY() {
		return func() {
			r.clearLLMStatus()
		}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		timer := time.NewTimer(llmIndicatorDelay)
		defer timer.Stop()
		select {
		case <-stop:
			return
		case <-timer.C:
		}
		frames := []string{"-", "\\", "|", "/"}
		idx := 0
		start := time.Now()
		ticker := time.NewTicker(llmIndicatorInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				fmt.Print("\r\x1b[2K")
				return
			case <-ticker.C:
				elapsed := formatElapsed(time.Since(start))
				fmt.Printf("\rLLM %s %s (%s)", label, frames[idx], elapsed)
				idx = (idx + 1) % len(frames)
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(stop)
			<-done
			r.clearLLMStatus()
		})
	}
}

func (r *Runner) startLLMIndicatorIfAllowed(label string) func() {
	if !r.llmAllowed() {
		return func() {}
	}
	return r.startLLMIndicator(label)
}

func (r *Runner) setLLMStatus(label string) {
	if label == "" {
		label = "thinking"
	}
	r.llmMu.Lock()
	r.llmInFlight = true
	r.llmLabel = label
	r.llmStarted = time.Now()
	r.llmMu.Unlock()
}

func (r *Runner) clearLLMStatus() {
	r.llmMu.Lock()
	r.llmInFlight = false
	r.llmLabel = ""
	r.llmStarted = time.Time{}
	r.llmMu.Unlock()
}

func (r *Runner) llmStatus() (bool, string, time.Time) {
	r.llmMu.Lock()
	defer r.llmMu.Unlock()
	return r.llmInFlight, r.llmLabel, r.llmStarted
}

func (r *Runner) isTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
