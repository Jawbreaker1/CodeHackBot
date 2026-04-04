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
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
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
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "test execution turn"}, nil
		},
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("runner calls = %d", len(runner.calls))
	}
	for _, needle := range []string{"Stream", "Status", "Input", "Assistant: Run updated. What I did: first done", "Assistant: Run updated. What I did: second done"} {
		if !strings.Contains(out.String(), needle) {
			t.Fatalf("output missing %q: %q", needle, out.String())
		}
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

func TestShellRunPersistsCompletedStatusAndSummary(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	var saves []sessionstate.State
	runner := &stubRunner{
		outcomes: []workerloop.Outcome{{
			Summary: "listed files",
			Packet: ctxpacket.WorkerPacket{
				SessionFoundation: session.Foundation{Goal: "what files are here?", ReportingRequirement: "owasp"},
				TaskRuntime:       ctxpacket.TaskRuntime{State: "done", MissingFact: "(none)"},
				LatestExecutionResult: ctxpacket.ExecutionResult{
					Action:        "ls -la",
					ExitStatus:    "0",
					OutputSummary: "stdout: birdhackbot\nREADME.md",
				},
			},
		}},
		errs: []error{nil},
	}
	shell := Shell{
		Reader:    strings.NewReader("what files are here?\nexit\n"),
		Writer:    io.Discard,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "test execution turn"}, nil
		},
		SaveState: func(path string, state sessionstate.State) error {
			saves = append(saves, state)
			return nil
		},
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(saves) != 2 {
		t.Fatalf("save calls = %d, want 2", len(saves))
	}
	if saves[1].Status != "completed" {
		t.Fatalf("final save status = %q, want %q", saves[1].Status, "completed")
	}
	if !strings.Contains(saves[1].Summary, "What I did: listed files") || !strings.Contains(saves[1].Summary, "Next progress: ask me to produce or refine the final report") {
		t.Fatalf("final save summary = %q", saves[1].Summary)
	}
}

func TestShellRunShowsShellCommandOutputInStream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &echoRunner{summary: "ok"}
	var out strings.Builder
	shell := Shell{
		Reader:    strings.NewReader("first goal\n/plan\nexit\n"),
		Writer:    &out,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "simple execution request"}, nil
		},
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	rendered := out.String()
	for _, needle := range []string{"Command: /plan", "System: no active plan", "understand goal"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("output missing %q: %q", needle, rendered)
		}
	}
}

func TestShellRunConversationModeBypassesRunner(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &stubRunner{}
	var out strings.Builder
	shell := Shell{
		Reader:    strings.NewReader("Who are you?\nexit\n"),
		Writer:    &out,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeConversation, Reason: "identity question only"}, nil
		},
		Chat: func(ctx context.Context, messages []llmclient.Message) (string, error) {
			return "I am BirdHackBot.", nil
		},
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
	for _, want := range []string{"User: Who are you?", "Assistant: I am BirdHackBot."} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q in %q", want, out.String())
		}
	}
}

func TestShellRunDirectExecutionSetsModeHint(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &stubRunner{
		outcomes: []workerloop.Outcome{{Summary: "listed", Packet: ctxpacket.WorkerPacket{SessionFoundation: session.Foundation{Goal: "list files", ReportingRequirement: "owasp"}}}},
		errs:     []error{nil},
	}
	shell := Shell{
		Reader:    strings.NewReader("List the files in this folder\nexit\n"),
		Writer:    io.Discard,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "simple listing request"}, nil
		},
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	if got := runner.calls[0].OperatorState.ModeHint; got != string(workerplan.ModeDirectExecution) {
		t.Fatalf("mode hint = %q, want %q", got, workerplan.ModeDirectExecution)
	}
}

func TestShellRunPlannedExecutionSetsModeHint(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &stubRunner{
		outcomes: []workerloop.Outcome{{Summary: "planned", Packet: ctxpacket.WorkerPacket{SessionFoundation: session.Foundation{Goal: "extract zip", ReportingRequirement: "owasp"}}}},
		errs:     []error{nil},
	}
	shell := Shell{
		Reader:    strings.NewReader("Extract secret.zip and find the password\nexit\n"),
		Writer:    io.Discard,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModePlannedExecution, Reason: "multi-phase extraction task"}, nil
		},
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	if got := runner.calls[0].OperatorState.ModeHint; got != string(workerplan.ModePlannedExecution) {
		t.Fatalf("mode hint = %q, want %q", got, workerplan.ModePlannedExecution)
	}
}

func TestShellRunFallsBackToConversationOnClassifierError(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &stubRunner{}
	var out strings.Builder
	shell := Shell{
		Reader:    strings.NewReader("Take a look at this\nexit\n"),
		Writer:    &out,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{}, errors.New("bad classifier response")
		},
		Chat: func(ctx context.Context, messages []llmclient.Message) (string, error) {
			return "Can you clarify what you want me to inspect?", nil
		},
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
	for _, want := range []string{"System: input classification failed; fell back to conversation mode", "Assistant: Can you clarify what you want me to inspect?"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q in %q", want, out.String())
		}
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
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "test execution turn"}, nil
		},
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
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "test execution turn"}, nil
		},
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
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "test execution turn"}, nil
		},
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
		Reader:    strings.NewReader("first goal\n"),
		Writer:    io.Discard,
		RepoRoot:  root,
		Runner:    runner,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{Mode: workerplan.ModeDirectExecution, Reason: "test execution turn"}, nil
		},
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

