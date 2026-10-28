package guided

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestGuidedTUIShowsASCIIBranding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	updated, _ = updated.Update(tuiOutput{kind: consoleOutput, text: cliLogo + "\n\n"})
	got := updated.(guidedTUI)
	view := got.View()
	if len(strings.Split(cliLogo, "\n")) != 11 {
		t.Fatal("ASCII wordmark lost one of its three word groups")
	}
	for _, line := range strings.Split(cliLogo, "\n") {
		if len(line) > got.conversation.Width {
			t.Fatalf("logo line exceeds the conversation pane: %q", line)
		}
		if strings.TrimSpace(line) != "" && !strings.Contains(view, line) {
			t.Fatalf("logo line is not visible in the TUI: %q\n%s", line, view)
		}
	}
}

func TestGuidedTUIKeepsSetupQuestionVisibleWhileTyping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, text: "Subscription model ID [gpt-daybreak-blue-latest]"})
	got := updated.(guidedTUI)
	got.input.SetValue("Hello who are you?")
	if !strings.Contains(got.View(), "Subscription model ID [gpt-daybreak-blue-latest]") {
		t.Fatal("setup question disappeared while the operator typed")
	}
}

func TestGuidedTUISettingsModalKeepsChoicesOutOfConversation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var input bytes.Buffer
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), captureWriteCloser{&input})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	updated, _ = updated.Update(tuiOutput{kind: consoleModalStart, text: "Model settings"})
	updated, _ = updated.Update(tuiOutput{kind: consoleChoices, text: "Configured models", choices: []consoleChoice{
		{Label: "Daybreak Blue", Detail: "OpenAI subscription", Value: "daybreak"},
		{Label: "Qwen 3.8", Detail: "Local model", Value: "qwen38"},
	}})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, text: "Choose a model"})
	got := updated.(guidedTUI)
	view := got.View()
	if !strings.Contains(view, "Model settings") || !strings.Contains(view, "Daybreak Blue") || !strings.Contains(view, "Qwen 3.8") {
		t.Fatal("model choices were not visible in the modal")
	}
	if len(got.lines) != 0 {
		t.Fatalf("settings leaked into the conversation: %#v", got.lines)
	}
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	if updated.(guidedTUI).modalSelected != 1 {
		t.Fatal("arrow key did not select the next model")
	}
	updated, command := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter did not submit the selected model")
	}
	_ = command()
	if input.String() != "qwen38\n" || len(updated.(guidedTUI).lines) != 0 {
		t.Fatalf("modal selection = %q, conversation = %#v", input.String(), updated.(guidedTUI).lines)
	}
}

func TestGuidedTUIModalEscapeCancelsWithoutChatOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var input bytes.Buffer
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), captureWriteCloser{&input})
	updated, _ := model.Update(tuiOutput{kind: consoleModalStart, text: "Model settings"})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, text: "Local model server address"})
	updated, command := updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command == nil {
		t.Fatal("Esc did not cancel the dialog")
	}
	_ = command()
	if input.String() != modalCancelInput+"\n" || len(updated.(guidedTUI).lines) != 0 {
		t.Fatalf("cancel signal = %q, conversation = %#v", input.String(), updated.(guidedTUI).lines)
	}
}

func TestGuidedTUIPromptStaysInInputState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := make(chan tuiOutput)
	done := make(chan error)
	model := newGuidedTUI(ctx, cancel, output, done, nopWriteCloser{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, text: "Choose model access: 1 or 2"})
	got := updated.(guidedTUI)
	if !got.waiting || got.inputPrompt != "Choose model access: 1 or 2" || !strings.Contains(got.View(), got.inputPrompt) {
		t.Fatalf("prompt state = waiting:%v question:%q", got.waiting, got.inputPrompt)
	}
	if len(got.lines) != 0 {
		t.Fatalf("prompt leaked into conversation: %#v", got.lines)
	}
	if got.conversation.Width <= 0 || got.conversation.Height <= 0 {
		t.Fatalf("layout was not initialized: %dx%d", got.conversation.Width, got.conversation.Height)
	}
}

