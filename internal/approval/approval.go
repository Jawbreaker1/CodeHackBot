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
	Summary string
	Target  string
	Risk    string
	// WorkerRisk is the worker's original label when an independent risk review
	// has examined an execution. Risk then holds the reviewer's effective label.
	WorkerRisk   string
	ReviewReason string
	Command      string
	UseShell     bool
	Cwd          string
	// trustedReadOnlyObservation is set only by the fixed observation path.
	// A worker's model-authored risk label cannot grant this capability.
	trustedReadOnlyObservation   bool
	independentlyReviewedLowRisk bool
	// Impact is model-authored context about what the action is expected to do.
	// It never grants permission; the exact invocation remains authoritative.
	Impact string
}

// ReviewedLowRiskRequest records that a separate pre-execution review found
// the exact invocation low risk. The worker's own label cannot set this mark.
func ReviewedLowRiskRequest(r Request, reason string) Request {
	r.independentlyReviewedLowRisk = true
	r.ReviewReason = reason
	return r
}

// ReadOnlyObservationRequest marks a validated built-in observation as eligible
// for automatic approval. Call only after the observation tool and its inputs
// have been checked by the host application; arbitrary commands must use Request.
func ReadOnlyObservationRequest(command, cwd, summary, target, impact string) Request {
	return Request{
		Command: command, Cwd: cwd, Summary: summary, Target: target,
		Impact: impact, Risk: "low", trustedReadOnlyObservation: true,
	}
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
		"Approve execution?\ncommand: %s\nmode: %s\ncwd: %s\nchoices: [t]his time, [a]lways allow (session), [n]o\n> ",
		strings.TrimSpace(req.Command),
		executionMode(req.UseShell),
		req.Cwd,
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
