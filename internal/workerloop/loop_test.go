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
		"If operator_state.mode_hint is direct_execution, prefer the simplest sufficient action and complete the task as soon as one supported result satisfies it.",
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
		filepath.Join(inspectDir, "step-001-pre-llm-validation.txt"),
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

func TestLoopStopsOnFatalPacketValidation(t *testing.T) {
	loop := Loop{
		LLM:      llmclient.Client{BaseURL: "http://example.invalid", Model: "test-model"},
		Executor: execx.Executor{LogDir: t.TempDir()},
		Approver: approval.StaticApprover{Decision: approval.DecisionApproveSession},
	}
	packet := ctxpacket.WorkerPacket{}
	outcome, err := loop.Run(context.Background(), packet, 1)
	if err == nil || !strings.Contains(err.Error(), "packet validation failed") {
		t.Fatalf("Run() error = %v, want packet validation failure", err)
	}
	if got := strings.TrimSpace(outcome.Packet.SessionFoundation.Goal); got != "" {
		t.Fatalf("unexpected packet mutation, goal=%q", got)
	}
}

func TestShouldUseWorkerPlanner(t *testing.T) {
	cases := []struct {
		goal string
		want bool
	}{
		{goal: "ls", want: false},
		{goal: "what files are in this folder?", want: false},
		{goal: "Extract contents of secret.zip and identify the password needed to decrypt it.", want: true},
		{goal: "Perform local reconnaissance of 127.0.0.1 and identify open ports and services.", want: true},
	}
	for _, tc := range cases {
		packet := ctxpacket.WorkerPacket{
			SessionFoundation: session.Foundation{Goal: tc.goal},
		}
		if got := shouldUseWorkerPlanner(packet); got != tc.want {
			t.Fatalf("shouldUseWorkerPlanner(%q) = %v, want %v", tc.goal, got, tc.want)
		}
	}
}

func TestLoopAppliesWorkerPlanAndAdvancesStep(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		content := `{"type":"step_complete","summary":"done"}`
		switch calls {
		case 1:
			content = `{"mode":"planned_execution","worker_goal":"extract secret.zip and identify password","plan_summary":"Inspect then recover then verify.","plan_steps":["identify archive input","attempt bounded recovery","verify extraction"],"active_step":"identify archive input","replan_conditions":["current step is genuinely blocked"]}`
		case 2:
			content = `{"type":"action","command":"pwd","use_shell":false}`
		case 3:
			content = `{"type":"step_complete","summary":"identified archive"}`
		case 4:
			content = `{"type":"action","command":"pwd","use_shell":false}`
		case 5:
			content = `{"type":"step_complete","summary":"recovered archive"}`
		case 6:
			content = `{"type":"step_complete","summary":"done"}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
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
		SessionFoundation: session.Foundation{Goal: "Extract contents of secret.zip and identify the password needed to decrypt it.", ReportingRequirement: "owasp"},
		CurrentStep:       ctxpacket.Step{RemainingBudget: "6 steps"},
	}
	outcome, err := loop.Run(context.Background(), packet, 6)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if outcome.Packet.PlanState.Mode != "planned_execution" {
		t.Fatalf("plan mode = %q", outcome.Packet.PlanState.Mode)
	}
	if outcome.Packet.PlanState.ActiveStep != "verify extraction" {
		t.Fatalf("active step = %q", outcome.Packet.PlanState.ActiveStep)
	}
	if outcome.Packet.TaskRuntime.State != "done" {
		t.Fatalf("task state = %q", outcome.Packet.TaskRuntime.State)
	}
	if outcome.Summary != "done" {
		t.Fatalf("summary = %q", outcome.Summary)
	}
}

func TestLoopWritesPlannerAttemptSnapshot(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		content := `{"type":"step_complete","summary":"done"}`
		if calls == 1 {
			content = `{"mode":"planned_execution","worker_goal":"inspect zip","plan_summary":"Inspect then recover.","plan_steps":["Inspect archive","Attempt recovery"],"active_step":"Inspect archive","replan_conditions":["blocked"]}`
		} else if calls == 2 {
			content = `{"type":"action","command":"pwd","use_shell":false}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
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
		SessionFoundation: session.Foundation{Goal: "Extract contents of secret.zip and identify the password needed to decrypt it.", ReportingRequirement: "owasp"},
		CurrentStep:       ctxpacket.Step{RemainingBudget: "4 steps"},
	}
	if _, err := loop.Run(context.Background(), packet, 4); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	path := filepath.Join(inspectDir, "planner-attempt-001.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(planner attempt) error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"[planner_attempt]",
		"accepted: true",
		"mode: planned_execution",
		"plan_steps: Inspect archive | Attempt recovery",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("planner attempt missing %q in:\n%s", want, text)
		}
	}
}

