package approval

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// Decision is the v1 approval outcome for a single execution request.
type Decision string

const (
	DecisionDeny           Decision = "denied"
	DecisionApproveOnce    Decision = "approved_once"
	DecisionApproveSession Decision = "approved_session"
)

// Request is a minimal execution approval request.
type Request struct {
	Command  string
	UseShell bool
}

// Approver decides whether an action may execute.
type Approver interface {
	Approve(context.Context, Request) (Decision, error)
}

// StaticApprover always returns the configured decision.
type StaticApprover struct {
	Decision Decision
}

func (a StaticApprover) Approve(context.Context, Request) (Decision, error) {
	return a.Decision, nil
}

// PromptApprover provides the minimal interactive approval model.
type PromptApprover struct {
	Reader         io.Reader
	Writer         io.Writer
	sessionAllowed bool
}

func (a *PromptApprover) Approve(ctx context.Context, req Request) (Decision, error) {
	if a.sessionAllowed {
		return DecisionApproveSession, nil
	}
	if a.Reader == nil || a.Writer == nil {
		return "", fmt.Errorf("interactive approval requires reader and writer")
	}

	_, _ = fmt.Fprintf(a.Writer,
		"Approve execution?\ncommand: %s\nmode: %s\nchoices: [t]his time, [a]lways allow (session), [n]o\n> ",
		strings.TrimSpace(req.Command),
		executionMode(req.UseShell),
	)

	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(a.Reader)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			errCh <- err
			return
		}
		lineCh <- line
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-errCh:
		return "", fmt.Errorf("read approval input: %w", err)
	case line := <-lineCh:
		if strings.TrimSpace(line) == "" {
			return "", fmt.Errorf("approval input required")
		}
		switch normalizeChoice(line) {
		case "t", "this", "once", "yes", "y":
			return DecisionApproveOnce, nil
		case "a", "always", "session":
			a.sessionAllowed = true
			return DecisionApproveSession, nil
		case "n", "no", "deny":
			return DecisionDeny, nil
		default:
			return "", fmt.Errorf("unsupported approval choice %q", strings.TrimSpace(line))
		}
	}
}

func executionMode(useShell bool) string {
	if useShell {
		return "shell"
	}
	return "direct"
}

func normalizeChoice(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
