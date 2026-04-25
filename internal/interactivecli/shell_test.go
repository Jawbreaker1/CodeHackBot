package interactivecli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
	"github.com/Jawbreaker1/CodeHackBot/internal/workertask"
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

type progressRunner struct {
	outcome  workerloop.Outcome
	err      error
	progress workerloop.ProgressSink
}

func (p *progressRunner) SetProgressSink(sink workerloop.ProgressSink) {
	p.progress = sink
}

func (p *progressRunner) Run(_ context.Context, packet ctxpacket.WorkerPacket, _ int) (workerloop.Outcome, error) {
	packet.TaskRuntime.State = "running"
	packet.PlanState.ActiveStep = "Inspect archive"
	packet.CurrentStep.Objective = "Inspect archive"
	packet.RunningSummary = "Inspecting archive metadata."
	_ = workerloopEmitTestProgress(p.progress, workerloop.ProgressEvent{
		Kind:       workerloop.EventPlanFinished,
		Message:    "Archive inspection plan established.",
		ActiveStep: packet.PlanState.ActiveStep,
	}, packet)

	packet.LatestExecutionResult.Action = "zipinfo -v secret.zip"
	_ = workerloopEmitTestProgress(p.progress, workerloop.ProgressEvent{
		Kind:       workerloop.EventExecutionStarted,
		Message:    "zipinfo -v secret.zip",
		Action:     "zipinfo -v secret.zip",
		ActiveStep: packet.PlanState.ActiveStep,
	}, packet)

	packet.TaskRuntime.State = "done"
	packet.LatestExecutionResult.ExitStatus = "0"
	packet.LatestExecutionResult.OutputSummary = "stdout: Archive: secret.zip"
	_ = workerloopEmitTestProgress(p.progress, workerloop.ProgressEvent{
		Kind:       workerloop.EventExecutionFinished,
		Message:    "execution finished",
		Action:     packet.LatestExecutionResult.Action,
		ExitStatus: packet.LatestExecutionResult.ExitStatus,
		ActiveStep: packet.PlanState.ActiveStep,
	}, packet)

	if p.outcome.Packet.SessionFoundation.Goal == "" {
		p.outcome.Packet = packet
	}
	return p.outcome, p.err
}

func workerloopEmitTestProgress(sink workerloop.ProgressSink, event workerloop.ProgressEvent, packet ctxpacket.WorkerPacket) error {
	if sink == nil {
		return nil
	}
	return sink.EmitProgress(event, packet)
}

type echoRunner struct {
	summary string
	calls   []ctxpacket.WorkerPacket
}

func (e *echoRunner) Run(_ context.Context, packet ctxpacket.WorkerPacket, _ int) (workerloop.Outcome, error) {
	e.calls = append(e.calls, packet)
	return workerloop.Outcome{Summary: e.summary, Packet: packet}, nil
}

type preservingRunner struct {
	summary string
	calls   []ctxpacket.WorkerPacket
}

func (p *preservingRunner) Run(_ context.Context, packet ctxpacket.WorkerPacket, _ int) (workerloop.Outcome, error) {
	p.calls = append(p.calls, packet)
	packet.TaskRuntime.State = "running"
	if strings.TrimSpace(packet.PlanState.ActiveStep) == "" {
		packet.PlanState.ActiveStep = packet.SessionFoundation.Goal
	}
	if strings.TrimSpace(packet.CurrentStep.Objective) == "" {
		packet.CurrentStep.Objective = packet.SessionFoundation.Goal
	}
	return workerloop.Outcome{Summary: p.summary, Packet: packet}, nil
}