func TestGuidedTUIAcceptsDefaultPromptChoice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tuiOutput{kind: consolePrompt, text: "Press Enter for all tasks"})
	updated, command := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || updated.(guidedTUI).waiting {
		t.Fatal("empty Enter did not submit the prompt's default choice")
	}
}

func TestGuidedTUIShowsCoordinatorWorkDuringIntake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, promptMode: promptChat, text: "birdhackbot> "})
	ready := updated.(guidedTUI)
	ready.input.SetValue("What happened with PHP?")
	updated, _ = ready.Update(tea.KeyMsg{Type: tea.KeyEnter})
	working := updated.(guidedTUI)
	view := ansi.Strip(working.View())
	if !working.busy || !working.coordinatorWorking || !strings.Contains(view, "Considering your") || !strings.Contains(view, "working") || strings.Contains(view, "setting up") {
		t.Fatalf("intake work was not clear while waiting for the model:\n%s", view)
	}
	updated, _ = working.Update(tuiOutput{kind: consoleCoordinatorStatus, text: "Search public web for PHP advisory", working: true})
	searching := updated.(guidedTUI)
	if !strings.Contains(ansi.Strip(searching.View()), "Search public") {
		t.Fatalf("source check did not replace the generic working status:\n%s", ansi.Strip(searching.View()))
	}
	updated, _ = searching.Update(tuiOutput{kind: consoleOutput, text: "Coordinator: Short answer."})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, promptMode: promptChat, text: "birdhackbot> "})
	if updated.(guidedTUI).coordinatorWorking {
		t.Fatal("finished answer still marked coordinator as working")
	}
}

func TestGuidedTUIShowsApprovalDetailsBesideInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tuiOutput{kind: consolePrompt, promptMode: promptApproval, text: "Review this action\nTarget: fixture\nImpact: read-only\nAllow this action? [y/N]"})
	got := updated.(guidedTUI)
	view := ansi.Strip(got.View())
	if got.inputPrompt != "Allow this action? [y/N]" || len(got.lines) != 0 || !strings.Contains(view, "Approval needed") || !strings.Contains(view, "Target: fixture") || !strings.Contains(view, "Impact: read-only") {
		t.Fatalf("approval details were not isolated from chat: question=%q lines=%#v\n%s", got.inputPrompt, got.lines, view)
	}
}

func TestGuidedTUIKeepsApprovalAnswersOutOfChat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var input bytes.Buffer
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), captureWriteCloser{&input})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	updated, _ = updated.Update(tuiOutput{kind: consoleAssessmentStarted})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, promptMode: promptApproval, text: "Review this action\nTarget: fixture\nAllow this action? [y/N, p=approval settings]"})
	approval := updated.(guidedTUI)
	approval.input.SetValue("/")
	approval.syncLayout()
	if matches := approval.slashMatches(); len(matches) != 1 || matches[0].name != "/permissions" {
		t.Fatalf("approval slash menu = %#v", matches)
	}
	if rows := len(strings.Split(strings.TrimRight(ansi.Strip(approval.View()), "\n"), "\n")); rows > 24 {
		t.Fatalf("approval menu overflows a 24-row terminal: %d rows", rows)
	}
	compact, _ := approval.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if rows := len(strings.Split(strings.TrimRight(ansi.Strip(compact.(guidedTUI).View()), "\n"), "\n")); rows > 20 {
		t.Fatalf("approval menu overflows a 20-row terminal: %d rows", rows)
	}
	approval.input.SetValue("y")
	updated, command := approval.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("approval answer was not submitted")
	}
	if batch, ok := command().(tea.BatchMsg); ok {
		for _, child := range batch {
			_ = child()
		}
	}
	got := updated.(guidedTUI)
	if input.String() != "y\n" || strings.Contains(strings.Join(got.lines, "\n"), "You: y") || got.coordinatorStatus == "responding" {
		t.Fatalf("approval answer entered chat or falsely marked coordinator as responding: input=%q lines=%#v status=%q", input.String(), got.lines, got.coordinatorStatus)
	}
	updated, _ = got.Update(tuiOutput{kind: consolePrompt, promptMode: promptChat, text: "birdhackbot> "})
	chat := updated.(guidedTUI)
	chat.input.SetValue("What happened with PHP?")
	updated, _ = chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(strings.Join(updated.(guidedTUI).lines, "\n"), "You: What happened with PHP?") {
		t.Fatal("ordinary chat input was hidden along with approval answers")
	}
}

