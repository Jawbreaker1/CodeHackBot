package guided

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

func wantsGuidedTUI(reader io.Reader, writer io.Writer) bool {
	if os.Getenv("BIRDHACKBOT_PLAIN") == "1" {
		return false
	}
	in, inOK := reader.(*os.File)
	out, outOK := writer.(*os.File)
	return inOK && outOK && term.IsTerminal(in.Fd()) && term.IsTerminal(out.Fd())
}

type tuiOutput struct {
	kind          consoleEventKind
	text          string
	promptMode    promptMode
	choices       []consoleChoice
	defaultChoice int
	working       bool
	activeWorkers int
}
type tuiDone struct{ err error }

type guidedTUI struct {
	ctx                context.Context
	cancel             context.CancelFunc
	input              textinput.Model
	inputPipe          io.WriteCloser
	spinner            spinner.Model
	conversation       viewport.Model
	output             <-chan tuiOutput
	done               <-chan error
	lines              []string
	width, height      int
	busy               bool
	spinnerRunning     bool
	coordinatorWorking bool
	activeWorkers      int
	waiting            bool
	active             bool
	sessionReady       bool
	inputPrompt        string
	inputContext       string
	inputMode          promptMode
	approvalDetails    string
	permissionLabel    string
	workerStatus       string
	coordinatorStatus  string
	notice             string
	modalTitle         string
	modalHeading       string
	modalLines         []string
	modalChoices       []consoleChoice
	modalSelected      int
	modalActive        bool
	slashSelected      int
	stopping           bool
	err                error
}

func (a App) runTUI(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	reader, input := io.Pipe()
	output := make(chan tuiOutput, 128)
	done := make(chan error, 1)
	go func() {
		instance := a
		instance.Reader = &pipeReader{Reader: reader}
		instance.Writer = io.Discard
		instance.events = func(event consoleEvent) {
			select {
			case output <- tuiOutput{kind: event.kind, text: event.text, promptMode: event.promptMode, choices: event.choices, defaultChoice: event.defaultChoice, working: event.working, activeWorkers: event.activeWorkers}:
			case <-ctx.Done():
			}
		}
		done <- instance.runPlain(ctx)
		close(output)
	}()
	model := newGuidedTUI(ctx, cancel, output, done, input)
	final, err := tea.NewProgram(model, tea.WithInput(a.Reader), tea.WithOutput(a.Writer), tea.WithAltScreen(), tea.WithoutSignalHandler()).Run()
	_ = input.Close()
	if err != nil {
		cancel()
		return fmt.Errorf("orchestrator terminal UI: %w", err)
	}
	if result, ok := final.(guidedTUI); ok && result.err != nil {
		return result.err
	}
	return nil
}

// pipeReader keeps the input pipe close operation private to the TUI while
// exposing an ordinary io.Reader to the guided console.
type pipeReader struct{ io.Reader }

func newGuidedTUI(ctx context.Context, cancel context.CancelFunc, output <-chan tuiOutput, done <-chan error, inputPipe io.WriteCloser) guidedTUI {
	input := textinput.New()
	input.Prompt = "birdhackbot> "
	input.Placeholder = "Talk to the orchestrator"
	input.Focus()
	input.CharLimit = 0
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	return guidedTUI{ctx: ctx, cancel: cancel, input: input, inputPipe: inputPipe, spinner: spin, conversation: viewport.New(100, 24), output: output, done: done, width: 120, height: 32, busy: true, spinnerRunning: true, coordinatorStatus: "ready"}
}

func (m guidedTUI) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick, m.waitOutput(), func() tea.Msg { return tuiDone{err: <-m.done} })
}

func (m guidedTUI) waitOutput() tea.Cmd {
	return func() tea.Msg {
		value, ok := <-m.output
		if !ok {
			return nil
		}
		return value
	}
}

