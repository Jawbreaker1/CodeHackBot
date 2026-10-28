// Package guided supplies the primary terminal application.
package guided

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
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

type promptMode uint8

const (
	promptAnswer promptMode = iota
	promptChat
	promptApproval
)

const (
	consoleOutput consoleEventKind = iota
	consolePrompt
	consoleAssessmentStarted
	consoleAssessmentFinished
	consolePermissions
	consoleDashboard
	consoleCoordinatorStatus
	consoleModalStart
	consoleModalEnd
	consoleChoices
	consoleNotice
)

type consoleChoice struct {
	Label  string
	Detail string
	Value  string
}

type consoleEvent struct {
	kind          consoleEventKind
	text          string
	promptMode    promptMode
	choices       []consoleChoice
	defaultChoice int
	working       bool
	activeWorkers int
}

var errModalCanceled = errors.New("dialog canceled")

const modalCancelInput = "\x1b"

// One reader owns stdin for the application lifetime. The mutex serializes
// prompts and progress from concurrent workers without losing buffered input.
type Console struct {
	permissionMode approval.Mode
	mu             sync.Mutex
	askMu          sync.Mutex
	writer         io.Writer
	requests       chan promptRequest
	commands       chan line
	dashboard      assessmentDashboard
	ctx            context.Context
	events         func(consoleEvent)
	eventMu        sync.Mutex
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
	return c.ask(ctx, prompt, nil, promptAnswer)
}

func (c *Console) AskChat(ctx context.Context, prompt string) (string, error) {
	return c.ask(ctx, prompt, nil, promptChat)
}

func (c *Console) AskApproval(ctx context.Context, prompt string) (string, error) {
	return c.ask(ctx, prompt, nil, promptApproval)
}

func (c *Console) ask(ctx context.Context, prompt string, choiceEvent *consoleEvent, mode promptMode) (string, error) {
	c.askMu.Lock()
	defer c.askMu.Unlock()
	response := make(chan line, 1)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case c.requests <- promptRequest{response: response}:
	}
	if choiceEvent != nil {
		c.emitEvent(*choiceEvent)
	}
	c.emitEvent(consoleEvent{kind: consolePrompt, text: prompt, promptMode: mode})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case input := <-response:
		if input.err != nil {
			return "", input.err
		}
		if input.text == modalCancelInput {
			return "", errModalCanceled
		}
		return strings.TrimSpace(input.text), nil
	}
}

func (c *Console) BeginModal(title string) { c.emit(consoleModalStart, title) }
func (c *Console) EndModal()               { c.emit(consoleModalEnd, "") }

func (c *Console) Choose(ctx context.Context, title, prompt string, choices []consoleChoice, defaultChoice int) (string, error) {
	if c.events == nil {
		c.Print("\n%s\n", title)
		for i, choice := range choices {
			c.Print("  %d. %s", i+1, choice.Label)
			if choice.Detail != "" {
				c.Print(" · %s", choice.Detail)
			}
			c.Print("\n")
		}
	}
	var selection *consoleEvent
	if c.events != nil {
		selection = &consoleEvent{kind: consoleChoices, text: title, choices: choices, defaultChoice: defaultChoice}
	}
	answer, err := c.ask(ctx, prompt, selection, promptAnswer)
	if err != nil {
		return "", err
	}
	if answer == "" && defaultChoice >= 0 && defaultChoice < len(choices) {
		return choices[defaultChoice].Value, nil
	}
	if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(choices) {
		return choices[n-1].Value, nil
	}
	return answer, nil
}

func (c *Console) Notice(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if c.events == nil {
		c.Print("%s\n", message)
		return
	}
	c.emit(consoleNotice, message)
}

func (c *Console) ShowInfoModal(ctx context.Context, title, message string) error {
	c.BeginModal(title)
	defer c.EndModal()
	c.Print("%s\n", message)
	_, err := c.Ask(ctx, "Press Enter or Esc to close")
	if errors.Is(err, errModalCanceled) {
		return nil
	}
	return err
}

// Commands returns operator input received while no approval or worker
// question owns the next line. It lets the guided application remain a live
// conversation while delegated workers execute.
func (c *Console) Commands() <-chan line { return c.commands }

// PromptCommand shows a free-form coordinator input prompt while Commands
// remains the sole receiver, including input typed during a phase change.
func (c *Console) PromptCommand(prompt string) {
	c.emitEvent(consoleEvent{kind: consolePrompt, text: prompt, promptMode: promptChat})
}

func (c *Console) Print(format string, args ...any) {
	c.emit(consoleOutput, fmt.Sprintf(format, args...))
}