func TestGuidedTUIResumesWorkingStatusAfterIntakeApproval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tuiOutput{kind: consolePrompt, promptMode: promptApproval, text: "Read source\nAllow this observation? [y/N]"})
	approval := updated.(guidedTUI)
	approval.input.SetValue("y")
	updated, _ = approval.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := ansi.Strip(updated.(guidedTUI).View())
	if !strings.Contains(view, "working") || strings.Contains(view, "setting up") {
		t.Fatalf("intake approval left an unclear waiting state:\n%s", view)
	}
}

func TestGuidedTUICtrlCWaitsForApplicationShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	model.active = true
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(guidedTUI)
	if !got.stopping || !got.busy {
		t.Fatalf("stop state = stopping:%v busy:%v", got.stopping, got.busy)
	}
}

func TestGuidedTUIShowsReadySessionAfterAssessment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tuiOutput{kind: consoleAssessmentStarted})
	updated, _ = updated.Update(tuiOutput{kind: consoleAssessmentFinished})
	ready := updated.(guidedTUI)
	if !ready.sessionReady || !ready.active || ready.busy {
		t.Fatalf("finished session cannot accept chat: %+v", ready)
	}
	updated, _ = ready.Update(tuiOutput{kind: consoleAssessmentStarted})
	if updated.(guidedTUI).sessionReady {
		t.Fatal("continued assessment still shown as finished")
	}
}

func TestGuidedTUIShowsWorkerProgressInInspector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	updated, _ = updated.Update(tuiOutput{kind: consoleAssessmentStarted})
	updated, _ = updated.Update(tuiOutput{kind: consoleDashboard, text: "inspect · running\n  executing approved action\n  context 23%"})
	got := updated.(guidedTUI)
	if !strings.Contains(got.View(), "inspect · running") || !strings.Contains(got.View(), "context 23%") {
		t.Fatal("worker status missing from right inspector")
	}
	if len(got.lines) != 0 {
		t.Fatalf("worker updates flooded conversation: %#v", got.lines)
	}
}

func TestGuidedTUIStatusFitsNarrowTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	updated, _ = updated.Update(tuiOutput{kind: consoleAssessmentStarted})
	updated, _ = updated.Update(tuiOutput{kind: consolePermissions, text: "Ask me before every execution"})
	updated, _ = updated.Update(tuiOutput{kind: consoleDashboard, text: "inspect · running\n  reviewing the target response\n  input 23%\nvalidate · running\n  comparing evidence\n  input 31%", activeWorkers: 2})
	got := updated.(guidedTUI)
	view := ansi.Strip(got.View())
	if rows := len(strings.Split(strings.TrimRight(view, "\n"), "\n")); rows > 24 {
		t.Fatalf("active status overflows a 24-row terminal: %d rows\n%s", rows, view)
	}
	for _, want := range []string{"RUNNING", "COORDINATOR", "Ask me before", "WORKERS · 2 ACTIVE", "inspect · running", "validate · running"} {
		if !strings.Contains(view, want) {
			t.Fatalf("status missing %q:\n%s", want, view)
		}
	}
	updated, _ = got.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	short := ansi.Strip(updated.(guidedTUI).View())
	if !strings.Contains(short, "inspect · running") || !strings.Contains(short, "validate · running") {
		t.Fatalf("compact terminal hid an active worker:\n%s", short)
	}
}

