package interactivecli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextinspect"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextstats"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
	"github.com/Jawbreaker1/CodeHackBot/internal/workertask"
	tea "github.com/charmbracelet/bubbletea"
)

type Runner interface {
	Run(ctx context.Context, packet ctxpacket.WorkerPacket, maxSteps int) (workerloop.Outcome, error)
}

type progressConfigurableRunner interface {
	Runner
	SetProgressSink(sink workerloop.ProgressSink)
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
	Boundary  func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string) (workertask.Decision, error)
}

type runnerProgress struct {
	Event  workerloop.ProgressEvent
	Packet ctxpacket.WorkerPacket
}

type runResult struct {
	outcome workerloop.Outcome
	err     error
}

type channelProgressSink struct {
	ctx context.Context
	ch  chan<- runnerProgress
}

func (s channelProgressSink) EmitProgress(event workerloop.ProgressEvent, packet ctxpacket.WorkerPacket) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.ch <- runnerProgress{Event: event, Packet: packet}:
		return nil
	}
}

func (s Shell) Run(ctx context.Context) error {
	return s.runInteractive(ctx, true)
}

func (s Shell) RunHeadless(ctx context.Context) error {
	return s.runInteractive(ctx, false)
}

func (s Shell) runInteractive(ctx context.Context, allowTUI bool) error {
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

	if !allowTUI || !usesTerminalTUI(s.Reader, s.Writer) {
		return s.runScripted(ctx, frame)
	}

	model := newBubbleModel(ctx, &s, frame)
	options := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithInput(s.Reader),
		tea.WithOutput(s.Writer),
		tea.WithoutSignalHandler(),
	}
	if file, ok := s.Writer.(*os.File); ok && isCharacterDevice(file) {
		options = append(options, tea.WithAltScreen())
	}
	finalModel, err := tea.NewProgram(model, options...).Run()
	if err != nil {
		return fmt.Errorf("run interactive tui: %w", err)
	}
	if fm, ok := finalModel.(bubbleModel); ok && fm.exitErr != nil {
		return fm.exitErr
	}
	return nil
}

