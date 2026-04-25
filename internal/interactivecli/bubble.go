package interactivecli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type classifiedMsg struct {
	line     string
	decision workermode.Decision
	err      error
}

type conversationMsg struct {
	reply string
	err   error
}

type runFinishedMsg struct {
	outcome workerloop.Outcome
	err     error
}

type runnerProgressMsg struct {
	progress runnerProgress
}

type progressClosedMsg struct{}

type bubbleModel struct {
	ctx    context.Context
	cancel context.CancelFunc
	shell  *Shell
	frame  behavior.Frame

	ui UIState

	spinner        spinner.Model
	input          textinput.Model
	streamViewport viewport.Model
	statusViewport viewport.Model
	width          int
	height         int
	ready          bool
	busy           bool
	busyLabel      string
	exitErr        error
	pendingLine    string
	progressCh     <-chan runnerProgress
	resultCh       <-chan runResult
	pendingRun     *runFinishedMsg
}

func newBubbleModel(parent context.Context, shell *Shell, frame behavior.Frame) bubbleModel {
	runCtx, cancel := context.WithCancel(parent)
	input := textinput.New()
	input.Prompt = "birdhackbot> "
	input.Placeholder = "Type a request or /help"
	input.Focus()
	input.CharLimit = 0
	input.Width = 80
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = busyStyle()

	m := bubbleModel{
		ctx:     runCtx,
		cancel:  cancel,
		shell:   shell,
		frame:   frame,
		ui:      NewUIState(),
		spinner: spin,
		input:   input,
		width:   120,
		height:  32,
	}
	cwd, _ := os.Getwd()
	m.ui.SetEnvironment(shell.Model, cwd, approvalStateLabel(shell.AllowAll))
	m.streamViewport = viewport.New(80, 20)
	m.statusViewport = viewport.New(36, 20)
	m.syncLayout()
	return m
}

func (m bubbleModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

func (m bubbleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.syncLayout()
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.syncLayout()
		return m, nil

	case classifiedMsg:
		msg.decision = m.shell.applyClassification(&m.ui, msg.decision, msg.err)
		if msg.decision.Mode == workerplan.ModeConversation {
			m.busyLabel = "thinking"
			return m, tea.Batch(m.spinner.Tick, m.conversationCmd(msg.line))
		}

		started := m.ui.Started && strings.TrimSpace(m.ui.Packet.SessionFoundation.Goal) != ""
		packet, _, err := m.shell.prepareTaskPacket(m.ctx, m.frame, m.ui.Packet, started, msg.line)
		if err != nil {
			m.busy = false
			m.ui.AddShellCommand("worker", "", err)
			m.syncLayout()
			return m, nil
		}
		if err := m.shell.applyTaskStart(&m.ui, packet, string(msg.decision.Mode)); err != nil {
			m.busy = false
			m.ui.AddShellCommand("save", "", err)
			m.syncLayout()
			return m, nil
		}
		m.syncLayout()
		m.busyLabel = "running"
		return m, m.startWorkerRun(m.ui.Packet)

	case conversationMsg:
		m.busy = false
		m.busyLabel = ""
		if err := m.shell.applyConversationResult(&m.ui, m.pendingLine, msg.reply, msg.err); err != nil {
			m.ui.AddShellCommand("save", "", fmt.Errorf("save session state after conversation: %w", err))
		}
		m.pendingLine = ""
		m.syncLayout()
		return m, nil

	case runFinishedMsg:
		if m.progressCh != nil {
			copy := msg
			m.pendingRun = &copy
			return m, nil
		}
		return m.finishRun(msg)

	case runnerProgressMsg:
		if err := m.shell.applyProgress(&m.ui, msg.progress); err != nil {
			m.ui.AddShellCommand("save", "", err)
		}
		m.syncLayout()
		if m.progressCh != nil {
			return m, m.waitProgressCmd(m.progressCh)
		}
		return m, nil

	case progressClosedMsg:
		m.progressCh = nil
		if m.pendingRun != nil {
			pending := *m.pendingRun
			m.pendingRun = nil
			return m.finishRun(pending)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancel()
			m.exitErr = context.Canceled
			return m, tea.Quit
		case "enter":
			if m.busy {
				return m, nil
			}
			line := strings.TrimSpace(m.input.Value())
			if line == "" {
				return m, nil
			}
			m.input.SetValue("")
			if line == "exit" || line == "quit" {
				return m, tea.Quit
			}
			if strings.HasPrefix(line, "/") {
				var cmdOut strings.Builder
				if handled, cmdErr := handleShellCommand(&cmdOut, line, m.ui.Started, m.ui.Packet); handled {
					m.ui.AddShellCommand(line, cmdOut.String(), cmdErr)
					m.syncLayout()
					return m, nil
				}
			}
			m.pendingLine = line
			m.shell.applyUserInput(&m.ui, line)
			m.busy = true
			m.busyLabel = "classifying"
			m.syncLayout()
			return m, tea.Batch(m.spinner.Tick, m.classifyCmd(line))
		}
		if m.busy {
			return m, nil
		}
	}

	if !m.busy {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.syncLayout()
		return m, cmd
	}
	return m, nil
}

