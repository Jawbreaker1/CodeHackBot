package guided

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }
