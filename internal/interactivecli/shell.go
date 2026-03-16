package interactivecli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
)

type Runner interface {
	Run(ctx context.Context, packet ctxpacket.WorkerPacket, maxSteps int) (workerloop.Outcome, error)
}

type Shell struct {
	Reader   io.Reader
	Writer   io.Writer
	Runner   Runner
	RepoRoot string

	BaseURL   string
	Model     string
	MaxSteps  int
	AllowAll  bool
	StatePath string
}

func (s Shell) Run(ctx context.Context) error {
	if s.Reader == nil || s.Writer == nil {
		return fmt.Errorf("reader and writer are required")
	}
	if s.Runner == nil {
		return fmt.Errorf("runner is required")
	}
	if strings.TrimSpace(s.RepoRoot) == "" {
		return fmt.Errorf("repo root is required")
	}
	if strings.TrimSpace(s.StatePath) == "" {
		return fmt.Errorf("state path is required")
	}
	if s.MaxSteps <= 0 {
		s.MaxSteps = 3
	}

	frame, err := behavior.Load(s.RepoRoot, "worker", map[string]string{
		"approval_mode": approvalModeLabel(s.AllowAll),
		"surface":       "interactive_worker_cli",
	})
	if err != nil {
		return err
	}

	reader := bufio.NewReader(s.Reader)
	var packet ctxpacket.WorkerPacket
	started := false

	_, _ = fmt.Fprintln(s.Writer, "BirdHackBot interactive session. Type 'exit' to quit.")
	for {
		_, _ = fmt.Fprint(s.Writer, "birdhackbot> ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("read input: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if err == io.EOF {
				return nil
			}
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}

		if !started {
			packet, err = newInitialPacket(frame, line, s.Model, s.AllowAll, s.MaxSteps)
			if err != nil {
				return err
			}
			started = true
		} else {
			packet.RecentConversation = appendRecentConversation(packet.RecentConversation, "User: "+line)
			packet.CurrentStep.Objective = line
			packet.PlanState.ActiveStep = line
		}

		_ = sessionstate.Save(s.StatePath, sessionstate.State{
			Status:   "active",
			BaseURL:  s.BaseURL,
			Model:    s.Model,
			MaxSteps: s.MaxSteps,
			AllowAll: s.AllowAll,
			Packet:   packet,
		})

		outcome, runErr := s.Runner.Run(ctx, packet, s.MaxSteps)
		packet = outcome.Packet
		if strings.TrimSpace(outcome.Summary) != "" {
			packet.RecentConversation = appendRecentConversation(packet.RecentConversation, "Assistant: "+outcome.Summary)
		}
		state := sessionstate.State{
			Status:   "active",
			BaseURL:  s.BaseURL,
			Model:    s.Model,
			MaxSteps: s.MaxSteps,
			AllowAll: s.AllowAll,
			Packet:   packet,
			Summary:  outcome.Summary,
		}
		if runErr != nil {
			state.Status = "stopped"
			state.LastError = runErr.Error()
			_ = sessionstate.Save(s.StatePath, state)
			_, _ = fmt.Fprintf(s.Writer, "error: %v\n", runErr)
		} else {
			_ = sessionstate.Save(s.StatePath, state)
			_, _ = fmt.Fprintf(s.Writer, "%s\n", strings.TrimSpace(outcome.Summary))
		}

		if err == io.EOF {
			return nil
		}
	}
}

func newInitialPacket(frame behavior.Frame, goal, model string, allowAll bool, maxSteps int) (ctxpacket.WorkerPacket, error) {
	foundation, err := session.NewFoundation(session.Input{Goal: goal, ReportingRequirement: "owasp"})
	if err != nil {
		return ctxpacket.WorkerPacket{}, err
	}
	return ctxpacket.WorkerPacket{
		BehaviorFrame:     frame,
		SessionFoundation: foundation,
		CurrentStep: ctxpacket.Step{
			Objective:        foundation.Goal,
			DoneCondition:    "the stated user goal has been satisfied with evidence",
			FailCondition:    "cannot make honest progress on the stated user goal",
			ExpectedEvidence: []string{"command logs", "artifacts if produced"},
			RemainingBudget:  fmt.Sprintf("%d steps", maxSteps),
		},
		TaskRuntime: ctxpacket.InitialTaskRuntime(foundation.Goal),
		PlanState: ctxpacket.PlanState{
			Steps:      []string{"understand goal", "work the named target/task", "verify and finish"},
			ActiveStep: foundation.Goal,
		},
		RecentConversation: []string{"User: " + foundation.Goal},
		RunningSummary:     "Worker loop starting from the stated user goal.",
		OperatorState: ctxpacket.OperatorState{
			ScopeState:    "from_session_foundation",
			ApprovalState: approvalStateLabel(allowAll),
			Model:         model,
			ContextUsage:  "(unset)",
		},
	}, nil
}

func appendRecentConversation(current []string, entry string) []string {
	if strings.TrimSpace(entry) == "" {
		return current
	}
	next := append(current, entry)
	if len(next) > 8 {
		next = next[len(next)-8:]
	}
	return next
}

func approvalStateLabel(allowAll bool) string {
	if allowAll {
		return "approved_session"
	}
	return "pending"
}

func approvalModeLabel(allowAll bool) string {
	if allowAll {
		return "session_allow_all"
	}
	return "approve_per_execution"
}