func (s Shell) artifacts() artifactRecorder {
	return newArtifactRecorder(s.StatePath)
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

func (s Shell) persistPacketState(packet ctxpacket.WorkerPacket, summary string, runErr error) error {
	state := sessionstate.State{
		Status:   persistedSessionStatus(packet, runErr),
		BaseURL:  s.BaseURL,
		Model:    s.Model,
		MaxSteps: s.MaxSteps,
		AllowAll: s.AllowAll,
		Packet:   packet,
		Summary:  summary,
	}
	if runErr != nil {
		state.LastError = normalizeRunError(context.Background(), runErr)
	}
	return s.saveState(state)
}

func (s Shell) startWorkerRunAsync(ctx context.Context, packet ctxpacket.WorkerPacket) (<-chan runnerProgress, <-chan runResult) {
	var progressCh chan runnerProgress
	if _, ok := s.Runner.(progressConfigurableRunner); ok {
		progressCh = make(chan runnerProgress, 32)
	}
	resultCh := make(chan runResult, 1)
	go func() {
		if runner, ok := s.Runner.(progressConfigurableRunner); ok && progressCh != nil {
			runner.SetProgressSink(channelProgressSink{ctx: ctx, ch: progressCh})
			defer func() {
				runner.SetProgressSink(nil)
				close(progressCh)
			}()
		}
		outcome, err := s.Runner.Run(ctx, packet, s.MaxSteps)
		resultCh <- runResult{outcome: outcome, err: err}
		close(resultCh)
	}()
	return progressCh, resultCh
}

func (s Shell) applyUserInput(ui *UIState, line string) {
	ui.AddUserInput(line)
	_ = s.artifacts().recordTranscript("user", line, ui.Packet)
	_ = s.artifacts().recordEvent("input.received", line, ui.Packet)
}

func (s Shell) recordClassificationResult(packet ctxpacket.WorkerPacket, decision workermode.Decision, classifyErr error) {
	if classifyErr != nil {
		_ = s.artifacts().recordEvent("classification.fallback", artifactEventMessageForClassification(string(decision.Mode), classifyErr), packet)
		return
	}
	_ = s.artifacts().recordEvent("classification.accepted", string(decision.Mode), packet)
}

func (s Shell) applyClassification(ui *UIState, decision workermode.Decision, classifyErr error) workermode.Decision {
	if classifyErr != nil {
		decision = workermode.FallbackDecision()
		ui.AddShellCommand("classification", fmt.Sprintf("input classification failed; fell back to conversation mode\nerror: %v", classifyErr), nil)
	}
	s.recordClassificationResult(ui.Packet, decision, classifyErr)
	return decision
}

func (s Shell) applyConversationReply(ui *UIState, line, reply string) error {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return nil
	}
	ui.AddAssistantReply(reply)
	_ = s.artifacts().recordTranscript("assistant", reply, ui.Packet)
	_ = s.artifacts().recordEvent("conversation.replied", reply, ui.Packet)
	if !ui.Started {
		return nil
	}
	ui.Packet.RecentConversation, ui.Packet.OlderConversationSummary = ctxpacket.AppendConversation(ui.Packet.RecentConversation, ui.Packet.OlderConversationSummary, "User: "+line)
	ui.Packet.RecentConversation, ui.Packet.OlderConversationSummary = ctxpacket.AppendConversation(ui.Packet.RecentConversation, ui.Packet.OlderConversationSummary, "Assistant: "+reply)
	state := sessionstate.State{
		Status:   persistedSessionStatus(ui.Packet, nil),
		BaseURL:  s.BaseURL,
		Model:    s.Model,
		MaxSteps: s.MaxSteps,
		AllowAll: s.AllowAll,
		Packet:   ui.Packet,
		Summary:  reply,
	}
	return s.saveState(state)
}

func (s Shell) applyConversationResult(ui *UIState, line, reply string, err error) error {
	if err != nil {
		ui.AddShellCommand("conversation", "", err)
		_ = s.artifacts().recordEvent("conversation.error", err.Error(), ui.Packet)
		return nil
	}
	return s.applyConversationReply(ui, line, reply)
}

func (s Shell) applyTaskStart(ui *UIState, packet ctxpacket.WorkerPacket, mode string) error {
	ui.Packet = packet
	ui.Started = true
	ui.Packet.OperatorState.ModeHint = mode
	_ = s.artifacts().recordEvent("task.started", ui.Packet.SessionFoundation.Goal, ui.Packet)
	return s.saveState(sessionstate.State{
		Status:   "active",
		BaseURL:  s.BaseURL,
		Model:    s.Model,
		MaxSteps: s.MaxSteps,
		AllowAll: s.AllowAll,
		Packet:   ui.Packet,
	})
}

func (s Shell) applyProgress(ui *UIState, progress runnerProgress) error {
	ui.AddProgressEvent(progress.Event, progress.Packet)
	_ = s.artifacts().recordProgress(progress)
	return s.persistPacketState(ui.Packet, "", nil)
}