func TestGuidedTUIAnimatesActiveCoordinatorAndWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tuiOutput{kind: consoleAssessmentStarted})
	updated, _ = updated.Update(tuiOutput{kind: consoleCoordinatorStatus, text: "planning next steps", working: true})
	before := updated.(guidedTUI)
	updated, next := updated.Update(before.spinner.Tick())
	after := updated.(guidedTUI)
	if next == nil || before.spinner.View() == after.spinner.View() || after.coordinatorStatus != "planning next steps" {
		t.Fatal("coordinator progress indicator did not animate during the assessment")
	}
	updated, _ = updated.Update(tuiOutput{kind: consoleCoordinatorStatus, text: "plan ready"})
	updated, next = updated.Update(updated.(guidedTUI).spinner.Tick())
	if next != nil || updated.(guidedTUI).spinnerRunning {
		t.Fatal("idle coordinator kept the animation timer running")
	}
	updated, next = updated.Update(tuiOutput{kind: consoleDashboard, text: "inspect · running", activeWorkers: 1})
	if next == nil || !updated.(guidedTUI).spinnerRunning {
		t.Fatal("running worker did not restart the progress indicator")
	}
	updated, next = updated.Update(updated.(guidedTUI).spinner.Tick())
	if next == nil || !strings.Contains(updated.(guidedTUI).View(), "inspect · running") {
		t.Fatal("worker progress indicator did not animate")
	}
	updated, _ = updated.Update(tuiOutput{kind: consoleDashboard, text: "inspect · done"})
	updated, next = updated.Update(updated.(guidedTUI).spinner.Tick())
	if next != nil || updated.(guidedTUI).spinnerRunning {
		t.Fatal("finished worker kept the animation timer running")
	}
}

func TestGuidedTUIFormatsConversationAndPreservesScroll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, text: "Ask the coordinator"})
	updated, _ = updated.Update(tuiOutput{kind: consoleOutput, text: "Coordinator: **Important**: use `nmap` carefully.\n\n- First step\n- Second step"})
	view := ansi.Strip(updated.(guidedTUI).conversation.View())
	if !strings.Contains(view, "COORDINATOR") || !strings.Contains(view, "Important: use nmap carefully.") || strings.Contains(view, "**Important**") || strings.Contains(view, "`nmap`") {
		t.Fatalf("conversation markup was not rendered clearly: %q", view)
	}
	for i := 0; i < 20; i++ {
		updated, _ = updated.Update(tuiOutput{kind: consoleOutput, text: "Coordinator: Progress update with enough words to occupy more than one line in the conversation pane."})
	}
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	scrolled := updated.(guidedTUI)
	if scrolled.conversation.AtBottom() {
		t.Fatal("PageUp did not scroll the conversation")
	}
	offset := scrolled.conversation.YOffset
	updated, _ = updated.Update(tuiOutput{kind: consoleOutput, text: "Coordinator: New activity while reading earlier output."})
	if updated.(guidedTUI).conversation.YOffset != offset {
		t.Fatal("new activity pulled the reader away from earlier output")
	}
}

