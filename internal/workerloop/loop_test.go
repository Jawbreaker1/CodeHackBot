package workerloop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextinspect"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestLoopStepComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"type":"step_complete","summary":"done"}`}},
			},
		})
	}))
	defer server.Close()

	loop := Loop{
		LLM:      llmclient.Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()},
		Executor: execx.Executor{LogDir: t.TempDir()},
		Approver: approval.StaticApprover{Decision: approval.DecisionApproveSession},
	}
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "test", ReportingRequirement: "owasp"},
	}
	outcome, err := loop.Run(context.Background(), packet, 1)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if outcome.Summary != "done" {
		t.Fatalf("summary = %q", outcome.Summary)
	}
	if !strings.Contains(outcome.Packet.RunningSummary, "Status: done.") {
		t.Fatalf("running summary = %q", outcome.Packet.RunningSummary)
	}
	if outcome.Packet.RunningSummary == "done" {
		t.Fatalf("running summary should not be raw model completion text")
	}
}

func TestBuildUserPromptIncludesCompletionGuidance(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame: behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{
			Goal:                 "inspect localhost",
			ReportingRequirement: "owasp",
		},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "nmap -sV -p- --open 127.0.0.1",
			ExitStatus:    "0",
			OutputSummary: "stdout: 22/tcp open ssh OpenSSH 10.2p1 Debian 3",
			Assessment:    "success",
		},
	}

	prompt := buildUserPrompt(packet)
	for _, want := range []string{
		"Use task_runtime.current_target as the concrete thing currently being worked.",
		"Use task_runtime.missing_fact as the primary description of what still needs to be learned or verified.",
		"If task_runtime.missing_fact is not '(none)', prefer an action that establishes that missing fact for the current target.",
		"Before choosing action, check whether the current goal is already satisfied by the latest execution result or relevant recent results.",
		"If the goal is already satisfied with evidence in the context packet, choose step_complete.",
		"Do not spend another turn re-reading or slicing the same log, command output, or artifact when the needed evidence is already present in the context packet.",
		"If the latest result clearly failed, first reconsider whether the failed command structure itself was necessary before repeating or elaborating it.",
		"After a clear failure, prefer a simpler next action that removes the failure cause or gathers the missing fact directly.",
		"Do not preserve self-invented scaffolding such as custom output files or new directories unless they are actually needed for the goal.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "[behavior_frame]") {
		t.Fatalf("prompt unexpectedly contains behavior_frame packet section: %q", prompt)
	}
	if !strings.Contains(prompt, "\"context_packet\":") {
		t.Fatalf("prompt missing context_packet payload: %q", prompt)
	}
}

func TestLoopWritesInspectionSnapshots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"type":"step_complete","summary":"done"}`}},
			},
		})
	}))
	defer server.Close()

	inspectDir := t.TempDir()
	loop := Loop{
		LLM:       llmclient.Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()},
		Executor:  execx.Executor{LogDir: t.TempDir()},
		Approver:  approval.StaticApprover{Decision: approval.DecisionApproveSession},
		Inspector: contextinspect.Recorder{Dir: inspectDir},
	}
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "test", ReportingRequirement: "owasp"},
	}
	if _, err := loop.Run(context.Background(), packet, 1); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(inspectDir, "step-001-pre-llm.txt"),
		filepath.Join(inspectDir, "step-001-step-complete.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected snapshot %s: %v", path, err)
		}
	}
}

func TestLoopApprovalDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"type":"action","command":"pwd","use_shell":false}`}},
			},
		})
	}))
	defer server.Close()

	loop := Loop{
		LLM:      llmclient.Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()},
		Executor: execx.Executor{LogDir: t.TempDir()},
		Approver: approval.StaticApprover{Decision: approval.DecisionDeny},
	}
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:      behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation:  session.Foundation{Goal: "test", ReportingRequirement: "owasp"},
		RunningSummary:     "start",
		RecentConversation: []string{"user: test"},
	}
	_, err := loop.Run(context.Background(), packet, 1)
	if err == nil || !strings.Contains(err.Error(), "execution denied by user") {
		t.Fatalf("Run() error = %v, want denial", err)
	}
}

func TestUpdateRelevantRecentResults(t *testing.T) {
	current := []ctxpacket.ExecutionResult{
		{Action: "second", ExitStatus: "0"},
		{Action: "third", ExitStatus: "1"},
		{Action: "fourth", ExitStatus: "0"},
	}
	got := updateRelevantRecentResults(current, ctxpacket.ExecutionResult{Action: "first", ExitStatus: "0"}, "")
	if len(got) != 3 {
		t.Fatalf("len(got) = %d", len(got))
	}
	if got[0].Action != "third" || got[1].Action != "first" || got[2].Action != "second" {
		t.Fatalf("unexpected ordering: %#v", got)
	}
}