func (m bubbleModel) View() string {
	m.syncLayout()
	streamTitle := titleStyle().Render(" Stream ")
	statusTitle := titleStyle().Render(" Status ")
	inputTitle := titleStyle().Render(" Input ")
	if m.busy {
		label := strings.TrimSpace(m.busyLabel)
		if label == "" {
			label = "busy"
		}
		inputTitle = titleStyle().Render(" Input " + m.spinner.View() + " " + label + " ")
	}

	top := lipgloss.JoinHorizontal(lipgloss.Top,
		paneStyle().Width(m.streamViewport.Width+2).Height(m.streamViewport.Height+2).Render(streamTitle+"\n"+m.streamViewport.View()),
		paneStyle().Width(m.statusViewport.Width+2).Height(m.statusViewport.Height+2).Render(statusTitle+"\n"+m.statusViewport.View()),
	)
	inputHelp := "Commands: /status /step /plan /fulloutput /lastlog /stats /packet /help"
	inputBody := lipgloss.JoinVertical(lipgloss.Left,
		m.input.View(),
		mutedStyle().Width(max(1, m.width-4)).Render(inputHelp),
	)
	bottom := paneStyle().Width(max(20, m.width-2)).Render(inputTitle + "\n" + inputBody)
	return lipgloss.JoinVertical(lipgloss.Left, top, bottom)
}

func (m bubbleModel) classifyCmd(line string) tea.Cmd {
	packet := m.ui.Packet
	started := m.ui.Started
	frame := m.frame
	return func() tea.Msg {
		decision, err := m.shell.classifyInput(m.ctx, frame, packet, line, started)
		return classifiedMsg{line: line, decision: decision, err: err}
	}
}

func (m bubbleModel) conversationCmd(line string) tea.Cmd {
	packet := m.ui.Packet
	started := m.ui.Started
	frame := m.frame
	return func() tea.Msg {
		reply, err := m.shell.answerConversation(m.ctx, frame, packet, line, started)
		return conversationMsg{reply: reply, err: err}
	}
}

func (m *bubbleModel) startWorkerRun(packet ctxpacket.WorkerPacket) tea.Cmd {
	progressCh, resultCh := m.shell.startWorkerRunAsync(m.ctx, packet)
	m.progressCh = progressCh
	m.resultCh = resultCh
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.waitRunResultCmd(resultCh),
	}
	if progressCh != nil {
		cmds = append(cmds, m.waitProgressCmd(progressCh))
	}
	return tea.Batch(cmds...)
}

func (m bubbleModel) waitRunResultCmd(ch <-chan runResult) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return runFinishedMsg{}
		}
		return runFinishedMsg{outcome: result.outcome, err: result.err}
	}
}

func (m bubbleModel) waitProgressCmd(ch <-chan runnerProgress) tea.Cmd {
	return func() tea.Msg {
		progress, ok := <-ch
		if !ok {
			return progressClosedMsg{}
		}
		return runnerProgressMsg{progress: progress}
	}
}

func (m bubbleModel) finishRun(msg runFinishedMsg) (tea.Model, tea.Cmd) {
	m.resultCh = nil
	m.busy = false
	m.busyLabel = ""
	if err := m.shell.finalizeRun(m.ctx, &m.ui, msg.outcome, msg.err); err != nil {
		m.ui.AddShellCommand("save", "", err)
	}
	if msg.err != nil && (m.ctx.Err() != nil || msg.err == context.Canceled || msg.err == context.DeadlineExceeded) {
		m.exitErr = msg.err
	}
	m.pendingLine = ""
	m.syncLayout()
	return m, nil
}