func TestBuildTerminalChatSummaryForBlockedRun(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "extract zip"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "blocked", MissingFact: "password hint"},
		PlanState:         ctxpacket.PlanState{ActiveStep: "Recover password"},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "zipinfo -v secret.zip",
			ExitStatus:    "0",
			OutputSummary: "stdout: There is no zipfile comment.",
		},
	}
	got := buildTerminalChatSummary(packet, "", errors.New("step did not complete within 8 steps"))
	for _, want := range []string{
		"Run stopped.",
		`What I did: executed "zipinfo -v secret.zip" (exit 0)`,
		"Result: stdout: There is no zipfile comment.",
		"Next progress: inspect the latest log or ask me to try a different method",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q in %q", want, got)
		}
	}
}

func TestHandleShellCommandStatsWithoutSession(t *testing.T) {
	var out strings.Builder
	for _, cmd := range []string{"/stats", "/status", "/step"} {
		out.Reset()
		handled, err := handleShellCommand(&out, cmd, false, ctxpacket.WorkerPacket{})
		if err != nil {
			t.Fatalf("handleShellCommand(%s) error = %v", cmd, err)
		}
		if !handled {
			t.Fatalf("handleShellCommand(%s) handled = false, want true", cmd)
		}
		if got := strings.TrimSpace(out.String()); got != "no active session" {
			t.Fatalf("%s output = %q, want %q", cmd, got, "no active session")
		}
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

func TestHandleShellCommandPlan(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			WorkerGoal: "inspect zip and report",
			Summary:    "Inspect, recover, report.",
			Steps: []string{
				"Inspect archive",
				"Attempt recovery",
				"Compile report",
			},
			ActiveStep: "Attempt recovery",
			ReplanConditions: []string{
				"archive missing",
				"recovery blocked",
			},
		},
	}

	var out strings.Builder
	handled, err := handleShellCommand(&out, "/plan", true, packet)
	if err != nil {
		t.Fatalf("handleShellCommand() error = %v", err)
	}
	if !handled {
		t.Fatalf("handleShellCommand() handled = false, want true")
	}
	text := out.String()
	for _, want := range []string{
		"plan:",
		"- mode: planned_execution",
		"- worker_goal: inspect zip and report",
		"* Attempt recovery",
		"- replan_conditions:",
		"  - archive missing",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plan output missing %q in:\n%s", want, text)
		}
	}
}

func TestHandleShellCommandStatusAndStep(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{
			Goal:                 "inspect target",
			ReportingRequirement: "owasp",
		},
		CurrentStep: ctxpacket.Step{
			Objective: "enumerate ports",
		},
		TaskRuntime: ctxpacket.TaskRuntime{
			State:         "running",
			CurrentTarget: "192.168.50.1",
			MissingFact:   "open services not yet confirmed",
		},
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			WorkerGoal: "scan and report",
			Steps: []string{
				"Verify reachability",
				"Enumerate ports",
				"Report findings",
			},
			ActiveStep: "Enumerate ports",
		},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "nmap --top-ports 1000 192.168.50.1",
			OutputSummary: "port scan still running",
			FailureClass:  "execution_interrupted",
		},
		RunningSummary: "Plan step still in progress while active work continues.",
		OperatorState: ctxpacket.OperatorState{
			Model:        "qwen3.5-27b",
			ContextUsage: "12k",
		},
	}

	var statusOut strings.Builder
	handled, err := handleShellCommand(&statusOut, "/status", true, packet)
	if err != nil {
		t.Fatalf("handleShellCommand(/status) error = %v", err)
	}
	if !handled {
		t.Fatalf("handleShellCommand(/status) handled = false, want true")
	}
	for _, want := range []string{
		"status:",
		"- goal: inspect target",
		"- active_step: Enumerate ports",
		"- latest_result: port scan still running",
		"- model: qwen3.5-27b",
	} {
		if !strings.Contains(statusOut.String(), want) {
			t.Fatalf("/status output missing %q in:\n%s", want, statusOut.String())
		}
	}

	var stepOut strings.Builder
	handled, err = handleShellCommand(&stepOut, "/step", true, packet)
	if err != nil {
		t.Fatalf("handleShellCommand(/step) error = %v", err)
	}
	if !handled {
		t.Fatalf("handleShellCommand(/step) handled = false, want true")
	}
	for _, want := range []string{
		"step:",
		"- objective: enumerate ports",
		"- active_step: Enumerate ports",
		"- latest_command: nmap --top-ports 1000 192.168.50.1",
		"  - [in_progress] Enumerate ports",
	} {
		if !strings.Contains(stepOut.String(), want) {
			t.Fatalf("/step output missing %q in:\n%s", want, stepOut.String())
		}
	}
}

func TestHandleShellCommandPlanWithoutSession(t *testing.T) {
	var out strings.Builder
	handled, err := handleShellCommand(&out, "/plan", false, ctxpacket.WorkerPacket{})
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

func TestHandleShellCommandHelp(t *testing.T) {
	var out strings.Builder
	handled, err := handleShellCommand(&out, "/help", false, ctxpacket.WorkerPacket{})
	if err != nil {
		t.Fatalf("handleShellCommand(/help) error = %v", err)
	}
	if !handled {
		t.Fatalf("handleShellCommand(/help) handled = false, want true")
	}
	for _, want := range []string{
		"commands:",
		"/status",
		"/step",
		"/plan",
		"/help",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("/help output missing %q in:\n%s", want, out.String())
		}
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
