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
	kind consoleEventKind
	text string
}
type tuiDone struct{ err error }

type guidedTUI struct {
	ctx           context.Context
	cancel        context.CancelFunc
	input         textinput.Model
	inputPipe     io.WriteCloser
	spinner       spinner.Model
	conversation  viewport.Model
	output        <-chan tuiOutput
	done          <-chan error
	lines         []string
	width, height int
	busy          bool
	waiting       bool
	active        bool
	inputPrompt   string
	stopping      bool
	err           error
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
			case output <- tuiOutput{kind: event.kind, text: event.text}:
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
	return guidedTUI{ctx: ctx, cancel: cancel, input: input, inputPipe: inputPipe, spinner: spin, conversation: viewport.New(100, 24), output: output, done: done, width: 120, height: 32, busy: true}
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
		case consolePrompt:
			m.waiting = true
			m.busy = false
			m.inputPrompt = strings.TrimSpace(value.text)
			m.input.Prompt = ""
			m.input.Placeholder = m.inputPrompt
		case consoleAssessmentStarted:
			m.active = true
			m.waiting = false
			m.busy = false
			m.inputPrompt = "Talk to the orchestrator"
			m.input.Prompt = ""
			m.input.Placeholder = m.inputPrompt
		case consoleOutput:
			if text := strings.TrimRight(value.text, "\n"); text != "" {
				m.lines = append(m.lines, text)
			}
			if len(m.lines) > 300 {
				m.lines = m.lines[len(m.lines)-300:]
			}
		}
		if m.active {
			m.busy = false
		}
		m.syncLayout()
		return m, m.waitOutput()
	case tuiDone:
		m.busy = false
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
		if !m.busy {
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
			m.input.Prompt = ""
			m.input.Placeholder = m.inputPrompt
			m.cancel()
			return m, nil
		case "enter":
			line := strings.TrimSpace(m.input.Value())
			if line == "" || m.busy || (!m.waiting && !m.active) {
				return m, nil
			}
			m.input.SetValue("")
			m.waiting = false
			m.busy = !m.active
			m.lines = append(m.lines, "You: "+line)
			// The guided console owns the conversation semantics. The TUI only
			// transports the exact line to that runtime.
			m.input.Prompt = ""
			m.input.Placeholder = "Talk to the orchestrator"
			return m, sendTUILine(m.ctx, m.inputPipe, line)
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
	m.conversation.Height = height - 8
	content := strings.Join(m.lines, "\n")
	if m.conversation.Width > 0 {
		content = lipgloss.NewStyle().Width(m.conversation.Width).Render(content)
	}
	m.conversation.SetContent(content)
	m.conversation.GotoBottom()
	m.input.Width = width - 8
}

func (m guidedTUI) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	pane := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	left := pane.Width(m.conversation.Width + 2).Height(m.conversation.Height + 2).Render(title.Render(" Conversation and activity ") + "\n" + m.conversation.View())
	rightBody := strings.Join([]string{
		"phase: live orchestrator",
		"workers: delegated by coordinator",
		"approvals: required per action",
		"",
		"The coordinator owns intent, planning, and scope questions.",
		"Worker progress appears in the conversation pane.",
		"",
		"/workers  /status  /help  /stop",
	}, "\n")
	right := pane.Width(maxTUI(24, m.width-m.conversation.Width-9)).Height(m.conversation.Height + 2).Render(title.Render(" Assessment status ") + "\n" + rightBody)
	inputTitle := " Input "
	if m.busy {
		inputTitle = " Input " + m.spinner.View() + " thinking "
	}
	bottom := pane.Width(maxTUI(30, m.width-2)).Render(title.Render(inputTitle) + "\n" + m.input.View() + "\nEnter sends · Ctrl-C stops all workers")
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right), bottom)
}

func maxTUI(a, b int) int {
	if a > b {
		return a
	}
	return b
}
