package interactivecli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextstats"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
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
	Chat      func(ctx context.Context, messages []llmclient.Message) (string, error)
	Classify  func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error)
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
	stream := []string{"BirdHackBot interactive session."}
	needsRender := true

	for {
		if needsRender {
			view := BuildViewState(packet, shellSessionStatus(started, packet))
			_, _ = fmt.Fprintln(s.Writer, renderDashboard(view, stream, "birdhackbot> "))
			needsRender = false
		}
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
			var cmdOut strings.Builder
			if handled, cmdErr := handleShellCommand(&cmdOut, line, started, packet); handled {
				stream = append(stream, "Command: "+line)
				if out := strings.TrimSpace(cmdOut.String()); out != "" {
					stream = append(stream, "System: "+compactForStream(out, 280))
				}
				if cmdErr != nil {
					stream = append(stream, "Error: "+cmdErr.Error())
				}
				needsRender = true
				if err == io.EOF {
					return nil
				}
				continue
			}
		}
		stream = append(stream, "User: "+line)
		modeDecision, classifyErr := s.classifyInput(ctx, frame, packet, line, started)
		if classifyErr != nil {
			modeDecision = workermode.FallbackDecision()
			stream = append(stream, "System: input classification failed; fell back to conversation mode")
		}
		if modeDecision.Mode == workerplan.ModeConversation {
			reply, convoErr := s.answerConversation(ctx, frame, packet, line, started)
			if convoErr != nil {
				stream = append(stream, "Error: "+convoErr.Error())
			} else {
				stream = append(stream, "Assistant: "+strings.TrimSpace(reply))
				if started {
					packet.RecentConversation, packet.OlderConversationSummary = ctxpacket.AppendConversation(packet.RecentConversation, packet.OlderConversationSummary, "User: "+line)
					packet.RecentConversation, packet.OlderConversationSummary = ctxpacket.AppendConversation(packet.RecentConversation, packet.OlderConversationSummary, "Assistant: "+strings.TrimSpace(reply))
					state := sessionstate.State{
						Status:   persistedSessionStatus(packet, nil),
						BaseURL:  s.BaseURL,
						Model:    s.Model,
						MaxSteps: s.MaxSteps,
						AllowAll: s.AllowAll,
						Packet:   packet,
						Summary:  strings.TrimSpace(reply),
					}
					if err := s.saveState(state); err != nil {
						return fmt.Errorf("save session state after conversation: %w", err)
					}
				}
			}
			needsRender = true
			if err == io.EOF {
				return nil
			}
			continue
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
		packet.OperatorState.ModeHint = string(modeDecision.Mode)

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
		stream = appendRunEvents(stream, packet)
		terminalSummary := buildTerminalChatSummary(packet, outcome.Summary, runErr)
		if strings.TrimSpace(terminalSummary) != "" {
			packet.RecentConversation, packet.OlderConversationSummary = ctxpacket.AppendConversation(packet.RecentConversation, packet.OlderConversationSummary, "Assistant: "+terminalSummary)
			stream = append(stream, "Assistant: "+strings.TrimSpace(terminalSummary))
		}
		state := sessionstate.State{
			Status:   persistedSessionStatus(packet, runErr),
			BaseURL:  s.BaseURL,
			Model:    s.Model,
			MaxSteps: s.MaxSteps,
			AllowAll: s.AllowAll,
			Packet:   packet,
			Summary:  terminalSummary,
		}
		if runErr != nil {
			state.LastError = normalizeRunError(ctx, runErr)
			if err := s.saveState(state); err != nil {
				return fmt.Errorf("save session state after run error %q: %w", runErr, err)
			}
			if ctx.Err() != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				return runErr
			}
			stream = append(stream, "Error: "+runErr.Error())
		} else {
			if err := s.saveState(state); err != nil {
				return fmt.Errorf("save session state after run: %w", err)
			}
		}
		needsRender = true

		if err == io.EOF {
			return nil
		}
	}
}

func shellSessionStatus(started bool, packet ctxpacket.WorkerPacket) string {
	if !started {
		return "idle"
	}
	switch strings.TrimSpace(packet.TaskRuntime.State) {
	case "done":
		return "completed"
	case "blocked":
		return "stopped"
	case "":
		return "active"
	default:
		return strings.TrimSpace(packet.TaskRuntime.State)
	}
}

func appendRunEvents(stream []string, packet ctxpacket.WorkerPacket) []string {
	if action := strings.TrimSpace(packet.LatestExecutionResult.Action); action != "" {
		stream = append(stream, "Command: "+compactForStream(action, 240))
	}
	if summary := strings.TrimSpace(packet.RunningSummary); summary != "" {
		stream = append(stream, "State: "+compactForStream(summary, 280))
	}
	if result := strings.TrimSpace(packet.LatestExecutionResult.OutputSummary); result != "" {
		stream = append(stream, "Result: "+compactForStream(result, 240))
	}
	return stream
}

func compactForStream(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " | "))
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func (s Shell) saveState(state sessionstate.State) error {
	if s.SaveState != nil {
		return s.SaveState(s.StatePath, state)
	}
	return sessionstate.Save(s.StatePath, state)
}