func (s Shell) finalizeRun(ctx context.Context, ui *UIState, outcome workerloop.Outcome, runErr error) error {
	ui.Packet = outcome.Packet
	if strings.TrimSpace(ui.Packet.CurrentStep.Objective) == "" {
		ui.Packet.CurrentStep.Objective = ui.Packet.SessionFoundation.Goal
	}
	if strings.TrimSpace(ui.Packet.PlanState.ActiveStep) == "" {
		ui.Packet.PlanState.ActiveStep = ui.Packet.SessionFoundation.Goal
	}
	terminalSummary := buildTerminalChatSummary(ui.Packet, outcome.Summary, runErr)
	if strings.TrimSpace(terminalSummary) != "" {
		ui.Packet.RecentConversation, ui.Packet.OlderConversationSummary = ctxpacket.AppendConversation(ui.Packet.RecentConversation, ui.Packet.OlderConversationSummary, "Assistant: "+terminalSummary)
		_ = s.artifacts().recordTranscript("assistant", terminalSummary, ui.Packet)
	}
	_ = s.artifacts().recordEvent(artifactEventKindForOutcome(ui.Packet, runErr), artifactEventMessageForOutcome(terminalSummary, runErr), ui.Packet)
	ui.AddRunOutcome(ui.Packet, terminalSummary, runErr)
	state := sessionstate.State{
		Status:   persistedSessionStatus(ui.Packet, runErr),
		BaseURL:  s.BaseURL,
		Model:    s.Model,
		MaxSteps: s.MaxSteps,
		AllowAll: s.AllowAll,
		Packet:   ui.Packet,
		Summary:  terminalSummary,
	}
	if runErr != nil {
		state.LastError = normalizeRunError(ctx, runErr)
	}
	return s.saveState(state)
}

func (s Shell) chat(ctx context.Context, messages []llmclient.Message) (string, error) {
	if s.Chat != nil {
		return s.Chat(ctx, messages)
	}
	return llmclient.Client{BaseURL: s.BaseURL, Model: s.Model}.Chat(ctx, messages)
}