func TestGuidedTUISlashCommandsFilterAndRunFromInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var input bytes.Buffer
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), captureWriteCloser{&input})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	updated, _ = updated.Update(tuiOutput{kind: consolePrompt, promptMode: promptChat, text: "birdhackbot> "})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	all := updated.(guidedTUI)
	if len(all.slashMatches()) != 5 || !strings.Contains(all.View(), "Slash commands") || !strings.Contains(all.View(), "/resume") {
		t.Fatal("typing / did not show the intake command menu")
	}
	for _, command := range all.slashMatches() {
		if !strings.Contains(all.View(), command.name) {
			t.Fatalf("slash menu omitted %s", command.name)
		}
	}
	if rows := len(strings.Split(strings.TrimRight(ansi.Strip(all.View()), "\n"), "\n")); rows > 24 {
		t.Fatalf("slash menu overflows a 24-row terminal: %d rows\n%s", rows, ansi.Strip(all.View()))
	}
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("per")})
	filtered := updated.(guidedTUI)
	if len(filtered.slashMatches()) != 1 || filtered.slashMatches()[0].name != "/permissions" {
		t.Fatalf("slash filtering = %#v", filtered.slashMatches())
	}
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	if updated.(guidedTUI).input.Value() != "/permissions" {
		t.Fatal("Tab did not complete the selected command")
	}
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter did not submit the selected command")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, child := range batch {
			_ = child()
		}
	}
	if input.String() != "/permissions\n" || len(updated.(guidedTUI).slashMatches()) != 0 {
		t.Fatalf("submitted %q; palette remained visible", input.String())
	}
	updated, _ = updated.Update(tuiOutput{kind: consoleAssessmentStarted})
	running := updated.(guidedTUI)
	running.input.SetValue("/")
	if !hasSlashCommand(running.slashMatches(), "/stop") || hasSlashCommand(running.slashMatches(), "/exit") {
		t.Fatal("running assessment offered the wrong slash commands")
	}
	updated, _ = running.Update(tuiOutput{kind: consoleAssessmentFinished})
	finished := updated.(guidedTUI)
	finished.input.SetValue("/")
	if !hasSlashCommand(finished.slashMatches(), "/exit") || hasSlashCommand(finished.slashMatches(), "/stop") {
		t.Fatal("finished assessment offered the wrong slash commands")
	}
}

func hasSlashCommand(commands []slashCommand, name string) bool {
	for _, command := range commands {
		if command.name == name {
			return true
		}
	}
	return false
}

func TestConsoleEmitsActualWorkerActivityForTUI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var events []consoleEvent
	console := newConsole(ctx, strings.NewReader(""), nil, func(event consoleEvent) {
		events = append(events, event)
	})
	console.Progress(assessment.Event{Kind: "planning"})
	if first := events[0]; first.kind != consoleCoordinatorStatus || !first.working {
		t.Fatalf("planning event did not mark coordinator active: %+v", first)
	}
	console.Progress(assessment.Event{TaskID: "inspect", Kind: "task_started"})
	if last := events[len(events)-1]; last.kind != consoleDashboard || last.activeWorkers != 1 {
		t.Fatalf("worker start did not mark worker active: %+v", last)
	}
	console.Progress(assessment.Event{TaskID: "inspect", Kind: "task_completed"})
	if last := events[len(events)-1]; last.kind != consoleDashboard || last.activeWorkers != 0 {
		t.Fatalf("worker completion did not clear activity: %+v", last)
	}
}

func TestConsoleShowsIntakeResearchAsCoordinatorActivity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var events []consoleEvent
	console := newConsole(ctx, strings.NewReader(""), nil, func(event consoleEvent) {
		events = append(events, event)
	})
	console.Progress(assessment.Event{TaskID: "coordinator", Kind: "execution_started", Message: "Search public web for PHP advisory"})
	console.Progress(assessment.Event{TaskID: "coordinator", Kind: "execution_finished", Message: "Coordinator observation recorded"})
	if len(events) != 2 || events[0].kind != consoleCoordinatorStatus || events[0].text != "Search public web for PHP advisory" || !events[0].working || events[1].kind != consoleCoordinatorStatus || events[1].text != "Reading the result" {
		t.Fatalf("intake progress did not stay concise and coordinator-owned: %+v", events)
	}
	if len(console.DashboardSnapshot()) != 1 {
		t.Fatal("coordinator observation was incorrectly added to worker dashboard")
	}
}

type nopWriteCloser struct{}

type captureWriteCloser struct{ *bytes.Buffer }

func (captureWriteCloser) Close() error { return nil }

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }
