package guided

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestGuidedTUIShowsASCIIBranding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	updated, _ = updated.Update(tuiOutput{kind: consoleOutput, text: cliLogo + "\n\n"})
	view := updated.(guidedTUI).View()
	if !strings.Contains(view, "BIRDHACKBOT.") || !strings.Contains(view, "/  o   \\") {
		t.Fatal("terminal UI did not render the ASCII raven")
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
	if !got.waiting || got.input.Placeholder != "Choose model access: 1 or 2" {
		t.Fatalf("prompt state = waiting:%v placeholder:%q", got.waiting, got.input.Placeholder)
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

func TestGuidedTUIShowsApprovalDetailsInConversation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newGuidedTUI(ctx, cancel, make(chan tuiOutput), make(chan error), nopWriteCloser{})
	updated, _ := model.Update(tuiOutput{kind: consolePrompt, text: "Review this action\nTarget: fixture\nImpact: read-only\nAllow this action? [y/N]"})
	got := updated.(guidedTUI)
	if got.input.Placeholder != "Allow this action? [y/N]" || len(got.lines) != 1 || got.lines[0] != "Review this action\nTarget: fixture\nImpact: read-only" {
		t.Fatalf("approval prompt was not readable: placeholder=%q details=%#v", got.input.Placeholder, got.lines)
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

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }
