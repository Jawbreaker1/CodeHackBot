package interactivecli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextstats"
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
	SaveState func(path string, state sessionstate.State) error
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
		if strings.HasPrefix(line, "/") {
			if handled, cmdErr := handleShellCommand(s.Writer, line, started, packet); handled {
				if cmdErr != nil {
					_, _ = fmt.Fprintf(s.Writer, "error: %v\n", cmdErr)
				}
				if err == io.EOF {
					return nil
				}
				continue
			}
		}

		if !started {
			packet, err = newInitialPacket(frame, line, s.Model, s.AllowAll, s.MaxSteps)
			if err != nil {
				return err
			}
			started = true
		} else {
			packet.RecentConversation, packet.OlderConversationSummary = ctxpacket.AppendConversation(packet.RecentConversation, packet.OlderConversationSummary, "User: "+line)
		}

		if err := s.saveState(sessionstate.State{
			Status:   "active",
			BaseURL:  s.BaseURL,
			Model:    s.Model,
			MaxSteps: s.MaxSteps,
			AllowAll: s.AllowAll,
			Packet:   packet,
		}); err != nil {
			return fmt.Errorf("save session state: %w", err)
		}

		outcome, runErr := s.Runner.Run(ctx, packet, s.MaxSteps)
		packet = outcome.Packet
		if strings.TrimSpace(packet.CurrentStep.Objective) == "" {
			packet.CurrentStep.Objective = packet.SessionFoundation.Goal
		}
		if strings.TrimSpace(packet.PlanState.ActiveStep) == "" {
			packet.PlanState.ActiveStep = packet.SessionFoundation.Goal
		}
		if strings.TrimSpace(outcome.Summary) != "" {
			packet.RecentConversation, packet.OlderConversationSummary = ctxpacket.AppendConversation(packet.RecentConversation, packet.OlderConversationSummary, "Assistant: "+outcome.Summary)
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
			state.LastError = normalizeRunError(ctx, runErr)
			if err := s.saveState(state); err != nil {
				return fmt.Errorf("save session state after run error %q: %w", runErr, err)
			}
			if ctx.Err() != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				return runErr
			}
			_, _ = fmt.Fprintf(s.Writer, "error: %v\n", runErr)
		} else {
			if err := s.saveState(state); err != nil {
				return fmt.Errorf("save session state after run: %w", err)
			}
			_, _ = fmt.Fprintf(s.Writer, "%s\n", strings.TrimSpace(outcome.Summary))
		}

		if err == io.EOF {
			return nil
		}
	}
}

func (s Shell) saveState(state sessionstate.State) error {
	if s.SaveState != nil {
		return s.SaveState(s.StatePath, state)
	}
	return sessionstate.Save(s.StatePath, state)
}

func normalizeRunError(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "aborted by signal"
	}
	return err.Error()
}

func handleShellCommand(w io.Writer, line string, started bool, packet ctxpacket.WorkerPacket) (bool, error) {
	switch strings.TrimSpace(line) {
	case "/stats":
		if !started {
			_, _ = fmt.Fprintln(w, "no active session")
			return true, nil
		}
		_, _ = fmt.Fprintln(w, renderPacketStats(packet))
		return true, nil
	case "/packet":
		if !started {
			_, _ = fmt.Fprintln(w, "no active session")
			return true, nil
		}
		_, _ = fmt.Fprintln(w, packet.Render())
		return true, nil
	case "/lastlog":
		if !started {
			_, _ = fmt.Fprintln(w, "no active session")
			return true, nil
		}
		logPath := latestLogPath(packet)
		if strings.TrimSpace(logPath) == "" {
			_, _ = fmt.Fprintln(w, "(none)")
			return true, nil
		}
		_, _ = fmt.Fprintln(w, logPath)
		return true, nil
	default:
		return false, nil
	}
}

func newInitialPacket(frame behavior.Frame, goal, model string, allowAll bool, maxSteps int) (ctxpacket.WorkerPacket, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return ctxpacket.WorkerPacket{}, fmt.Errorf("resolve working directory: %w", err)
	}
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
		TaskRuntime: ctxpacket.InitialTaskRuntimeInDir(foundation.Goal, cwd),
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
			WorkingDir:    cwd,
		},
	}, nil
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

func renderPacketStats(packet ctxpacket.WorkerPacket) string {
	rendered := packet.Render()
	stats := contextstats.Build(rendered, packet.RenderSections())
	lines := []string{"context stats:"}
	for _, section := range stats.Sections {
		lines = append(lines, fmt.Sprintf("- %s: chars=%d lines=%d approx_tokens=%d", section.Name, section.Chars, section.Lines, section.ApproxTokens))
	}
	lines = append(lines, fmt.Sprintf("total: chars=%d lines=%d approx_tokens=%d sections=%d", stats.TotalChars, stats.TotalLines, stats.ApproxTotalTokens, stats.SectionCount))
	return strings.Join(lines, "\n")
}

func latestLogPath(packet ctxpacket.WorkerPacket) string {
	if strings.TrimSpace(packet.OperatorState.PendingLog) != "" {
		return strings.TrimSpace(packet.OperatorState.PendingLog)
	}
	if len(packet.LatestExecutionResult.LogRefs) > 0 {
		return strings.TrimSpace(packet.LatestExecutionResult.LogRefs[0])
	}
	return ""
}
