package interactivecli

import (
	"context"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
	tea "github.com/charmbracelet/bubbletea"
)

func runBubbleCmd[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	if cmd == nil {
		var zero T
		t.Fatalf("expected command")
		return zero
	}
	msg := cmd()
	if typed, ok := msg.(T); ok {
		return typed
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if typed, ok := c().(T); ok {
				return typed
			}
		}
	}
	var zero T
	t.Fatalf("command did not produce expected message type %T; got %T", zero, msg)
	return zero
}

func runBubbleBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		out := make([]tea.Msg, 0, len(batch))
		for _, c := range batch {
			if c == nil {
				continue
			}
			if next := c(); next != nil {
				out = append(out, next)
			}
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func TestBubbleModelConversationFlow(t *testing.T) {
	shell := &Shell{
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: t.TempDir() + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeConversation, Reason: "plain chat"}, nil
		},
		Chat: func(ctx context.Context, messages []llmclient.Message) (string, error) {
			return "I am BirdHackBot.", nil
		},
	}
	m := newBubbleModel(context.Background(), shell, behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"})
	m.input.SetValue("Who are you?")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := next.(bubbleModel)
	if !model.busy {
		t.Fatalf("busy = false, want true")
	}
	if got := strings.Join(model.ui.Stream, "\n"); !strings.Contains(got, "User: Who are you?") {
		t.Fatalf("stream missing user input: %q", got)
	}
	if cmd == nil {
		t.Fatal("expected classify cmd")
	}
	msg := runBubbleCmd[classifiedMsg](t, cmd)
	next, cmd = model.Update(msg)
	model = next.(bubbleModel)
	if cmd == nil {
		t.Fatal("expected conversation cmd")
	}
	next, _ = model.Update(runBubbleCmd[conversationMsg](t, cmd))
	model = next.(bubbleModel)
	if got := strings.Join(model.ui.Stream, "\n"); !strings.Contains(got, "Assistant: I am BirdHackBot.") {
		t.Fatalf("stream missing assistant reply: %q", got)
	}
}

func TestBubbleModelDirectExecutionMarksRunningBeforeRun(t *testing.T) {
	runner := &stubRunner{outcomes: []workerloop.Outcome{{Summary: "listed files", Packet: ctxpacket.WorkerPacket{SessionFoundation: session.Foundation{Goal: "what files are in this folder?", ReportingRequirement: "owasp"}, TaskRuntime: ctxpacket.TaskRuntime{State: "done", MissingFact: "(none)"}}}}, errs: []error{nil}}
	shell := &Shell{
		Runner:    runner,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: t.TempDir() + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "simple request"}, nil
		},
	}
	m := newBubbleModel(context.Background(), shell, behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"})
	m.input.SetValue("what files are in this folder?")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := next.(bubbleModel)
	classified := runBubbleCmd[classifiedMsg](t, cmd)
	next, cmd = model.Update(classified)
	model = next.(bubbleModel)
	if !model.ui.Started {
		t.Fatal("ui.Started = false, want true")
	}
	if got := model.ui.Packet.OperatorState.ModeHint; got != string(workerplan.ModeDirectExecution) {
		t.Fatalf("mode hint = %q", got)
	}
	if got := shellSessionStatus(model.ui.Started, model.ui.Packet); got != "running" {
		t.Fatalf("session status = %q, want running", got)
	}
	if cmd == nil {
		t.Fatal("expected worker run cmd")
	}
}

func TestBubbleModelShowsBusyLabelDuringClassification(t *testing.T) {
	shell := &Shell{
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: t.TempDir() + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeConversation, Reason: "plain chat"}, nil
		},
		Chat: func(ctx context.Context, messages []llmclient.Message) (string, error) {
			return "ok", nil
		},
	}
	m := newBubbleModel(context.Background(), shell, behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"})
	m.input.SetValue("Hello")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := next.(bubbleModel)
	if !model.busy {
		t.Fatal("expected busy state")
	}
	if model.busyLabel != "classifying" {
		t.Fatalf("busyLabel = %q", model.busyLabel)
	}
	view := model.View()
	if !strings.Contains(view, "classifying") {
		t.Fatalf("view missing busy label: %q", view)
	}
}

func TestBubbleModelConsumesWorkerProgressEvents(t *testing.T) {
	runner := &progressRunner{
		outcome: workerloop.Outcome{
			Summary: "archive inspected",
			Packet: ctxpacket.WorkerPacket{
				SessionFoundation: session.Foundation{Goal: "inspect secret.zip", ReportingRequirement: "owasp"},
				TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
			},
		},
	}
	shell := &Shell{
		Runner:    runner,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: t.TempDir() + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "simple request"}, nil
		},
	}
	m := newBubbleModel(context.Background(), shell, behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"})
	m.input.SetValue("inspect secret.zip")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := next.(bubbleModel)
	classified := runBubbleCmd[classifiedMsg](t, cmd)
	next, cmd = model.Update(classified)
	model = next.(bubbleModel)

	msgs := runBubbleBatch(cmd)
	var sawProgress bool
	var sawFinish bool
	for _, msg := range msgs {
		switch typed := msg.(type) {
		case runnerProgressMsg:
			sawProgress = true
			next, cmd = model.Update(typed)
			model = next.(bubbleModel)
			if cmd != nil {
				if follow, ok := cmd().(runnerProgressMsg); ok {
					next, _ = model.Update(follow)
					model = next.(bubbleModel)
				}
			}
		case runFinishedMsg:
			sawFinish = true
			next, _ = model.Update(typed)
			model = next.(bubbleModel)
		}
	}

	if !sawProgress {
		t.Fatal("expected progress message")
	}
	if !sawFinish {
		t.Fatal("expected run finished message")
	}
	stream := strings.Join(model.ui.Stream, "\n")
	if !strings.Contains(stream, "System: Archive inspection plan established.") {
		t.Fatalf("stream missing plan progress: %q", stream)
	}
	if !strings.Contains(stream, "Command: zipinfo -v secret.zip") {
		t.Fatalf("stream missing execution command: %q", stream)
	}
	if got := model.ui.Packet.PlanState.ActiveStep; got != "Inspect archive" {
		t.Fatalf("active step = %q", got)
	}
}

func TestRenderStreamContentWrapsAssistantBlocksConsistently(t *testing.T) {
	rendered := renderStreamContent([]string{
		"Assistant: Hello! I'm BirdHackBot, your security testing assistant for authorized lab environments.",
	}, 40)
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output, got %q", rendered)
	}
	if !strings.Contains(lines[0], "Assistant:") {
		t.Fatalf("first line missing assistant label: %q", lines[0])
	}
	if strings.Contains(lines[1], "Assistant:") {
		t.Fatalf("continuation line should not repeat label: %q", lines[1])
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Fatalf("continuation line not indented under assistant body: %q", lines[1])
	}
}