func (m guidedTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		if value.Width > 0 {
			m.width = value.Width
		}
		if value.Height > 0 {
			m.height = value.Height
		}
		m.syncLayout()
	case tuiOutput:
		switch value.kind {
		case consoleModalStart:
			m.modalActive = true
			m.modalTitle = value.text
			m.modalHeading = ""
			m.modalLines = nil
			m.modalChoices = nil
			m.input.SetValue("")
		case consoleModalEnd:
			m.modalActive = false
			m.modalTitle = ""
			m.modalHeading = ""
			m.modalLines = nil
			m.modalChoices = nil
			m.waiting = false
			m.approvalDetails = ""
			if m.active {
				m.inputMode = promptChat
			}
			m.input.SetValue("")
		case consoleChoices:
			m.modalHeading = value.text
			m.modalChoices = append([]consoleChoice(nil), value.choices...)
			m.modalSelected = value.defaultChoice
			if m.modalSelected < 0 || m.modalSelected >= len(m.modalChoices) {
				m.modalSelected = 0
			}
		case consolePrompt:
			m.waiting = true
			m.inputMode = value.promptMode
			m.approvalDetails = ""
			m.busy = false
			m.coordinatorWorking = false
			m.slashSelected = 0
			prompt := strings.TrimSpace(value.text)
			m.inputContext = prompt
			if detail, question, found := strings.Cut(prompt, "\n"); found {
				m.inputContext = strings.TrimSpace(detail)
				last := strings.LastIndex(question, "\n")
				if last >= 0 {
					detail += "\n" + question[:last]
					question = question[last+1:]
				}
				if m.modalActive {
					m.modalLines = append(m.modalLines, strings.TrimSpace(detail))
				} else if m.inputMode == promptApproval {
					m.approvalDetails = strings.TrimSpace(detail)
				} else {
					m.lines = append(m.lines, strings.TrimSpace(detail))
				}
				prompt = strings.TrimSpace(question)
			}
			m.inputPrompt = prompt
			m.input.Prompt = "> "
			m.input.Placeholder = ""
		case consolePermissions:
			m.permissionLabel = value.text
		case consoleDashboard:
			m.workerStatus = value.text
			m.activeWorkers = value.activeWorkers
		case consoleCoordinatorStatus:
			m.coordinatorStatus = value.text
			m.coordinatorWorking = value.working
		case consoleNotice:
			m.notice = value.text
		case consoleAssessmentStarted:
			m.active = true
			m.inputMode = promptChat
			m.approvalDetails = ""
			m.sessionReady = false
			m.workerStatus = ""
			m.waiting = false
			m.busy = false
			m.inputPrompt = "Talk to the orchestrator"
			m.inputContext = "Coordinator"
			m.input.Prompt = "> "
			m.input.Placeholder = m.inputPrompt
			m.coordinatorStatus = "working"
			m.coordinatorWorking = true
			m.activeWorkers = 0
			m.slashSelected = 0
		case consoleAssessmentFinished:
			m.sessionReady = true
			m.inputMode = promptChat
			m.approvalDetails = ""
			m.busy = false
			m.waiting = false
			m.input.Placeholder = "Ask about results or continue the session"
			m.coordinatorStatus = "ready to discuss results"
			m.coordinatorWorking = false
			m.activeWorkers = 0
			m.slashSelected = 0
		case consoleOutput:
			if text := strings.TrimRight(value.text, "\n"); text != "" {
				if m.modalActive {
					m.modalLines = append(m.modalLines, text)
				} else {
					m.lines = append(m.lines, text)
				}
			}
			if len(m.lines) > 300 {
				m.lines = m.lines[len(m.lines)-300:]
			}
		}
		if m.active {
			m.busy = false
		}
		m.syncLayout()
		return m, tea.Batch(m.waitOutput(), m.startSpinner())
	case tuiDone:
		m.busy = false
		m.coordinatorWorking = false
		m.activeWorkers = 0
		m.err = value.err
		if m.stopping && (errors.Is(value.err, context.Canceled) || errors.Is(value.err, context.DeadlineExceeded)) {
			m.err = nil
		}
		if value.err != nil {
			m.lines = append(m.lines, "Application: "+value.err.Error())
			m.syncLayout()
		}
		return m, tea.Quit
	case spinner.TickMsg:
		if !m.needsSpinner() {
			m.spinnerRunning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(value)
		return m, cmd
	case tea.KeyMsg:
		switch value.String() {
		case "ctrl+c":
			if m.stopping {
				return m, nil
			}
			m.stopping = true
			m.busy = true
			m.waiting = false
			m.inputPrompt = "Stopping workers..."
			m.inputContext = "Stopping"
			m.input.Prompt = "> "
			m.input.Placeholder = m.inputPrompt
			m.cancel()
			return m, m.startSpinner()
		case "esc":
			if m.modalActive && m.waiting {
				m.waiting = false
				m.busy = true
				m.modalChoices = nil
				m.input.SetValue("")
				return m, tea.Batch(sendTUILine(m.ctx, m.inputPipe, modalCancelInput), m.startSpinner())
			}
			if len(m.slashMatches()) > 0 {
				m.input.SetValue("")
				m.slashSelected = 0
				m.syncLayout()
				return m, nil
			}
		case "up", "shift+tab", "k":
			if m.modalActive && len(m.modalChoices) > 0 && m.waiting {
				m.modalSelected = (m.modalSelected + len(m.modalChoices) - 1) % len(m.modalChoices)
				return m, nil
			}
			if value.String() == "up" {
				if matches := m.slashMatches(); len(matches) > 0 {
					m.slashSelected = (m.slashSelected + len(matches) - 1) % len(matches)
					return m, nil
				}
			}
		case "down", "tab", "j":
			if m.modalActive && len(m.modalChoices) > 0 && m.waiting {
				m.modalSelected = (m.modalSelected + 1) % len(m.modalChoices)
				return m, nil
			}
			if matches := m.slashMatches(); len(matches) > 0 {
				if value.String() == "down" {
					m.slashSelected = (m.slashSelected + 1) % len(matches)
					return m, nil
				}
				if value.String() == "tab" {
					m.input.SetValue(matches[m.slashSelected].name)
					m.slashSelected = 0
					m.syncLayout()
					return m, nil
				}
			}
		case "enter":
			if m.modalActive {
				if !m.waiting {
					return m, nil
				}
				line := strings.TrimSpace(m.input.Value())
				if len(m.modalChoices) > 0 {
					line = m.modalChoices[m.modalSelected].Value
				}
				m.input.SetValue("")
				m.waiting = false
				m.busy = true
				m.modalChoices = nil
				return m, tea.Batch(sendTUILine(m.ctx, m.inputPipe, line), m.startSpinner())
			}
			line := strings.TrimSpace(m.input.Value())
			if matches := m.slashMatches(); len(matches) > 0 {
				line = matches[m.slashSelected].name
			}
			if m.busy || (!m.waiting && !m.active) || (line == "" && !m.waiting) {
				return m, nil
			}
			m.input.SetValue("")
			m.slashSelected = 0
			m.waiting = false
			m.approvalDetails = ""
			m.busy = !m.active
			if m.active && m.inputMode == promptChat && !strings.HasPrefix(line, "/") {
				m.coordinatorWorking = true
				m.coordinatorStatus = "responding"
			}
			if m.inputMode == promptChat && !strings.HasPrefix(line, "/") && !strings.EqualFold(line, "settings") && !strings.EqualFold(line, "resume") {
				m.lines = append(m.lines, "You: "+line)
			}
			if m.active {
				m.inputMode = promptChat
			}
			// The guided console owns the conversation semantics. The TUI only
			// transports the exact line to that runtime.
			m.input.Prompt = "> "
			m.input.Placeholder = "Talk to the orchestrator"
			m.syncLayout()
			return m, tea.Batch(sendTUILine(m.ctx, m.inputPipe, line), m.startSpinner())
		case "pgup":
			if !m.modalActive {
				m.conversation.PageUp()
				return m, nil
			}
		case "pgdown":
			if !m.modalActive {
				m.conversation.PageDown()
				return m, nil
			}
		}
	}
	if !m.busy {
		var cmd tea.Cmd
		oldValue := m.input.Value()
		oldPaletteHeight := len(m.slashPaletteLines())
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != oldValue {
			m.slashSelected = 0
		}
		if oldPaletteHeight != len(m.slashPaletteLines()) {
			m.syncLayout()
		}
		return m, cmd
	}
	return m, nil
}