func TestShellRunStartsNewTaskAfterCompletedTurn(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &stubRunner{
		outcomes: []workerloop.Outcome{
			{Summary: "first done", Packet: ctxpacket.WorkerPacket{
				BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
				SessionFoundation: session.Foundation{Goal: "first goal", ReportingRequirement: "owasp"},
				TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
				LatestExecutionResult: ctxpacket.ExecutionResult{
					Action:        "pwd",
					OutputSummary: "stdout: /tmp/work",
					LogRefs:       []string{"/tmp/cmd.log"},
				},
				RecentConversation: []string{"User: first goal"},
			}},
			{Summary: "second done", Packet: ctxpacket.WorkerPacket{
				BehaviorFrame:      behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
				SessionFoundation:  session.Foundation{Goal: "next step", ReportingRequirement: "owasp"},
				TaskRuntime:        ctxpacket.TaskRuntime{State: "done"},
				RecentConversation: []string{"User: next step"},
			}},
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
	for _, needle := range []string{"BirdHackBot interactive session.", "User: first goal", "User: next step", "Assistant: Done."} {
		if !strings.Contains(out.String(), needle) {
			t.Fatalf("output missing %q: %q", needle, out.String())
		}
	}
	if got := runner.calls[1].SessionFoundation.Goal; got != "next step" {
		t.Fatalf("second call goal = %q, want %q", got, "next step")
	}
	if got := runner.calls[1].CurrentStep.Objective; got != "next step" {
		t.Fatalf("second call objective = %q, want %q", got, "next step")
	}
	if got := runner.calls[1].PlanState.ActiveStep; got != "next step" {
		t.Fatalf("second call active step = %q, want %q", got, "next step")
	}
	if !strings.Contains(runner.calls[1].OlderConversationSummary, "User: first goal") {
		t.Fatalf("second call older summary missing prior conversation: %q", runner.calls[1].OlderConversationSummary)
	}
	if len(runner.calls[1].RelevantRecentResults) == 0 || runner.calls[1].RelevantRecentResults[0].Action != "pwd" {
		t.Fatalf("second call relevant results = %#v", runner.calls[1].RelevantRecentResults)
	}
}

func TestPrepareTaskPacketContinuesActiveTaskWhenBoundarySaysContinue(t *testing.T) {
	shell := Shell{
		Model:     "test-model",
		MaxSteps:  3,
		AllowAll:  true,
		StatePath: filepath.Join(t.TempDir(), "session.json"),
		Boundary: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string) (workertask.Decision, error) {
			return workertask.Decision{Action: workertask.ActionContinueActiveTask, Reason: "latest turn refines current task"}, nil
		},
	}
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "extract zip"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "running"},
		PlanState:         ctxpacket.PlanState{ActiveStep: "Recover password"},
		RecentConversation: []string{
			"User: extract zip",
			"Assistant: plan created",
		},
	}
	next, startedNew, err := shell.prepareTaskPacket(context.Background(), behavior.Frame{}, packet, true, "try a dictionary attack first")
	if err != nil {
		t.Fatalf("prepareTaskPacket() error = %v", err)
	}
	if startedNew {
		t.Fatal("expected active task continuation")
	}
	if next.SessionFoundation.Goal != "extract zip" {
		t.Fatalf("goal = %q, want %q", next.SessionFoundation.Goal, "extract zip")
	}
	if got := next.RecentConversation[len(next.RecentConversation)-1]; got != "User: try a dictionary attack first" {
		t.Fatalf("recent conversation tail = %q", got)
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
	if !strings.Contains(saves[1].Summary, "Done.") || !strings.Contains(saves[1].Summary, "If you want, I can continue with a new target.") {
		t.Fatalf("final save summary = %q", saves[1].Summary)
	}
}