func (m *bubbleModel) syncLayout() {
	totalWidth := m.width
	if totalWidth <= 0 {
		totalWidth = 120
	}
	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 32
	}

	gap := 1
	rightWidth := max(28, minInt(42, totalWidth/3))
	leftWidth := totalWidth - rightWidth - gap - 2
	if leftWidth < 40 {
		leftWidth = max(30, totalWidth-30-gap-2)
		rightWidth = max(24, totalWidth-leftWidth-gap-2)
	}
	topHeight := max(10, totalHeight-6)
	innerHeight := max(5, topHeight-2)
	m.streamViewport.Width = max(20, leftWidth-2)
	m.streamViewport.Height = innerHeight
	m.statusViewport.Width = max(20, rightWidth-2)
	m.statusViewport.Height = innerHeight
	m.input.Width = max(20, totalWidth-8)

	m.streamViewport.SetContent(renderStreamContent(m.ui.Stream, m.streamViewport.Width))
	m.streamViewport.GotoBottom()
	m.statusViewport.SetContent(renderStatusContent(m.ui.View()))
	m.statusViewport.GotoTop()
}

func renderStatusContent(view ViewState) string {
	sections := []string{
		renderSection("Goal", firstNonEmpty(view.WorkerGoal, view.Goal, "(none)")),
		renderSection("Plan", renderPlanBlock(view)),
		renderSection("Step", renderStepBlock(view)),
		renderSection("Latest", renderLatestBlock(view)),
		renderSection("Environment", renderRuntimeBlock(view)),
	}
	return strings.Join(sections, "\n\n")
}

func renderPlanBlock(view ViewState) string {
	lines := []string{renderKV("summary", blankOrNone(view.PlanSummary))}
	if len(view.PlanSteps) == 0 {
		lines = append(lines, renderKV("steps", "(none)"))
		return strings.Join(lines, "\n")
	}
	lines = append(lines, labelStyle().Render("steps")+":")
	for _, step := range view.PlanSteps {
		marker := mutedStyle().Render("-")
		switch step.State {
		case StepVisualDone:
			marker = statusDoneStyle().Render("x")
		case StepVisualInProgress:
			marker = statusActiveStyle().Render(">")
		case StepVisualBlocked:
			marker = statusBlockedStyle().Render("!")
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, valueStyle().Render(step.Label)))
	}
	return strings.Join(lines, "\n")
}

func renderStepBlock(view ViewState) string {
	lines := []string{
		renderKV("objective", blankOrNone(view.CurrentObjective)),
		renderKV("state", renderStepState(view.CurrentStepState)),
		renderKV("active", blankOrNone(view.ActiveStep)),
		renderKV("step_eval", renderStepState(view.LatestStepEval)),
	}
	if strings.TrimSpace(view.LatestStepSummary) != "" {
		lines = append(lines, renderKV("summary", view.LatestStepSummary))
	}
	if review := blankOrNone(string(view.LatestActionReview)); review != "(none)" && review != "unknown" {
		lines = append(lines, renderKV("action_review", renderReviewState(view.LatestActionReview)))
	}
	return strings.Join(lines, "\n")
}

func renderLatestBlock(view ViewState) string {
	lines := []string{
		renderKV("command", blankOrNone(view.LatestCommand)),
		renderKV("result", blankOrNone(view.LatestResultSummary)),
		renderKV("assessment", blankOrNone(view.LatestAssessment)),
	}
	if strings.TrimSpace(view.LatestFailureClass) != "" {
		lines = append(lines, renderKV("failure", view.LatestFailureClass))
	}
	if len(view.LatestSignals) > 0 {
		lines = append(lines, renderKV("signals", strings.Join(view.LatestSignals, ", ")))
	}
	if strings.TrimSpace(view.LatestLog) != "" {
		lines = append(lines, renderKV("log", view.LatestLog))
	}
	return strings.Join(lines, "\n")
}

func renderRuntimeBlock(view ViewState) string {
	lines := []string{
		renderKV("cwd", blankOrNone(view.WorkingDir)),
		renderKV("model", blankOrNone(view.Model)),
		renderKV("session", blankOrNone(view.SessionStatus)),
		renderKV("task", blankOrNone(view.TaskState)),
		renderKV("scope", blankOrNone(view.ScopeState)),
		renderKV("approval", blankOrNone(view.ApprovalState)),
	}
	if strings.TrimSpace(view.CurrentTarget) != "" && view.CurrentTarget != "(none)" {
		lines = append(lines, renderKV("target", view.CurrentTarget))
	}
	if strings.TrimSpace(view.ReportingRequirement) != "" && view.ReportingRequirement != "(none)" {
		lines = append(lines, renderKV("reporting", view.ReportingRequirement))
	}
	if strings.TrimSpace(view.ContextUsage) != "" && view.ContextUsage != "(none)" {
		lines = append(lines, renderKV("context", view.ContextUsage))
	}
	if strings.TrimSpace(view.MissingFact) != "" && view.MissingFact != "(none)" {
		lines = append(lines, renderKV("missing", view.MissingFact))
	}
	return strings.Join(lines, "\n")
}

func paneStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
}

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("60"))
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
}

func busyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
}

func sectionTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
}

func labelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
}

func valueStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
}

func userStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
}

func assistantStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("114"))
}

func commandStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
}

func systemStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("183"))
}

func errorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
}

func stateStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
}

func resultStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("222"))
}

func statusDoneStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("114"))
}

func statusActiveStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
}

func statusBlockedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
}

func renderSection(title, body string) string {
	return sectionTitleStyle().Render(title) + "\n" + body
}

func renderKV(key, value string) string {
	return labelStyle().Render(key) + ": " + valueStyle().Render(value)
}

func renderStepState(state StepVisualState) string {
	switch state {
	case StepVisualDone:
		return statusDoneStyle().Render(string(state))
	case StepVisualInProgress:
		return statusActiveStyle().Render(string(state))
	case StepVisualBlocked:
		return statusBlockedStyle().Render(string(state))
	default:
		return mutedStyle().Render(string(state))
	}
}

func renderReviewState(state ReviewVisualState) string {
	switch state {
	case ReviewVisualExecute:
		return statusDoneStyle().Render(string(state))
	case ReviewVisualRevise:
		return statusActiveStyle().Render(string(state))
	case ReviewVisualBlocked:
		return statusBlockedStyle().Render(string(state))
	default:
		return mutedStyle().Render(string(state))
	}
}

func renderStreamContent(entries []string, width int) string {
	if len(entries) == 0 {
		return ""
	}
	var rendered []string
	bodyWidth := max(10, width-2)
	for _, entry := range entries {
		kind, body := splitStreamEntry(entry)
		label, hasLabel := renderStreamLabel(kind)
		if strings.TrimSpace(body) == "" {
			if hasLabel {
				rendered = append(rendered, label)
			} else {
				rendered = append(rendered, valueStyle().Render(kind))
			}
			continue
		}
		if !hasLabel {
			rendered = append(rendered, valueStyle().Width(bodyWidth).Render(strings.TrimSpace(body)))
			continue
		}
		rendered = append(rendered, renderLabeledBlock(label, strings.TrimSpace(body), width)...)
	}
	return strings.Join(rendered, "\n")
}

func renderLabeledBlock(label, body string, width int) []string {
	firstWidth := max(8, width-visibleWidth(label)-1)
	const continuationIndent = "  "
	restWidth := max(8, width-len(continuationIndent))
	paragraphs := strings.Split(body, "\n")
	lines := make([]string, 0, len(paragraphs))
	firstLine := true
	for _, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			if firstLine {
				lines = append(lines, label)
				firstLine = false
			} else {
				lines = append(lines, "")
			}
			continue
		}
		wrapped := wrapParagraphForPane(paragraph, firstWidth, restWidth)
		for _, wrappedLine := range wrapped {
			if firstLine {
				lines = append(lines, label+" "+wrappedLine)
				firstLine = false
				continue
			}
			lines = append(lines, continuationIndent+wrappedLine)
		}
	}
	return lines
}

func wrapParagraphForPane(paragraph string, firstWidth, restWidth int) []string {
	paragraph = strings.TrimSpace(paragraph)
	if paragraph == "" {
		return nil
	}
	words := strings.Fields(paragraph)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 4)
	currentWidth := firstWidth
	current := words[0]
	currentLen := lipgloss.Width(words[0])
	for _, word := range words[1:] {
		wordLen := lipgloss.Width(word)
		if currentLen+1+wordLen <= currentWidth {
			current += " " + word
			currentLen += 1 + wordLen
			continue
		}
		lines = append(lines, current)
		current = word
		currentLen = wordLen
		currentWidth = restWidth
	}
	lines = append(lines, current)
	return lines
}

func visibleWidth(s string) int {
	return lipgloss.Width(s)
}

func splitStreamEntry(entry string) (string, string) {
	if idx := strings.Index(entry, ": "); idx >= 0 {
		return entry[:idx], entry[idx+2:]
	}
	return entry, ""
}

func renderStreamLabel(kind string) (string, bool) {
	switch kind {
	case "User":
		return userStyle().Render("User:"), true
	case "Assistant":
		return assistantStyle().Render("Assistant:"), true
	case "Command":
		return commandStyle().Render("Command:"), true
	case "System":
		return systemStyle().Render("System:"), true
	case "Error":
		return errorStyle().Render("Error:"), true
	case "State":
		return stateStyle().Render("State:"), true
	case "Result":
		return resultStyle().Render("Result:"), true
	default:
		return "", false
	}
}

func isCharacterDevice(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
