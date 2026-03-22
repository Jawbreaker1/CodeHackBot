package interactivecli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
)

type stubRunner struct {
	outcomes []workerloop.Outcome
	errs     []error
	calls    []ctxpacket.WorkerPacket
}

func (s *stubRunner) Run(_ context.Context, packet ctxpacket.WorkerPacket, _ int) (workerloop.Outcome, error) {
	s.calls = append(s.calls, packet)
	idx := len(s.calls) - 1
	return s.outcomes[idx], s.errs[idx]
}

type echoRunner struct {
	summary string
	calls   []ctxpacket.WorkerPacket
}

func (e *echoRunner) Run(_ context.Context, packet ctxpacket.WorkerPacket, _ int) (workerloop.Outcome, error) {
	e.calls = append(e.calls, packet)
	return workerloop.Outcome{Summary: e.summary, Packet: packet}, nil
}

func TestShellRunMaintainsConversationAcrossTurns(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &stubRunner{
		outcomes: []workerloop.Outcome{
			{Summary: "first done", Packet: ctxpacket.WorkerPacket{BehaviorFrame: behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"}, SessionFoundation: session.Foundation{Goal: "first goal", ReportingRequirement: "owasp"}, RecentConversation: []string{"User: first goal"}}},
			{Summary: "second done", Packet: ctxpacket.WorkerPacket{BehaviorFrame: behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"}, SessionFoundation: session.Foundation{Goal: "first goal", ReportingRequirement: "owasp"}, RecentConversation: []string{"User: first goal", "Assistant: first done", "User: next step"}}},
		},
		errs: []error{nil, nil},
	}
	var out strings.Builder
	shell := Shell{
		Reader:    strings.NewReader("first goal\nnext step\nexit\n"),
		Writer:    &out,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("runner calls = %d", len(runner.calls))
	}
	if !strings.Contains(out.String(), "first done") || !strings.Contains(out.String(), "second done") {
		t.Fatalf("output missing summaries: %q", out.String())
	}
	if got := runner.calls[1].RecentConversation; len(got) == 0 || got[len(got)-1] != "User: next step" {
		t.Fatalf("second call conversation = %#v", got)
	}
	if got := runner.calls[1].CurrentStep.Objective; got != "first goal" {
		t.Fatalf("second call objective = %q, want %q", got, "first goal")
	}
	if got := runner.calls[1].PlanState.ActiveStep; got != "first goal" {
		t.Fatalf("second call active step = %q, want %q", got, "first goal")
	}
}

func TestShellRunRollsOlderConversationAfterRecentCap(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &echoRunner{summary: "ok"}
	var in strings.Builder
	in.WriteString("first goal\n")
	for i := 0; i < 12; i++ {
		in.WriteString("follow up\n")
	}
	in.WriteString("exit\n")

	shell := Shell{
		Reader:    strings.NewReader(in.String()),
		Writer:    io.Discard,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	stateBytes, err := os.ReadFile(root + "/session.json")
	if err != nil {
		t.Fatalf("ReadFile(session.json) error = %v", err)
	}
	text := string(stateBytes)
	if !strings.Contains(text, "\"OlderConversationSummary\":") {
		t.Fatalf("session state missing OlderConversationSummary: %s", text)
	}
	if !strings.Contains(text, "User: first goal") {
		t.Fatalf("session state missing rolled older conversation: %s", text)
	}
	last := runner.calls[len(runner.calls)-1]
	if len(last.RecentConversation) > 20 {
		t.Fatalf("recent conversation len = %d, want <= 20", len(last.RecentConversation))
	}
}

func TestShellRunFailsWhenInitialStateSaveFails(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &echoRunner{summary: "ok"}
	shell := Shell{
		Reader:    strings.NewReader("first goal\n"),
		Writer:    io.Discard,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		SaveState: func(path string, state sessionstate.State) error {
			return errors.New("disk full")
		},
	}
	err := shell.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "save session state") {
		t.Fatalf("Run() error = %v, want save session state failure", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestShellRunFailsWhenStoppedStateSaveFails(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &stubRunner{
		outcomes: []workerloop.Outcome{{Packet: ctxpacket.WorkerPacket{SessionFoundation: session.Foundation{Goal: "first goal", ReportingRequirement: "owasp"}}}},
		errs:     []error{errors.New("worker failed")},
	}
	saveCalls := 0
	shell := Shell{
		Reader:    strings.NewReader("first goal\n"),
		Writer:    io.Discard,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		SaveState: func(path string, state sessionstate.State) error {
			saveCalls++
			if saveCalls == 1 {
				return nil
			}
			return errors.New("write failure")
		},
	}
	err := shell.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "save session state after run error") {
		t.Fatalf("Run() error = %v, want stopped save failure", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
}

func TestShellReturnsContextCancellationAfterSavingStoppedState(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	var saves []sessionstate.State
	runner := &stubRunner{
		outcomes: []workerloop.Outcome{{}},
		errs:     []error{context.Canceled},
	}
	shell := Shell{
		Reader:   strings.NewReader("first goal\n"),
		Writer:   io.Discard,
		RepoRoot: root,
		Runner:   runner,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		SaveState: func(path string, state sessionstate.State) error {
			saves = append(saves, state)
			return nil
		},
	}
	err := shell.Run(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if len(saves) != 2 {
		t.Fatalf("save calls = %d, want 2", len(saves))
	}
	if saves[1].Status != "stopped" {
		t.Fatalf("second save status = %q, want %q", saves[1].Status, "stopped")
	}
	if saves[1].LastError != "aborted by signal" {
		t.Fatalf("second save last_error = %q, want %q", saves[1].LastError, "aborted by signal")
	}
}

func TestHandleShellCommandStatsWithoutSession(t *testing.T) {
	var out strings.Builder
	handled, err := handleShellCommand(&out, "/stats", false, ctxpacket.WorkerPacket{})
	if err != nil {
		t.Fatalf("handleShellCommand() error = %v", err)
	}
	if !handled {
		t.Fatalf("handleShellCommand() handled = false, want true")
	}
	if got := strings.TrimSpace(out.String()); got != "no active session" {
		t.Fatalf("output = %q, want %q", got, "no active session")
	}
}

func TestHandleShellCommandPacketAndLastLog(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame: behavior.Frame{
			SystemPrompt: "prompt",
			AgentsText:   "rules",
			RuntimeMode:  "worker",
		},
		SessionFoundation: session.Foundation{
			Goal:                 "inspect target",
			ReportingRequirement: "owasp",
		},
		OperatorState: ctxpacket.OperatorState{
			PendingLog: "/tmp/pending.log",
			WorkingDir: "/tmp/work",
		},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			LogRefs: []string{"/tmp/fallback.log"},
		},
		RunningSummary: "summary",
	}

	var packetOut strings.Builder
	handled, err := handleShellCommand(&packetOut, "/packet", true, packet)
	if err != nil {
		t.Fatalf("handleShellCommand(/packet) error = %v", err)
	}
	if !handled {
		t.Fatalf("handleShellCommand(/packet) handled = false, want true")
	}
	rendered := packetOut.String()
	if !strings.Contains(rendered, "[behavior_frame]") || !strings.Contains(rendered, "goal: inspect target") {
		t.Fatalf("/packet output missing expected content: %q", rendered)
	}

	var logOut strings.Builder
	handled, err = handleShellCommand(&logOut, "/lastlog", true, packet)
	if err != nil {
		t.Fatalf("handleShellCommand(/lastlog) error = %v", err)
	}
	if !handled {
		t.Fatalf("handleShellCommand(/lastlog) handled = false, want true")
	}
	if got := strings.TrimSpace(logOut.String()); got != "/tmp/pending.log" {
		t.Fatalf("/lastlog output = %q, want %q", got, "/tmp/pending.log")
	}
}

func TestRenderPacketStatsIncludesTotals(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame: behavior.Frame{
			SystemPrompt: "prompt",
			AgentsText:   "rules",
			RuntimeMode:  "worker",
		},
		SessionFoundation: session.Foundation{
			Goal:                 "inspect target",
			ReportingRequirement: "owasp",
		},
		RunningSummary: "summary",
	}

	stats := renderPacketStats(packet)
	if !strings.Contains(stats, "context stats:") {
		t.Fatalf("stats missing header: %q", stats)
	}
	if !strings.Contains(stats, "- behavior_frame:") {
		t.Fatalf("stats missing section line: %q", stats)
	}
	if !strings.Contains(stats, "total: chars=") {
		t.Fatalf("stats missing total line: %q", stats)
	}
}

func TestLatestLogPathFallsBackToLatestExecutionResult(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		LatestExecutionResult: ctxpacket.ExecutionResult{
			LogRefs: []string{"/tmp/fallback.log"},
		},
	}
	if got := latestLogPath(packet); got != "/tmp/fallback.log" {
		t.Fatalf("latestLogPath() = %q, want %q", got, "/tmp/fallback.log")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