func TestShellRunPersistsProgressSnapshots(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	var saves []sessionstate.State
	runner := &progressRunner{
		outcome: workerloop.Outcome{
			Summary: "archive inspected",
			Packet: ctxpacket.WorkerPacket{
				SessionFoundation: session.Foundation{Goal: "inspect secret.zip", ReportingRequirement: "owasp"},
				TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
			},
		},
	}
	shell := Shell{
		Reader:    strings.NewReader("inspect secret.zip\nexit\n"),
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
	if len(saves) < 4 {
		t.Fatalf("save calls = %d, want at least 4", len(saves))
	}
	if got := saves[1].Packet.PlanState.ActiveStep; got != "Inspect archive" {
		t.Fatalf("progress save active step = %q", got)
	}
	if got := saves[len(saves)-1].Status; got != "completed" {
		t.Fatalf("final save status = %q, want completed", got)
	}
}

func TestShellRunHeadlessWritesEventAndTranscriptArtifacts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &progressRunner{
		outcome: workerloop.Outcome{
			Summary: "archive inspected",
			Packet: ctxpacket.WorkerPacket{
				SessionFoundation: session.Foundation{Goal: "inspect secret.zip", ReportingRequirement: "owasp"},
				TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
			},
		},
	}
	shell := Shell{
		Reader:    strings.NewReader("inspect secret.zip\nexit\n"),
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
	if err := shell.RunHeadless(context.Background()); err != nil {
		t.Fatalf("RunHeadless() error = %v", err)
	}

	eventsPath := filepath.Join(root, "events.ndjson")
	transcriptPath := filepath.Join(root, "transcript.ndjson")
	events, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("ReadFile(events.ndjson) error = %v", err)
	}
	transcript, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("ReadFile(transcript.ndjson) error = %v", err)
	}
	for _, want := range []string{
		`"kind":"classification.accepted"`,
		`"kind":"task.started"`,
		`"kind":"progress.execution_started"`,
		`"kind":"run.completed"`,
	} {
		if !strings.Contains(string(events), want) {
			t.Fatalf("events missing %q in:\n%s", want, string(events))
		}
	}
	for _, want := range []string{
		`"role":"user"`,
		`"content":"inspect secret.zip"`,
		`"role":"assistant"`,
		`"content":"Done.`,
	} {
		if !strings.Contains(string(transcript), want) {
			t.Fatalf("transcript missing %q in:\n%s", want, string(transcript))
		}
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
	for _, needle := range []string{"Command: /plan", "System: no active plan", "Assistant: Run updated.\nWhat I did:\n- ok"} {
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

func TestBuildConversationPromptKeepsSessionContextLightweight(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "inspect secret.zip"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "running"},
		RecentConversation: []string{
			"User: hello",
			"Assistant: hi",
		},
		OlderConversationSummary: "User asked about secret.zip",
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:         "zipinfo secret.zip",
			OutputSummary:  "stdout: treasure-note.txt",
			OutputEvidence: "stdout: treasure-note.txt\nstdout: another line",
		},
		OperatorState: ctxpacket.OperatorState{
			WorkingDir: "/tmp/testrepo",
		},
	}

	prompt := buildConversationPrompt(packet, "Who are you?", true)

	for _, want := range []string{
		`"session_context"`,
		`"active_goal": "inspect secret.zip"`,
		`"task_state": "running"`,
		`"recent_conversation"`,
		`"working_dir": "/tmp/testrepo"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q in:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{
		`"current_worker_state"`,
		`"latest_execution_result"`,
		`zipinfo secret.zip`,
		`treasure-note.txt`,
	} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("prompt unexpectedly contained %q in:\n%s", unwanted, prompt)
		}
	}
}

func TestBuildModeClassifierPromptKeepsSessionContextLightweight(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "inspect secret.zip"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
		PlanState:         ctxpacket.PlanState{ActiveStep: "verify zip presence"},
		RecentConversation: []string{
			"User: list zip files",
			"Assistant: found secret.zip",
		},
		OlderConversationSummary: "Earlier the user asked about the archive.",
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:         "zipinfo secret.zip",
			OutputSummary:  "stdout: treasure-note.txt",
			OutputEvidence: "stdout: treasure-note.txt\nstdout: another line",
		},
		OperatorState: ctxpacket.OperatorState{
			WorkingDir: "/tmp/testrepo",
		},
	}

	prompt := buildModeClassifierPrompt(packet, "Extract the zip file", true)

	for _, want := range []string{
		`"session_context"`,
		`"active_goal": "inspect secret.zip"`,
		`"task_state": "done"`,
		`"active_step": "verify zip presence"`,
		`"working_dir": "/tmp/testrepo"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q in:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{
		`"current_worker_state"`,
		`"latest_execution_result"`,
		`zipinfo secret.zip`,
		`treasure-note.txt`,
	} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("prompt unexpectedly contained %q in:\n%s", unwanted, prompt)
		}
	}
}