func (s Shell) classifyInput(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
	attempt := workermode.AttemptRecord{
		Prompt: buildModeClassifierPrompt(packet, line, started),
	}
	acceptDecision := func(decision workermode.Decision) (workermode.Decision, error) {
		attempt.Parsed = decision
		attempt.Validation = workermode.Validate(decision)
		if !attempt.Validation.Valid() {
			normalized := workermode.NormalizeConciseReason(decision)
			if normalized != decision {
				attempt.Parsed = normalized
				attempt.Validation = workermode.Validate(normalized)
				if attempt.Validation.Valid() {
					attempt.Accepted = true
					_ = s.captureClassificationAttempt(attempt)
					return normalized, nil
				}
			}
			attempt.FinalError = attempt.Validation.Error().Error()
			_ = s.captureClassificationAttempt(attempt)
			return workermode.Decision{}, fmt.Errorf("validate input classification: %w", attempt.Validation.Error())
		}
		attempt.Accepted = true
		_ = s.captureClassificationAttempt(attempt)
		return decision, nil
	}
	if s.Classify != nil {
		decision, err := s.Classify(ctx, frame, packet, line, started)
		if err != nil {
			attempt.FinalError = fmt.Sprintf("classify input: %v", err)
			_ = s.captureClassificationAttempt(attempt)
			return workermode.Decision{}, err
		}
		return acceptDecision(decision)
	}
	resp, err := s.chat(ctx, []llmclient.Message{
		{Role: "system", Content: frame.PromptText()},
		{Role: "user", Content: attempt.Prompt},
	})
	attempt.RawResponse = resp
	if err != nil {
		attempt.FinalError = fmt.Sprintf("classify input: %v", err)
		_ = s.captureClassificationAttempt(attempt)
		return workermode.Decision{}, fmt.Errorf("classify input: %w", err)
	}
	decision, err := workermode.Parse(resp)
	if err != nil {
		attempt.FinalError = fmt.Sprintf("parse input classification: %v", err)
		_ = s.captureClassificationAttempt(attempt)
		return workermode.Decision{}, fmt.Errorf("parse input classification: %w", err)
	}
	return acceptDecision(decision)
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

func (s Shell) captureClassificationAttempt(attempt workermode.AttemptRecord) error {
	contextDir := filepath.Join(filepath.Dir(s.StatePath), "context")
	return contextinspect.Recorder{Dir: contextDir}.CaptureClassificationAttempt(attempt)
}

func (s Shell) captureTaskBoundaryAttempt(attempt workertask.AttemptRecord) error {
	contextDir := filepath.Join(filepath.Dir(s.StatePath), "context")
	return contextinspect.Recorder{Dir: contextDir}.CaptureTaskBoundaryAttempt(attempt)
}

func (s Shell) decideTaskBoundary(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string) (workertask.Decision, error) {
	attempt := workertask.AttemptRecord{
		Prompt: buildTaskBoundaryPrompt(packet, line),
	}
	if s.Boundary != nil {
		decision, err := s.Boundary(ctx, frame, packet, line)
		attempt.Parsed = decision
		if err != nil {
			attempt.FinalError = fmt.Sprintf("decide task boundary: %v", err)
			_ = s.captureTaskBoundaryAttempt(attempt)
			return workertask.Decision{}, err
		}
		attempt.Validation = workertask.Validate(decision)
		attempt.Accepted = attempt.Validation.Valid()
		if !attempt.Accepted {
			attempt.FinalError = attempt.Validation.Error().Error()
		}
		_ = s.captureTaskBoundaryAttempt(attempt)
		if !attempt.Accepted {
			return workertask.Decision{}, fmt.Errorf("validate task boundary decision: %w", attempt.Validation.Error())
		}
		return decision, nil
	}
	resp, err := s.chat(ctx, []llmclient.Message{
		{Role: "system", Content: frame.PromptText()},
		{Role: "user", Content: attempt.Prompt},
	})
	attempt.RawResponse = resp
	if err != nil {
		attempt.FinalError = fmt.Sprintf("decide task boundary: %v", err)
		_ = s.captureTaskBoundaryAttempt(attempt)
		return workertask.Decision{}, fmt.Errorf("decide task boundary: %w", err)
	}
	decision, err := workertask.Parse(resp)
	if err != nil {
		attempt.FinalError = fmt.Sprintf("parse task boundary: %v", err)
		_ = s.captureTaskBoundaryAttempt(attempt)
		return workertask.Decision{}, fmt.Errorf("parse task boundary: %w", err)
	}
	attempt.Parsed = decision
	attempt.Validation = workertask.Validate(decision)
	if !attempt.Validation.Valid() {
		attempt.FinalError = fmt.Sprintf("validate task boundary decision: %v", attempt.Validation.Error())
		_ = s.captureTaskBoundaryAttempt(attempt)
		return workertask.Decision{}, fmt.Errorf("validate task boundary decision: %w", attempt.Validation.Error())
	}
	attempt.Accepted = true
	_ = s.captureTaskBoundaryAttempt(attempt)
	return decision, nil
}

func (s Shell) prepareTaskPacket(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, started bool, line string) (ctxpacket.WorkerPacket, bool, error) {
	if !started {
		next, err := newInitialPacket(frame, line, s.Model, s.AllowAll, s.MaxSteps)
		return next, true, err
	}
	state := strings.TrimSpace(packet.TaskRuntime.State)
	switch state {
	case "", "done", "blocked":
		next, err := newTaskPacketFromPrevious(frame, packet, line, s.Model, s.AllowAll, s.MaxSteps)
		return next, true, err
	}
	decision, err := s.decideTaskBoundary(ctx, frame, packet, line)
	if err != nil {
		decision = workertask.FallbackDecision()
	}
	if decision.Action == workertask.ActionStartNewTask {
		next, err := newTaskPacketFromPrevious(frame, packet, line, s.Model, s.AllowAll, s.MaxSteps)
		return next, true, err
	}
	packet.RecentConversation, packet.OlderConversationSummary = ctxpacket.AppendConversation(packet.RecentConversation, packet.OlderConversationSummary, "User: "+line)
	return packet, false, nil
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

	result := deriveChatResultSummary(packet, runErr)

	next := firstNonEmpty(nextProgressSuggestion(packet, runErr), "ask me to inspect the current state and choose the next step")

	if done && runErr == nil {
		lines := []string{"Done.", "Completed your request."}
		if concise := conciseSuccessSummary(outcomeSummary); concise != "" {
			lines[1] = concise
		}
		if strings.TrimSpace(result) != "" && result != "(none)" {
			lines = append(lines, result)
		}
		if strings.TrimSpace(next) != "" {
			lines = append(lines, "If you want, I can "+strings.TrimSuffix(strings.TrimSpace(next), ".")+".")
		}
		return strings.Join(lines, "\n")
	}

	status := "Run complete."
	switch {
	case done:
		status = "Run complete."
	case blocked:
		status = "Run stopped."
	default:
		status = "Run updated."
	}

	return strings.Join([]string{
		status,
		"What I did:",
		"- " + strings.TrimSpace(whatDone),
		"Result:",
		indentBlock(formatStreamBody(result, 8, 700), "  "),
		"Next progress:",
		"- " + strings.TrimSpace(next),
	}, "\n")
}

func conciseSuccessSummary(outcomeSummary string) string {
	outcomeSummary = strings.TrimSpace(outcomeSummary)
	if outcomeSummary == "" {
		return ""
	}
	line := strings.TrimSpace(strings.Split(outcomeSummary, "\n")[0])
	if line == "" {
		return ""
	}
	if len(line) > 160 {
		line = line[:157] + "..."
	}
	return line
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
		if latestLogPath(packet) != "" {
			return "use /fulloutput, inspect the latest log, or continue with a new target"
		}
		return "continue with a new target"
	}
	if mf := strings.TrimSpace(packet.TaskRuntime.MissingFact); mf != "" && mf != "(none)" {
		return "help me resolve the missing fact or tell me to try a different method"
	}
	if strings.TrimSpace(packet.PlanState.ActiveStep) != "" {
		return "tell me to continue the active step or adjust the plan if you want a different approach"
	}
	return "tell me what to investigate next"
}

