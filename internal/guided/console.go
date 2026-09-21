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

type consoleEventKind uint8

const (
	consoleOutput consoleEventKind = iota
	consolePrompt
	consoleAssessmentStarted
)

type consoleEvent struct {
	kind consoleEventKind
	text string
}

// One reader owns stdin for the application lifetime. The mutex serializes
// prompts and progress from concurrent workers without losing buffered input.
type Console struct {
	mu        sync.Mutex
	askMu     sync.Mutex
	writer    io.Writer
	requests  chan promptRequest
	commands  chan line
	dashboard assessmentDashboard
	ctx       context.Context
	events    func(consoleEvent)
	eventMu   sync.Mutex
}

type promptRequest struct{ response chan line }

func NewConsole(ctx context.Context, reader io.Reader, writer io.Writer) *Console {
	return newConsole(ctx, reader, writer, nil)
}

func newConsole(ctx context.Context, reader io.Reader, writer io.Writer, events func(consoleEvent)) *Console {
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
	requests := make(chan promptRequest)
	commands := make(chan line, 32)
	go func() {
		defer close(commands)
		var active *promptRequest
		for {
			select {
			case request := <-requests:
				active = &request
			case input, ok := <-lines:
				if !ok {
					if active != nil {
						active.response <- line{err: io.EOF}
					}
					return
				}
				if active != nil {
					active.response <- input
					active = nil
					continue
				}
				select {
				case commands <- input:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return &Console{writer: writer, requests: requests, commands: commands, dashboard: newAssessmentDashboard(), ctx: ctx, events: events}
}

func (c *Console) Ask(ctx context.Context, prompt string) (string, error) {
	c.askMu.Lock()
	defer c.askMu.Unlock()
	response := make(chan line, 1)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case c.requests <- promptRequest{response: response}:
	}
	c.emit(consolePrompt, prompt)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case input := <-response:
		if input.err != nil {
			return "", input.err
		}
		return strings.TrimSpace(input.text), nil
	}
}

// Commands returns operator input received while no approval or worker
// question owns the next line. It lets the guided application remain a live
// conversation while delegated workers execute.
func (c *Console) Commands() <-chan line { return c.commands }

func (c *Console) Print(format string, args ...any) {
	c.emit(consoleOutput, fmt.Sprintf(format, args...))
}

func (c *Console) Progress(e assessment.Event) {
	var lines []string
	c.mu.Lock()
	for _, line := range c.dashboard.apply(e) {
		lines = append(lines, line)
	}
	c.mu.Unlock()
	if len(lines) > 0 {
		c.emit(consoleOutput, strings.Join(lines, "\n")+"\n")
	}
}

func (c *Console) approvalRequested(taskID, command string) {
	var lines []string
	c.mu.Lock()
	for _, line := range c.dashboard.apply(assessment.Event{TaskID: taskID, Kind: "approval_required", Message: command, Action: command}) {
		lines = append(lines, line)
	}
	c.mu.Unlock()
	if len(lines) > 0 {
		c.emit(consoleOutput, strings.Join(lines, "\n")+"\n")
	}
}

func (c *Console) emit(kind consoleEventKind, text string) {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if c.events != nil {
		c.events(consoleEvent{kind: kind, text: text})
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if kind == consolePrompt {
		_, _ = fmt.Fprint(c.writer, text+"\n> ")
		return
	}
	_, _ = fmt.Fprint(c.writer, text)
}

func (c *Console) assessmentStarted() {
	c.emit(consoleAssessmentStarted, "")
}

func (c *Console) DashboardSnapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dashboard.snapshot()
}

type taskApprover struct {
	console *Console
	task    assessment.Task
	scope   string
}

func (a taskApprover) Approve(ctx context.Context, r approval.Request) (approval.Decision, error) {
	a.console.approvalRequested(a.task.ID, r.Command)
	impact := strings.TrimSpace(r.Impact)
	if impact == "" {
		impact = "The coordinator did not provide an impact summary; review the exact invocation carefully."
	}
	prompt := fmt.Sprintf("\nAction approval — %s\nPurpose: %s\nDeclared scope: %s\nWorking directory: %s\nExpected effect / risk: %s\nExact invocation: %s\nAllow this action? [y/N] (Ctrl-C stops the entire assessment)", a.task.ID, a.task.Goal, a.scope, r.Cwd, impact, r.Command)
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