func TestApplyTaskStartPreservesSharedCapabilityInputs(t *testing.T) {
	root := t.TempDir()
	shell := Shell{
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
	}
	ui := NewUIState()
	packet := ctxpacket.NewInitialWorkerPacket(
		behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
		session.Foundation{Goal: "inspect secret.zip", ReportingRequirement: "owasp"},
		"/tmp/testrepo",
		"test-model",
		"approved_session",
		2,
	)

	if err := shell.applyTaskStart(&ui, packet, string(workerplan.ModePlannedExecution)); err != nil {
		t.Fatalf("applyTaskStart() error = %v", err)
	}
	if len(ui.Packet.CapabilityInputs) == 0 {
		t.Fatalf("CapabilityInputs should not be empty for worker mode")
	}
	for _, want := range []string{
		"operating_environment: standard Kali Linux environment",
		"Metasploit Framework",
		"tooling_preference:",
	} {
		found := false
		for _, item := range ui.Packet.CapabilityInputs {
			if strings.Contains(item, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("CapabilityInputs missing %q: %#v", want, ui.Packet.CapabilityInputs)
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
	for _, want := range []string{"System: input classification failed; fell back to conversation mode", "error: bad classifier response", "Assistant: Can you clarify what you want me to inspect?"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q in %q", want, out.String())
		}
	}
	attemptPath := filepath.Join(root, "context", "classification-attempt-001.txt")
	data, err := os.ReadFile(attemptPath)
	if err != nil {
		t.Fatalf("ReadFile(classification attempt) error = %v", err)
	}
	text := string(data)
	for _, want := range []string{"[classification_attempt]", "accepted: false", "final_error: classify input: bad classifier response"} {
		if !strings.Contains(text, want) {
			t.Fatalf("classification attempt missing %q in:\n%s", want, text)
		}
	}
}

func TestClassifyInputAcceptsVerboseReasonByNormalizingIt(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	shell := Shell{
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
		Classify: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string, started bool) (workermode.Decision, error) {
			return workermode.Decision{
				Mode:   workerplan.ModePlannedExecution,
				Reason: strings.Repeat("too many words ", 20),
			}, nil
		},
	}
	frame, err := behavior.Load(root, "worker", map[string]string{
		"approval_mode": approvalModeLabel(true),
		"surface":       "interactive_worker_cli",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	decision, err := shell.classifyInput(context.Background(), frame, ctxpacket.WorkerPacket{}, "Extract the zip file", false)
	if err != nil {
		t.Fatalf("classifyInput() error = %v", err)
	}
	if decision.Mode != workerplan.ModePlannedExecution {
		t.Fatalf("mode = %q", decision.Mode)
	}
	if decision.Reason != "task requires multiple execution phases" {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestShellRunRollsOlderConversationAfterRecentCap(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &preservingRunner{summary: "ok"}
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
		Boundary: func(ctx context.Context, frame behavior.Frame, packet ctxpacket.WorkerPacket, line string) (workertask.Decision, error) {
			return workertask.Decision{Action: workertask.ActionContinueActiveTask, Reason: "test continuation"}, nil
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
		"What I did:\n- executed \"zipinfo -v secret.zip\" (exit 0)",
		"Result:\n  stdout: There is no zipfile comment.",
		"Next progress:\n- inspect the latest log or ask me to try a different method",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q in %q", want, got)
		}
	}
}

func TestBuildTerminalChatSummaryUsesConciseResultForCompletedVerboseOutput(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "Can you list the files in the current folder please"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:         "ls -la",
			ExitStatus:     "0",
			OutputSummary:  "stdout: total 10\ndrwxr-xr-x .",
			OutputEvidence: "stdout: total 10\ndrwxr-xr-x .\n-rw-r--r-- AGENTS.md",
			Assessment:     "success",
			LogRefs:        []string{"/tmp/cmd.log"},
		},
	}
	got := buildTerminalChatSummary(packet, "listed files", nil)
	for _, want := range []string{
		"Done.",
		"listed files",
		"Preview:\n  total 10\n  drwxr-xr-x .\n  -rw-r--r-- AGENTS.md",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "stdout: total 10") {
		t.Fatalf("summary should not dump raw verbose output: %q", got)
	}
}

func TestHandleShellCommandFullOutputReadsLatestLogStdout(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")
	logPath := filepath.Join(root, "cmd.log")
	mustWrite(t, logPath, strings.Join([]string{
		"action: ls -la",
		"actual_invocation: /bin/sh -lc \"ls -la\"",
		"",
		"[stdout]",
		"total 10",
		"AGENTS.md",
		"TASKS.md",
		"",
		"[stderr]",
		"",
	}, "\n"))

	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "list files"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:  "ls -la",
			LogRefs: []string{logPath},
		},
		OperatorState: ctxpacket.OperatorState{
			PendingLog: logPath,
		},
	}
	var out strings.Builder
	handled, err := handleShellCommand(&out, "/fulloutput", true, packet)
	if err != nil {
		t.Fatalf("handleShellCommand(/fulloutput) error = %v", err)
	}
	if !handled {
		t.Fatal("expected handled = true")
	}
	reply := out.String()
	for _, want := range []string{
		"Done.",
		"Here is the full output from `ls -la`.",
		"stdout:",
		"AGENTS.md",
		"TASKS.md",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("reply missing %q in:\n%s", want, reply)
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
