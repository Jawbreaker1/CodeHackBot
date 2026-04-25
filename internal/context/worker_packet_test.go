package context

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestWorkerPacketRenderIncludesAllCoreSections(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame: behavior.Frame{
			SystemPrompt: "prompt",
			AgentsText:   "agents",
			RuntimeMode:  "worker",
			Parameters: map[string]string{
				"approval_mode": "default",
			},
		},
		SessionFoundation: session.Foundation{
			Goal:                 "Recover the zip password",
			ReportingRequirement: "owasp",
		},
		CurrentStep: Step{
			Objective:        "Inspect archive metadata",
			DoneCondition:    "metadata captured",
			FailCondition:    "archive unreadable",
			ExpectedEvidence: []string{"zipinfo output"},
			RemainingBudget:  "5 actions",
		},
		TaskRuntime: TaskRuntime{
			State:         "running",
			CurrentTarget: "secret.zip",
			MissingFact:   "credential or password required for secret.zip",
		},
		PlanState: PlanState{
			Steps:      []string{"inspect archive", "attempt recovery", "verify result"},
			ActiveStep: "inspect archive",
		},
		RecentConversation:       []string{"User: crack secret.zip", "Assistant: inspecting archive"},
		OlderConversationSummary: "User wants password and contents.",
		LatestExecutionResult: ExecutionResult{
			Action:        "zipinfo -v secret.zip",
			ExitStatus:    "0",
			OutputSummary: "Archive readable.",
			LogRefs:       []string{"logs/cmd-1.log"},
			ArtifactRefs:  []string{"artifacts/zipinfo.txt"},
			Assessment:    "success",
			Signals:       []string{"archive_readable"},
		},
		RunningSummary: "Archive identified and metadata readable.",
		RelevantRecentResults: []ExecutionResult{
			{Action: "ls -la", ExitStatus: "0", OutputSummary: "secret.zip present"},
		},
		MemoryBankRetrievals: []string{"artifact_ref: secret.zip"},
		CapabilityInputs:     []string{"runbook_hint: inspect metadata before recovery"},
		OperatorState: OperatorState{
			ScopeState:    "local_only",
			ApprovalState: "pending",
			Model:         "qwen3.5",
			ContextUsage:  "low",
		},
	}

	rendered := packet.Render()
	for _, want := range []string{
		"[behavior_frame]",
		"[session_foundation]",
		"[current_step]",
		"[task_runtime]",
		"[plan_state]",
		"[recent_conversation]",
		"[older_conversation_summary]",
		"[latest_execution_result]",
		"[running_summary]",
		"[relevant_recent_results]",
		"[memory_bank_retrievals]",
		"[capability_inputs]",
		"[operator_state]",
		"Recover the zip password",
		"zipinfo -v secret.zip",
		"Archive identified and metadata readable.",
		"current_target: secret.zip",
		"missing_fact: credential or password required for secret.zip",
		"assessment: success",
		"signals: archive_readable",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("Render() missing %q in:\n%s", want, rendered)
		}
	}
}

func TestNewInitialWorkerPacketSetsSharedExecutionDefaults(t *testing.T) {
	frame := behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"}
	foundation := session.Foundation{
		Goal:                 "Inspect secret.zip",
		ReportingRequirement: "owasp",
	}

	packet := NewInitialWorkerPacket(frame, foundation, "/tmp/testrepo", "test-model", "approved_session", 8)

	if packet.SessionFoundation.Goal != foundation.Goal {
		t.Fatalf("goal = %q", packet.SessionFoundation.Goal)
	}
	if packet.CurrentStep.Objective != foundation.Goal {
		t.Fatalf("objective = %q", packet.CurrentStep.Objective)
	}
	if packet.PlanState.ActiveStep != foundation.Goal {
		t.Fatalf("active_step = %q", packet.PlanState.ActiveStep)
	}
	if packet.OperatorState.WorkingDir != "/tmp/testrepo" {
		t.Fatalf("working_dir = %q", packet.OperatorState.WorkingDir)
	}
	if packet.OperatorState.Model != "test-model" {
		t.Fatalf("model = %q", packet.OperatorState.Model)
	}
	if len(packet.CapabilityInputs) == 0 {
		t.Fatalf("CapabilityInputs should not be empty")
	}
	if !strings.Contains(strings.Join(packet.CapabilityInputs, "\n"), "Metasploit Framework") {
		t.Fatalf("CapabilityInputs missing tooling context: %#v", packet.CapabilityInputs)
	}
}

func TestRenderExecutionResultsPreservesRetainedEntriesForModelContext(t *testing.T) {
	results := []ExecutionResult{
		{
			Action:         "printf " + strings.Repeat("a", 200),
			ExitStatus:     "0",
			OutputSummary:  "stdout: " + strings.Repeat("b", 300),
			OutputEvidence: "stdout: first line\nstdout: second line\nstdout: third line",
			LogRefs:        []string{"logs/cmd-1.log", "logs/cmd-2.log"},
			ArtifactRefs:   []string{"artifacts/a.txt", "artifacts/b.txt"},
			Assessment:     "success",
			Signals:        []string{"archive_readable"},
		},
	}

	rendered := renderExecutionResults(results)
	for _, want := range []string{
		"result_1:",
		"printf " + strings.Repeat("a", 200),
		"output_evidence: stdout: first line",
		"stdout: second line",
		"stdout: third line",
		"log_refs: logs/cmd-1.log | logs/cmd-2.log",
		"artifact_refs: artifacts/a.txt | artifacts/b.txt",
		"assessment: success",
		"signals: archive_readable",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("renderExecutionResults() missing %q in:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "...") {
		t.Fatalf("renderExecutionResults() should not compact retained evidence in:\n%s", rendered)
	}
}

func TestRenderConversationPreservesTurnStructure(t *testing.T) {
	packet := WorkerPacket{
		RecentConversation: []string{
			"User: first",
			"Assistant: second",
			"User: third",
		},
		MemoryBankRetrievals: []string{"artifact_ref: secret.zip", "finding_ref: 127.0.0.1"},
	}

	rendered := packet.Render()
	if !strings.Contains(rendered, "[recent_conversation]\nUser: first\nAssistant: second\nUser: third") {
		t.Fatalf("recent_conversation not rendered as raw turns:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[memory_bank_retrievals]\nartifact_ref: secret.zip | finding_ref: 127.0.0.1") {
		t.Fatalf("memory_bank_retrievals should remain compact list:\n%s", rendered)
	}
}

func TestRenderWithoutBehaviorFrameOmitsBehaviorSection(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame: behavior.Frame{
			SystemPrompt: "prompt",
			AgentsText:   "agents",
			RuntimeMode:  "worker",
		},
		SessionFoundation: session.Foundation{
			Goal:                 "Recover the zip password",
			ReportingRequirement: "owasp",
		},
		RunningSummary: "summary",
	}

	rendered := packet.RenderWithoutBehaviorFrame()
	if strings.Contains(rendered, "[behavior_frame]") {
		t.Fatalf("RenderWithoutBehaviorFrame() unexpectedly included behavior_frame:\n%s", rendered)
	}
	for _, want := range []string{
		"[session_foundation]",
		"[running_summary]",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderWithoutBehaviorFrame() missing %q in:\n%s", want, rendered)
		}
	}
}