func TestUpdateRelevantRecentResultsPrefersTargetRelevantEvidence(t *testing.T) {
	current := []ctxpacket.ExecutionResult{
		{
			Action:        `find . -name "*.txt"`,
			ExitStatus:    "0",
			OutputSummary: "(none)",
			Assessment:    "ambiguous",
			Signals:       []string{"empty_output"},
		},
		{
			Action:        "unzip -t secret.zip",
			ExitStatus:    "0",
			OutputSummary: "unable to get password",
			Assessment:    "suspicious",
			Signals:       []string{"incorrect_password"},
		},
	}
	got := updateRelevantRecentResults(current, ctxpacket.ExecutionResult{
		Action:        "env | grep -i pass",
		ExitStatus:    "0",
		OutputSummary: "(none)",
		Assessment:    "success",
	}, "secret.zip")
	if len(got) < 2 {
		t.Fatalf("len(got) = %d", len(got))
	}
	if got[0].Action != "unzip -t secret.zip" {
		t.Fatalf("got[0].Action = %q", got[0].Action)
	}
}

func TestBuildRunningSummary(t *testing.T) {
	summary := buildRunningSummary(
		"inspect archive",
		ctxpacket.ExecutionResult{
			Action:        "file ./secret.zip",
			ExitStatus:    "0",
			Assessment:    "suspicious",
			Signals:       []string{"error_text", "incorrect_password"},
			OutputSummary: "stdout: ./secret.zip: ASCII text",
		},
		[]ctxpacket.ExecutionResult{
			{Action: "ls -la ./secret.zip", ExitStatus: "0"},
		},
	)
	for _, want := range []string{
		"Status: needs interpretation.",
		`Evidence: "file ./secret.zip" exited with 0.`,
		"Assessment: suspicious.",
		"Signals: error_text, incorrect_password.",
		"Key output: stdout: ./secret.zip: ASCII text.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in %q", want, summary)
		}
	}
	for _, unwanted := range []string{
		"Objective:",
		"Recent prior results retained:",
		"Current status:",
	} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("summary unexpectedly contains %q in %q", unwanted, summary)
		}
	}
}

func TestBuildRunningSummaryPrefersStrongerRetainedEvidence(t *testing.T) {
	summary := buildRunningSummary(
		"extract archive",
		ctxpacket.ExecutionResult{
			Action:        "find /home/johan -name \"secret.zip\"",
			ExitStatus:    "0",
			OutputSummary: "stdout: /home/johan/.../secret.zip",
			Assessment:    "success",
		},
		[]ctxpacket.ExecutionResult{
			{
				Action:        "unzip -t secret.zip",
				ExitStatus:    "0",
				OutputSummary: "stdout: unable to get password",
				Assessment:    "suspicious",
				Signals:       []string{"incorrect_password"},
			},
		},
	)
	for _, want := range []string{
		"Status: needs interpretation.",
		`Evidence: "unzip -t secret.zip" exited with 0.`,
		"Assessment: suspicious.",
		"Signals: incorrect_password.",
		"Key output: stdout: unable to get password.",
		"Latest result was weaker than retained evidence.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in %q", want, summary)
		}
	}
}

func TestBuildRunningSummaryCompactsLongFields(t *testing.T) {
	longAction := "printf " + strings.Repeat("a", 200)
	longOutput := "stdout: " + strings.Repeat("b", 400)
	summary := buildRunningSummary(
		"inspect archive",
		ctxpacket.ExecutionResult{
			Action:        longAction,
			ExitStatus:    "0",
			Assessment:    "success",
			OutputSummary: longOutput,
		},
		nil,
	)
	if len(summary) >= len(longAction)+len(longOutput) {
		t.Fatalf("summary was not compacted: len(summary)=%d", len(summary))
	}
	if !strings.Contains(summary, "...") {
		t.Fatalf("summary missing compacted marker: %q", summary)
	}
}

func TestBuildCompletionSummary(t *testing.T) {
	summary := buildCompletionSummary(
		"Perform local reconnaissance of 127.0.0.1",
		ctxpacket.ExecutionResult{
			Action:        "nmap -sV 127.0.0.1",
			ExitStatus:    "0",
			Assessment:    "success",
			OutputSummary: "stdout: 22/tcp open ssh OpenSSH 10.2p1 Debian 3",
		},
		nil,
	)
	for _, want := range []string{
		"Status: done.",
		"Goal: Perform local reconnaissance of 127.0.0.1.",
		`Completion evidence: "nmap -sV 127.0.0.1" exited with 0.`,
		"Assessment: success.",
		"Key output: stdout: 22/tcp open ssh OpenSSH 10.2p1 Debian 3.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("completion summary missing %q in %q", want, summary)
		}
	}
}

func TestPrepareActionSplitsDirectCommandAndChecksExecutability(t *testing.T) {
	action, validationFailure := prepareAction(Response{Type: "action", Command: "printf hello", UseShell: false})
	if validationFailure != nil {
		t.Fatalf("prepareAction() validation failure = %#v", validationFailure)
	}
	if action.Command != "printf" {
		t.Fatalf("action.Command = %q", action.Command)
	}
	if len(action.Args) != 1 || action.Args[0] != "hello" {
		t.Fatalf("action.Args = %#v", action.Args)
	}
}
