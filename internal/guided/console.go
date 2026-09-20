// Package guided supplies the primary terminal application.
package guided

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

type line struct {
	text string
	err  error
}

// One reader owns stdin for the application lifetime. The mutex serializes
// prompts and progress from concurrent workers without losing buffered input.
type Console struct {
	mu        sync.Mutex
	writer    io.Writer
	lines     <-chan line
	dashboard assessmentDashboard
}

func NewConsole(ctx context.Context, reader io.Reader, writer io.Writer) *Console {
	lines := make(chan line)
	go func() {
		defer close(lines)
		s := bufio.NewScanner(reader)
		s.Buffer(make([]byte, 4096), 1<<20)
		for s.Scan() {
			select {
			case lines <- line{text: s.Text()}:
			case <-ctx.Done():
				return
			}
		}
		if err := s.Err(); err != nil {
			select {
			case lines <- line{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return &Console{writer: writer, lines: lines, dashboard: newAssessmentDashboard()}
}

func (c *Console) Ask(ctx context.Context, prompt string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprint(c.writer, prompt+"\n> ")
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case input, ok := <-c.lines:
		if !ok {
			return "", io.EOF
		}
		if input.err != nil {
			return "", input.err
		}
		return strings.TrimSpace(input.text), nil
	}
}

func (c *Console) Print(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(c.writer, format, args...)
}

func (c *Console) Progress(e assessment.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, line := range c.dashboard.apply(e) {
		fmt.Fprintln(c.writer, line)
	}
}

func (c *Console) approvalRequested(taskID, command string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, line := range c.dashboard.apply(assessment.Event{TaskID: taskID, Kind: "approval_required", Message: command, Action: command}) {
		fmt.Fprintln(c.writer, line)
	}
}

type taskApprover struct {
	console *Console
	task    assessment.Task
	scope   string
}

func (a taskApprover) Approve(ctx context.Context, r approval.Request) (approval.Decision, error) {
	a.console.approvalRequested(a.task.ID, r.Command)
	prompt := fmt.Sprintf("\nAction approval — %s\nPurpose: %s\nDeclared scope: %s\nWorking directory: %s\nExact invocation: %s\nAllow this action? [y/N] (Ctrl-C stops the entire assessment)", a.task.ID, a.task.Goal, a.scope, r.Cwd, r.Command)
	for {
		answer, err := a.console.Ask(ctx, prompt)
		if err != nil {
			return approval.DecisionDeny, err
		}
		switch strings.ToLower(answer) {
		case "y", "yes":
			return approval.DecisionApproveOnce, nil
		case "", "n", "no":
			return approval.DecisionDeny, nil
		default:
			prompt = "Please enter y to allow this action or n to deny it."
		}
	}
}