func (m guidedTUI) needsSpinner() bool {
	return m.busy || m.coordinatorWorking || m.activeWorkers > 0
}

func (m *guidedTUI) startSpinner() tea.Cmd {
	if !m.needsSpinner() || m.spinnerRunning {
		return nil
	}
	m.spinnerRunning = true
	return m.spinner.Tick
}

func sendTUILine(ctx context.Context, input io.Writer, text string) tea.Cmd {
	return func() tea.Msg {
		if _, err := io.WriteString(input, text+"\n"); err != nil {
			return tuiOutput{kind: consoleOutput, text: "Input error: " + err.Error()}
		}
		return nil
	}
}

func (m *guidedTUI) syncLayout() {
	width := m.width
	if width < 80 {
		width = 80
	}
	height := m.height
	if height < 20 {
		height = 20
	}
	m.width, m.height = width, height
	left := width * 2 / 3
	m.conversation.Width = left - 4
	approvalRows := 0
	if m.waiting && m.inputMode == promptApproval {
		approvalRows = len(m.approvalLines()) + 1
	}
	m.conversation.Height = maxTUI(4, height-10-len(m.slashPaletteLines())-approvalRows)
	follow := m.conversation.AtBottom()
	content := renderTUIConversation(m.lines, m.conversation.Width)
	m.conversation.SetContent(content)
	if follow {
		m.conversation.GotoBottom()
	}
	m.input.Width = width - 8
	if m.modalActive {
		m.input.Width = maxTUI(20, min(62, width-12)-2)
	}
}