func (c *Console) Progress(e assessment.Event) {
	var lines []string
	var status string
	var activeWorkers int
	c.mu.Lock()
	for _, line := range c.dashboard.apply(e) {
		lines = append(lines, line)
	}
	if e.TaskID != "" {
		status = strings.Join(c.dashboard.compactSnapshot(), "\n")
		activeWorkers = c.dashboard.activeWorkers()
	}
	c.mu.Unlock()
	if status != "" && c.events != nil {
		c.emitEvent(consoleEvent{kind: consoleDashboard, text: status, activeWorkers: activeWorkers})
		return
	}
	if c.events != nil && e.TaskID == "" {
		switch e.Kind {
		case "planning":
			c.emitEvent(consoleEvent{kind: consoleCoordinatorStatus, text: "planning next steps", working: true})
		case "plan":
			c.emitEvent(consoleEvent{kind: consoleCoordinatorStatus, text: "plan ready"})
		case "round_update":
			c.emitEvent(consoleEvent{kind: consoleCoordinatorStatus, text: "reviewing worker results", working: true})
		}
	}
	if len(lines) > 0 {
		c.emit(consoleOutput, strings.Join(lines, "\n")+"\n")
	}
}

func (c *Console) approvalRequested(taskID, command string) {
	var lines []string
	var status string
	var activeWorkers int
	c.mu.Lock()
	for _, line := range c.dashboard.apply(assessment.Event{TaskID: taskID, Kind: "approval_required", Message: command, Action: command}) {
		lines = append(lines, line)
	}
	status = strings.Join(c.dashboard.compactSnapshot(), "\n")
	activeWorkers = c.dashboard.activeWorkers()
	c.mu.Unlock()
	if c.events != nil {
		c.emitEvent(consoleEvent{kind: consoleDashboard, text: status, activeWorkers: activeWorkers})
		return
	}
	if len(lines) > 0 {
		c.emit(consoleOutput, strings.Join(lines, "\n")+"\n")
	}
}

func (c *Console) dashboardCompactSnapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dashboard.compactSnapshot()
}

func (c *Console) emit(kind consoleEventKind, text string) {
	c.emitEvent(consoleEvent{kind: kind, text: text})
}

func (c *Console) emitEvent(event consoleEvent) {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if c.events != nil {
		c.events(event)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if event.kind == consolePrompt {
		_, _ = fmt.Fprint(c.writer, event.text+"\n> ")
		return
	}
	if event.kind == consoleOutput {
		_, _ = fmt.Fprint(c.writer, event.text)
	}
}

func (c *Console) assessmentStarted() {
	c.emit(consoleAssessmentStarted, "")
}

func (c *Console) assessmentFinished() {
	c.emit(consoleAssessmentFinished, "")
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
	if !a.console.approvalMode().RequiresApproval(r) {
		return approval.DecisionApproveSession, ctx.Err()
	}
	a.console.approvalRequested(a.task.ID, r.Summary)
	impact := strings.TrimSpace(r.Impact)
	if impact == "" {
		impact = "The coordinator did not provide an impact summary; review the exact invocation carefully."
	}
	purpose := r.Summary
	if purpose == "" {
		purpose = a.task.Goal
	}
	review := ""
	if r.ReviewReason != "" {
		review = "\nRisk review: " + r.ReviewReason
	}
	prompt := fmt.Sprintf("\n%s — %s\nTarget: %s\nImpact: %s%s\nAllow this action? [y/N, d=command details, p=approval settings]", a.task.ID, purpose, r.Target, impact, review)
	for {
		answer, err := a.console.AskApproval(ctx, prompt)
		if err != nil {
			return approval.DecisionDeny, err
		}
		switch strings.ToLower(answer) {
		case "d", "details":
			a.console.Print("Action: %s\nTarget: %s\nImpact: %s%s\nExact invocation: %s\nWorking directory: %s\nDeclared scope: %s\nModel risk assessment: %s\n", purpose, r.Target, impact, review, r.Command, r.Cwd, a.scope, r.Risk)
		case "p", "/permissions":
			if err := a.console.choosePermissions(ctx); err != nil {
				return approval.DecisionDeny, err
			}
			if !a.console.approvalMode().RequiresApproval(r) {
				return approval.DecisionApproveSession, nil
			}
		case "y", "yes":
			return approval.DecisionApproveOnce, nil
		case "", "n", "no":
			return approval.DecisionDeny, nil
		default:
			prompt = "Please enter y to allow this action or n to deny it."
		}
	}
}
