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
