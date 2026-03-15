package context

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

// Step is the minimal step contract used by the rebuild worker packet.
type Step struct {
	Objective        string
	DoneCondition    string
	FailCondition    string
	ExpectedEvidence []string
	RemainingBudget  string
}

// PlanState is the minimal high-level semantic plan state.
type PlanState struct {
	Steps       []string
	ActiveStep  string
	BlockedStep string
}

// ExecutionResult is the minimal latest execution truth for the rebuild path.
type ExecutionResult struct {
	Action        string
	ExitStatus    string
	OutputSummary string
	LogRefs       []string
	ArtifactRefs  []string
	FailureClass  string
}

// OperatorState is the visible operator/runtime state in the context packet.
type OperatorState struct {
	ScopeState    string
	ApprovalState string
	Model         string
	ContextUsage  string
}

// WorkerPacket is the authoritative v1 worker context packet.
type WorkerPacket struct {
	BehaviorFrame            behavior.Frame
	SessionFoundation        session.Foundation
	CurrentStep              Step
	PlanState                PlanState
	RecentConversation       []string
	OlderConversationSummary string
	LatestExecutionResult    ExecutionResult
	RunningSummary           string
	RelevantRecentResults    []ExecutionResult
	MemoryBankRetrievals     []string
	CapabilityInputs         []string
	OperatorState            OperatorState
}

// Render produces a stable human-readable representation for inspection.
func (p WorkerPacket) Render() string {
	var sections []string
	sections = append(sections, section("behavior_frame", p.BehaviorFrame.PromptText()))
	sections = append(sections, section("session_foundation", renderSessionFoundation(p.SessionFoundation)))
	sections = append(sections, section("current_step", renderStep(p.CurrentStep)))
	sections = append(sections, section("plan_state", renderPlanState(p.PlanState)))
	sections = append(sections, section("recent_conversation", renderList(p.RecentConversation)))
	sections = append(sections, section("older_conversation_summary", blankOrValue(p.OlderConversationSummary)))
	sections = append(sections, section("latest_execution_result", renderExecutionResult(p.LatestExecutionResult)))
	sections = append(sections, section("running_summary", blankOrValue(p.RunningSummary)))
	sections = append(sections, section("relevant_recent_results", renderExecutionResults(p.RelevantRecentResults)))
	sections = append(sections, section("memory_bank_retrievals", renderList(p.MemoryBankRetrievals)))
	sections = append(sections, section("capability_inputs", renderList(p.CapabilityInputs)))
	sections = append(sections, section("operator_state", renderOperatorState(p.OperatorState)))
	return strings.Join(sections, "\n\n")
}

func section(name, content string) string {
	return fmt.Sprintf("[%s]\n%s", name, blankOrValue(content))
}

func renderSessionFoundation(f session.Foundation) string {
	return strings.Join([]string{
		"goal: " + f.Goal,
		"reporting_requirement: " + f.ReportingRequirement,
	}, "\n")
}

func renderStep(s Step) string {
	return strings.Join([]string{
		"objective: " + blankOrValue(s.Objective),
		"done_condition: " + blankOrValue(s.DoneCondition),
		"fail_condition: " + blankOrValue(s.FailCondition),
		"expected_evidence: " + joinOrNone(s.ExpectedEvidence),
		"remaining_budget: " + blankOrValue(s.RemainingBudget),
	}, "\n")
}

func renderPlanState(p PlanState) string {
	return strings.Join([]string{
		"steps: " + joinOrNone(p.Steps),
		"active_step: " + blankOrValue(p.ActiveStep),
		"blocked_step: " + blankOrValue(p.BlockedStep),
	}, "\n")
}

func renderExecutionResult(r ExecutionResult) string {
	return strings.Join([]string{
		"action: " + blankOrValue(r.Action),
		"exit_status: " + blankOrValue(r.ExitStatus),
		"output_summary: " + blankOrValue(r.OutputSummary),
		"log_refs: " + joinOrNone(r.LogRefs),
		"artifact_refs: " + joinOrNone(r.ArtifactRefs),
		"failure_class: " + blankOrValue(r.FailureClass),
	}, "\n")
}

func renderExecutionResults(results []ExecutionResult) string {
	if len(results) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(results))
	for i, r := range results {
		parts = append(parts, fmt.Sprintf("result_%d:\n%s", i+1, indent(renderExecutionResult(r), "  ")))
	}
	return strings.Join(parts, "\n")
}

func renderOperatorState(s OperatorState) string {
	return strings.Join([]string{
		"scope_state: " + blankOrValue(s.ScopeState),
		"approval_state: " + blankOrValue(s.ApprovalState),
		"model: " + blankOrValue(s.Model),
		"context_usage: " + blankOrValue(s.ContextUsage),
	}, "\n")
}

func renderList(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return joinOrNone(items)
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, " | ")
}

func blankOrValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "(none)"
	}
	return v
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