func (s Shell) chat(ctx context.Context, messages []llmclient.Message) (string, error) {
	if s.Chat != nil {
		return s.Chat(ctx, messages)
	}
	return llmclient.Client{BaseURL: s.BaseURL, Model: s.Model}.Chat(ctx, messages)
}

func (s Shell) classifyInput(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
	if s.Classify != nil {
		return s.Classify(ctx, frame, packet, line, started)
	}
	prompt := buildModeClassifierPrompt(packet, line, started)
	resp, err := s.chat(ctx, []llmclient.Message{
		{Role: "system", Content: frame.PromptText()},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return workermode.Decision{}, fmt.Errorf("classify input: %w", err)
	}
	decision, err := workermode.Parse(resp)
	if err != nil {
		return workermode.Decision{}, fmt.Errorf("parse input classification: %w", err)
	}
	if report := workermode.Validate(decision); !report.Valid() {
		return workermode.Decision{}, fmt.Errorf("validate input classification: %w", report.Error())
	}
	return decision, nil
}

func (s Shell) answerConversation(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (string, error) {
	prompt := buildConversationPrompt(packet, line, started)
	resp, err := s.chat(ctx, []llmclient.Message{
		{Role: "system", Content: frame.PromptText()},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return "", fmt.Errorf("conversation reply: %w", err)
	}
	return strings.TrimSpace(resp), nil
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

func persistedSessionStatus(packet ctxpacket.WorkerPacket, runErr error) string {
	if runErr != nil {
		return "stopped"
	}
	switch strings.TrimSpace(packet.TaskRuntime.State) {
	case "done":
		return "completed"
	case "blocked":
		return "stopped"
	case "":
		return "active"
	default:
		return "active"
	}
}

func buildTerminalChatSummary(packet ctxpacket.WorkerPacket, outcomeSummary string, runErr error) string {
	done := strings.TrimSpace(packet.TaskRuntime.State) == "done"
	blocked := strings.TrimSpace(packet.TaskRuntime.State) == "blocked" || runErr != nil

	whatDone := firstNonEmpty(
		strings.TrimSpace(outcomeSummary),
		deriveWhatDone(packet),
		"Completed a worker run.",
	)

	result := firstNonEmpty(
		strings.TrimSpace(packet.LatestExecutionResult.OutputSummary),
		strings.TrimSpace(packet.RunningSummary),
		"(none)",
	)

	next := firstNonEmpty(nextProgressSuggestion(packet, runErr), "ask me to inspect the current state and choose the next step")

	status := "Run complete."
	switch {
	case done:
		status = "Run complete."
	case blocked:
		status = "Run stopped."
	default:
		status = "Run updated."
	}

	return fmt.Sprintf("%s What I did: %s Result: %s Next progress: %s.", status, whatDone, compactForStream(result, 220), next)
}

func deriveWhatDone(packet ctxpacket.WorkerPacket) string {
	action := strings.TrimSpace(packet.LatestExecutionResult.Action)
	if action == "" {
		return ""
	}
	if exit := strings.TrimSpace(packet.LatestExecutionResult.ExitStatus); exit != "" {
		return fmt.Sprintf("executed %q (exit %s)", action, exit)
	}
	return fmt.Sprintf("executed %q", action)
}

func nextProgressSuggestion(packet ctxpacket.WorkerPacket, runErr error) string {
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return "resume the session or rerun with a higher step budget if you want to continue from this state"
		}
		return "inspect the latest log or ask me to try a different method from the current state"
	}
	if strings.TrimSpace(packet.TaskRuntime.State) == "done" {
		return "ask me to produce or refine the final report, or continue with a new target"
	}
	if mf := strings.TrimSpace(packet.TaskRuntime.MissingFact); mf != "" && mf != "(none)" {
		return "help me resolve the missing fact or tell me to try a different method"
	}
	if strings.TrimSpace(packet.PlanState.ActiveStep) != "" {
		return "tell me to continue the active step or adjust the plan if you want a different approach"
	}
	return "tell me what to investigate next"
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
	case "/status":
		if !started {
			_, _ = fmt.Fprintln(w, "no active session")
			return true, nil
		}
		_, _ = fmt.Fprintln(w, renderStatusSummary(packet))
		return true, nil
	case "/step":
		if !started {
			_, _ = fmt.Fprintln(w, "no active session")
			return true, nil
		}
		_, _ = fmt.Fprintln(w, renderStepDetail(packet))
		return true, nil
	case "/packet":
		if !started {
			_, _ = fmt.Fprintln(w, "no active session")
			return true, nil
		}
		_, _ = fmt.Fprintln(w, packet.Render())
		return true, nil
	case "/help":
		_, _ = fmt.Fprintln(w, renderHelp())
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
	case "/plan":
		if !started {
			_, _ = fmt.Fprintln(w, "no active session")
			return true, nil
		}
		_, _ = fmt.Fprintln(w, renderPlan(packet))
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

func renderPlan(packet ctxpacket.WorkerPacket) string {
	plan := packet.PlanState
	if strings.TrimSpace(plan.Mode) == "" || len(plan.Steps) == 0 {
		return "no active plan"
	}
	lines := []string{
		"plan:",
		fmt.Sprintf("- mode: %s", blankOrNone(plan.Mode)),
		fmt.Sprintf("- worker_goal: %s", blankOrNone(plan.WorkerGoal)),
		fmt.Sprintf("- summary: %s", blankOrNone(plan.Summary)),
		"- steps:",
	}
	for _, step := range plan.Steps {
		marker := "  "
		if strings.TrimSpace(step) == strings.TrimSpace(plan.ActiveStep) {
			marker = "* "
		}
		lines = append(lines, marker+step)
	}
	if len(plan.ReplanConditions) == 0 {
		lines = append(lines, "- replan_conditions: (none)")
	} else {
		lines = append(lines, "- replan_conditions:")
		for _, item := range plan.ReplanConditions {
			lines = append(lines, "  - "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func renderStatusSummary(packet ctxpacket.WorkerPacket) string {
	view := BuildViewState(packet, shellSessionStatus(true, packet))
	lines := []string{
		"status:",
		fmt.Sprintf("- goal: %s", blankOrNone(view.Goal)),
		fmt.Sprintf("- worker_goal: %s", blankOrNone(view.WorkerGoal)),
		fmt.Sprintf("- session: %s", blankOrNone(view.SessionStatus)),
		fmt.Sprintf("- task: %s", blankOrNone(view.TaskState)),
		fmt.Sprintf("- target: %s", blankOrNone(view.CurrentTarget)),
		fmt.Sprintf("- active_step: %s", blankOrNone(view.ActiveStep)),
		fmt.Sprintf("- step_state: %s", blankOrNone(string(view.CurrentStepState))),
		fmt.Sprintf("- latest_action_review: %s", blankOrNone(string(view.LatestActionReview))),
		fmt.Sprintf("- latest_step_eval: %s", blankOrNone(string(view.LatestStepEval))),
		fmt.Sprintf("- latest_result: %s", blankOrNone(view.LatestResultSummary)),
		fmt.Sprintf("- latest_failure: %s", blankOrNone(view.LatestFailureClass)),
		fmt.Sprintf("- model: %s", blankOrNone(view.Model)),
		fmt.Sprintf("- context: %s", blankOrNone(view.ContextUsage)),
	}
	return strings.Join(lines, "\n")
}

func renderStepDetail(packet ctxpacket.WorkerPacket) string {
	view := BuildViewState(packet, shellSessionStatus(true, packet))
	lines := []string{
		"step:",
		fmt.Sprintf("- objective: %s", blankOrNone(view.CurrentObjective)),
		fmt.Sprintf("- active_step: %s", blankOrNone(view.ActiveStep)),
		fmt.Sprintf("- state: %s", blankOrNone(string(view.CurrentStepState))),
		fmt.Sprintf("- step_eval: %s", blankOrNone(string(view.LatestStepEval))),
		fmt.Sprintf("- summary: %s", blankOrNone(view.LatestStepSummary)),
		fmt.Sprintf("- latest_command: %s", blankOrNone(view.LatestCommand)),
		fmt.Sprintf("- latest_result: %s", blankOrNone(view.LatestResultSummary)),
	}
	if len(view.PlanSteps) == 0 {
		lines = append(lines, "- plan_steps: (none)")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "- plan_steps:")
	for _, step := range view.PlanSteps {
		lines = append(lines, fmt.Sprintf("  - [%s] %s", step.State, step.Label))
	}
	return strings.Join(lines, "\n")
}

func renderHelp() string {
	return strings.Join([]string{
		"commands:",
		"- /status: compact current worker status",
		"- /step: active step detail and plan step states",
		"- /plan: semantic plan and replan conditions",
		"- /lastlog: latest command log path",
		"- /stats: packet/context size summary",
		"- /packet: full rendered worker packet",
		"- /help: show available commands",
	}, "\n")
}

func buildModeClassifierPrompt(packet ctxpacket.WorkerPacket, line string, started bool) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond with JSON only.",
			`Use: {"mode":"conversation|direct_execution|planned_execution","reason":"short explanation"}.`,
			"Choose conversation for explanation, advice, identity, status, or discussion requests that should not execute tools.",
			"Choose direct_execution for obviously one-step actionable requests such as list/show/pwd/file inspection requests.",
			"Choose planned_execution for multi-phase tasks that require decomposition, verification, or distinct semantic phases.",
			"If uncertain, choose conversation.",
			"Do not emit commands, plan steps, or extra fields.",
		},
		"user_turn": line,
		"started":   started,
	}
	if started {
		payload["current_worker_state"] = packet.RenderWithoutBehaviorFrame()
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func buildConversationPrompt(packet ctxpacket.WorkerPacket, line string, started bool) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond conversationally to the user.",
			"Do not propose or perform tool execution in this reply.",
			"Be concise and helpful.",
		},
		"user_turn": line,
	}
	if started {
		payload["current_worker_state"] = packet.RenderWithoutBehaviorFrame()
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func blankOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