func indentBlock(s, prefix string) string {
	if strings.TrimSpace(s) == "" {
		return prefix + "(none)"
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func deriveChatResultSummary(packet ctxpacket.WorkerPacket, runErr error) string {
	raw := firstNonEmpty(
		strings.TrimSpace(preferredOutputForChat(packet.LatestExecutionResult)),
		strings.TrimSpace(packet.RunningSummary),
		"(none)",
	)
	if shouldShowRawResult(packet, runErr) {
		return formatStreamBody(raw, 8, 700)
	}
	if preview := deriveSuccessfulOutputPreview(packet); preview != "" {
		return preview
	}
	if snippet := conciseOutputSnippet(raw); snippet != "" {
		return snippet
	}
	if latestLogPath(packet) != "" {
		return "Output captured successfully. Use /fulloutput to inspect the full command output."
	}
	return "Output captured successfully."
}

func deriveSuccessfulOutputPreview(packet ctxpacket.WorkerPacket) string {
	preview := latestOutputPreview(packet)
	if strings.TrimSpace(preview) == "" {
		return ""
	}
	lines := []string{"Preview:"}
	lines = append(lines, indentBlock(preview, "  "))
	if latestLogPath(packet) != "" {
		lines = append(lines, "Use /fulloutput to inspect the full command output.")
	}
	return strings.Join(lines, "\n")
}

func latestOutputPreview(packet ctxpacket.WorkerPacket) string {
	if logPath := latestLogPath(packet); strings.TrimSpace(logPath) != "" {
		stdout, stderr, err := readExecutionLogOutput(logPath)
		if err == nil {
			if preview := boundedPreviewBlock(stdout, 4, 320); preview != "" {
				return preview
			}
			if preview := boundedPreviewBlock(stderr, 4, 320); preview != "" {
				return preview
			}
		}
	}
	return boundedPreviewBlock(normalizeOutputSnippet(preferredOutputForChat(packet.LatestExecutionResult)), 4, 320)
}

func boundedPreviewBlock(raw string, maxLines, maxChars int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "(none)" {
		return ""
	}
	return limitOutputForChat(raw, maxLines, maxChars)
}

func readExecutionLogOutput(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	text := string(data)
	stdout := extractLogSection(text, "[stdout]", "[stderr]")
	stderr := extractLogSection(text, "[stderr]", "")
	return strings.TrimSpace(stdout), strings.TrimSpace(stderr), nil
}

func extractLogSection(text, startMarker, endMarker string) string {
	start := strings.Index(text, startMarker)
	if start < 0 {
		return ""
	}
	section := text[start+len(startMarker):]
	section = strings.TrimPrefix(section, "\n")
	if endMarker != "" {
		if end := strings.Index(section, endMarker); end >= 0 {
			section = section[:end]
		}
	}
	return strings.TrimSpace(section)
}

func renderFullOutputReply(packet ctxpacket.WorkerPacket, stdout, stderr string) string {
	action := strings.TrimSpace(packet.LatestExecutionResult.Action)
	lines := []string{"Done."}
	if action != "" {
		lines = append(lines, fmt.Sprintf("Here is the full output from `%s`.", action))
	} else {
		lines = append(lines, "Here is the full output from the latest command.")
	}
	if strings.TrimSpace(stdout) != "" && stdout != "(none)" {
		lines = append(lines, "stdout:")
		lines = append(lines, indentBlock(limitOutputForChat(stdout, 80, 12000), "  "))
	}
	if strings.TrimSpace(stderr) != "" && stderr != "(none)" {
		lines = append(lines, "stderr:")
		lines = append(lines, indentBlock(limitOutputForChat(stderr, 40, 6000), "  "))
	}
	if latestLogPath(packet) != "" {
		lines = append(lines, fmt.Sprintf("Latest log: %s", latestLogPath(packet)))
	}
	return strings.Join(lines, "\n")
}

func limitOutputForChat(s string, maxLines, maxChars int) string {
	s = strings.TrimSpace(s)
	if maxChars > 0 && len(s) > maxChars {
		s = strings.TrimSpace(s[:maxChars]) + "\n..."
	}
	lines := strings.Split(s, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = append(lines[:maxLines], "...")
	}
	return strings.Join(lines, "\n")
}

func shouldShowRawResult(packet ctxpacket.WorkerPacket, runErr error) bool {
	if runErr != nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(packet.TaskRuntime.State), "blocked") {
		return true
	}
	assessment := strings.TrimSpace(packet.LatestExecutionResult.Assessment)
	if assessment != "" && assessment != "success" {
		return true
	}
	if strings.TrimSpace(packet.LatestExecutionResult.FailureClass) != "" {
		return true
	}
	return false
}

func conciseOutputSnippet(raw string) string {
	raw = normalizeOutputSnippet(raw)
	if raw == "" || raw == "(none)" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	if len(lines) == 1 && len(strings.TrimSpace(lines[0])) <= 120 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

func preferredOutputForChat(result ctxpacket.ExecutionResult) string {
	if strings.TrimSpace(result.OutputEvidence) != "" && strings.TrimSpace(result.OutputEvidence) != "(none)" {
		return result.OutputEvidence
	}
	return result.OutputSummary
}

func normalizeOutputSnippet(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "stdout: ")
	raw = strings.TrimPrefix(raw, "stderr: ")
	return strings.TrimSpace(raw)
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
	case "/fulloutput":
		if !started {
			_, _ = fmt.Fprintln(w, "no active session")
			return true, nil
		}
		logPath := latestLogPath(packet)
		if strings.TrimSpace(logPath) == "" {
			_, _ = fmt.Fprintln(w, "(none)")
			return true, nil
		}
		stdout, stderr, err := readExecutionLogOutput(logPath)
		if err != nil {
			return true, fmt.Errorf("read latest command output: %w", err)
		}
		_, _ = fmt.Fprintln(w, renderFullOutputReply(packet, stdout, stderr))
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
	return ctxpacket.NewInitialWorkerPacket(frame, foundation, cwd, model, approvalStateLabel(allowAll), maxSteps), nil
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

func newTaskPacketFromPrevious(frame behavior.Frame, previous ctxpacket.WorkerPacket, goal, model string, allowAll bool, maxSteps int) (ctxpacket.WorkerPacket, error) {
	next, err := newInitialPacket(frame, goal, model, allowAll, maxSteps)
	if err != nil {
		return ctxpacket.WorkerPacket{}, err
	}
	next.OlderConversationSummary = ctxpacket.CarryConversationSummary(previous.OlderConversationSummary, previous.RecentConversation)
	next.RelevantRecentResults = carryRecentResults(previous)
	return next, nil
}

func carryRecentResults(previous ctxpacket.WorkerPacket) []ctxpacket.ExecutionResult {
	results := make([]ctxpacket.ExecutionResult, 0, 1+len(previous.RelevantRecentResults))
	if hasExecutionEvidence(previous.LatestExecutionResult) {
		results = append(results, previous.LatestExecutionResult)
	}
	for _, result := range previous.RelevantRecentResults {
		if !hasExecutionEvidence(result) {
			continue
		}
		results = append(results, result)
		if len(results) >= 4 {
			break
		}
	}
	if len(results) == 0 {
		return nil
	}
	return results
}

func hasExecutionEvidence(result ctxpacket.ExecutionResult) bool {
	return strings.TrimSpace(result.Action) != "" ||
		strings.TrimSpace(result.OutputSummary) != "" ||
		len(result.LogRefs) > 0 ||
		len(result.ArtifactRefs) > 0
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
		"- /fulloutput: show full stdout/stderr from the latest command",
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
		payload["session_context"] = map[string]any{
			"active_goal":         blankOrNone(packet.SessionFoundation.Goal),
			"task_state":          blankOrNone(packet.TaskRuntime.State),
			"active_step":         blankOrNone(packet.PlanState.ActiveStep),
			"recent_conversation": packet.RecentConversation,
			"older_conversation":  blankOrNone(packet.OlderConversationSummary),
			"working_dir":         blankOrNone(packet.OperatorState.WorkingDir),
		}
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func buildTaskBoundaryPrompt(packet ctxpacket.WorkerPacket, line string) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond with JSON only.",
			`Use: {"action":"continue_active_task|start_new_task","reason":"short explanation"}.`,
			"Choose continue_active_task only when the latest user turn clearly refines, continues, or redirects the current active task without replacing it.",
			"Choose start_new_task when the latest user turn introduces a new goal that should replace the current active task.",
			"If uncertain, choose start_new_task.",
			"Do not emit commands, plan steps, or extra fields.",
		},
		"user_turn": line,
		"current_task": map[string]any{
			"goal":        packet.SessionFoundation.Goal,
			"state":       packet.TaskRuntime.State,
			"active_step": packet.PlanState.ActiveStep,
		},
		"current_worker_state": packet.RenderWithoutBehaviorFrame(),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func buildConversationPrompt(packet ctxpacket.WorkerPacket, line string, started bool) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond conversationally to the user.",
			"Do not propose or perform tool execution in this reply.",
			"Be concise, natural, and helpful.",
			"Do not restate authorization or policy unless the user asks or it is directly relevant.",
		},
		"user_turn": line,
	}
	if started {
		sessionContext := map[string]any{
			"active_goal":         blankOrNone(packet.SessionFoundation.Goal),
			"task_state":          blankOrNone(packet.TaskRuntime.State),
			"recent_conversation": packet.RecentConversation,
			"older_conversation":  blankOrNone(packet.OlderConversationSummary),
			"working_dir":         blankOrNone(packet.OperatorState.WorkingDir),
		}
		payload["session_context"] = sessionContext
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