func (m guidedTUI) View() string {
	conversationTitle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	inputTitleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	pane := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	left := pane.Width(m.conversation.Width + 2).Height(m.conversation.Height + 2).Render(conversationTitle.Render(" Conversation ") + "\n" + m.conversation.View())
	rightWidth := maxTUI(24, m.width-m.conversation.Width-9)
	right := pane.Width(rightWidth).Height(m.conversation.Height + 2).Render(tuiSectionTitle(" Activity ") + "\n" + m.statusBody(rightWidth-4, m.conversation.Height))
	inputTitle := " Input "
	if m.waiting && m.inputMode == promptApproval {
		inputTitle = " Approval needed "
	} else if m.waiting && m.inputPrompt != "" {
		question := m.inputPrompt
		if m.inputContext != "" && m.inputContext != question {
			question = m.inputContext + " · " + question
		}
		inputTitle = " " + compactSessionText(question, m.width-12) + " "
	}
	if m.busy && !m.stopping {
		inputTitle = " Input " + m.spinner.View() + " setting up "
	} else if m.stopping {
		inputTitle = " Input " + m.spinner.View() + " stopping "
	}
	bottomLines := []string{inputTitleStyle.Render(inputTitle)}
	if m.waiting && m.inputMode == promptApproval {
		for _, line := range m.approvalLines() {
			bottomLines = append(bottomLines, tuiBody.Render(line))
		}
		bottomLines = append(bottomLines, tuiBlue.Render(m.inputPrompt))
	}
	bottomLines = append(bottomLines, m.input.View())
	bottomLines = append(bottomLines, m.slashPaletteLines()...)
	bottomLines = append(bottomLines, lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("Enter send  ·  / commands  ·  PgUp/PgDn history  ·  Ctrl-C stop"))
	bottom := pane.Width(maxTUI(30, m.width-2)).Render(strings.Join(bottomLines, "\n"))
	if m.modalActive {
		bottom = pane.Width(maxTUI(30, m.width-2)).Render(inputTitleStyle.Render(" Dialog open ") + "\nUse the dialog above. Esc closes it.\nCtrl-C stops all workers")
	}
	base := lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right), bottom)
	if m.modalActive {
		return m.viewModal(base)
	}
	return base
}

func maxTUI(a, b int) int {
	if a > b {
		return a
	}
	return b
}