func TestLoopAutoAdvancesPlannedStepFromStepEvaluation(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		content := `{"type":"step_complete","summary":"done"}`
		switch calls {
		case 1:
			content = `{"mode":"planned_execution","worker_goal":"inspect zip","plan_summary":"Inspect then recover.","plan_steps":["Inspect archive","Attempt recovery"],"active_step":"Inspect archive","replan_conditions":["blocked"]}`
		case 2:
			content = `{"type":"action","command":"pwd","use_shell":false}`
		case 3:
			content = `{"decision":"execute","reason":"action fits the active step"}`
		case 4:
			content = `{"status":"satisfied","reason":"archive inspection evidence is sufficient","summary":"archive inspected"}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
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
		SessionFoundation: session.Foundation{Goal: "Extract contents of secret.zip and identify the password needed to decrypt it.", ReportingRequirement: "owasp"},
		CurrentStep:       ctxpacket.Step{RemainingBudget: "1 step"},
	}
	outcome, err := loop.Run(context.Background(), packet, 1)
	if err == nil || !strings.Contains(err.Error(), "step did not complete within 1 steps") {
		t.Fatalf("Run() error = %v, want step limit", err)
	}
	if outcome.Packet.PlanState.ActiveStep != "Attempt recovery" {
		t.Fatalf("active step = %q", outcome.Packet.PlanState.ActiveStep)
	}
	if outcome.Packet.CurrentStep.Objective != "Attempt recovery" {
		t.Fatalf("current objective = %q", outcome.Packet.CurrentStep.Objective)
	}
	if !strings.Contains(outcome.Packet.RunningSummary, "Next step: Attempt recovery") {
		t.Fatalf("running summary = %q", outcome.Packet.RunningSummary)
	}
}

func TestLoopWritesActionReviewAttemptAndRequestsRevision(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		content := `{"type":"step_complete","summary":"done"}`
		switch calls {
		case 1:
			content = `{"mode":"planned_execution","worker_goal":"inspect host","plan_summary":"scan then report","plan_steps":["Scan target","Report"],"active_step":"Scan target","replan_conditions":["blocked"]}`
		case 2:
			content = `{"type":"action","command":"nmap -sV -p- --open 192.168.50.1 > /tmp/out.txt 2>&1 && cat /tmp/out.txt","use_shell":true}`
		case 3:
			content = `{"decision":"revise","reason":"proposed action is unnecessarily elaborate for the active step"}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
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
		SessionFoundation: session.Foundation{Goal: "Perform local reconnaissance of 192.168.50.1 and identify open ports and services.", ReportingRequirement: "owasp"},
		CurrentStep:       ctxpacket.Step{RemainingBudget: "1 step"},
	}
	outcome, err := loop.Run(context.Background(), packet, 1)
	if err == nil || !strings.Contains(err.Error(), "step did not complete within 1 steps") {
		t.Fatalf("Run() error = %v, want step limit", err)
	}
	if got := outcome.Packet.LatestExecutionResult.FailureClass; got != "pre_execution_review" {
		t.Fatalf("failure class = %q", got)
	}
	if got := outcome.Packet.LatestExecutionResult.Action; !strings.Contains(got, "nmap -sV -p-") {
		t.Fatalf("latest action = %q", got)
	}
	path := filepath.Join(inspectDir, "action-review-attempt-001.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(action review) error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"[action_review_attempt]",
		"decision: revise",
		"reason: proposed action is unnecessarily elaborate for the active step",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("action review attempt missing %q in:\n%s", want, text)
		}
	}
}

func TestLoopBlocksPlannedStepOnBlockingFailure(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		content := `{"type":"step_complete","summary":"done"}`
		if calls == 1 {
			content = `{"mode":"planned_execution","worker_goal":"recover archive","plan_summary":"Recover archive.","plan_steps":["Attempt recovery","Verify extraction"],"active_step":"Attempt recovery","replan_conditions":["wordlist missing"]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
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
		SessionFoundation: session.Foundation{Goal: "Extract contents of secret.zip and identify the password needed to decrypt it.", ReportingRequirement: "owasp"},
		CurrentStep:       ctxpacket.Step{RemainingBudget: "4 steps"},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "fcrackzip -D -p /usr/share/wordlists/rockyou.txt secret.zip",
			ExitStatus:    "1",
			Assessment:    "failed",
			OutputSummary: "stderr: /usr/share/wordlists/rockyou.txt: No such file or directory",
			Signals:       []string{"missing_path", "nonzero_exit"},
			FailureClass:  "command_failed",
		},
	}
	outcome, err := loop.Run(context.Background(), packet, 4)
	if err == nil || !strings.Contains(err.Error(), "planned step blocked") {
		t.Fatalf("Run() error = %v, want planned step blocked", err)
	}
	if outcome.Packet.TaskRuntime.State != "blocked" {
		t.Fatalf("task state = %q, want blocked", outcome.Packet.TaskRuntime.State)
	}
	if outcome.Packet.PlanState.BlockedStep != "Attempt recovery" {
		t.Fatalf("blocked step = %q", outcome.Packet.PlanState.BlockedStep)
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

func TestBuildRunningSummaryInterruptedExecution(t *testing.T) {
	summary := buildRunningSummary(
		"scan router",
		ctxpacket.ExecutionResult{
			Action:        "nmap -sV --top-ports 1000 192.168.50.1",
			ExitStatus:    "-1",
			Assessment:    "ambiguous",
			OutputSummary: "(none)",
			Signals:       []string{"execution_timeout"},
			FailureClass:  "execution_interrupted",
		},
		nil,
	)
	for _, want := range []string{
		"Status: in progress.",
		`Evidence: "nmap -sV --top-ports 1000 192.168.50.1" exited with -1.`,
		"Execution was interrupted before the active work completed.",
		"Assessment: ambiguous.",
		"Signals: execution_timeout.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in %q", want, summary)
		}
	}
	if strings.Contains(summary, "encountered a failure") {
		t.Fatalf("summary incorrectly treated interrupted work as failure: %q", summary)
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
