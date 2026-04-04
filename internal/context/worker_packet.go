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
	Mode             string
	WorkerGoal       string
	Summary          string
	Steps            []string
	ActiveStep       string
	BlockedStep      string
	ReplanConditions []string
}

// ExecutionResult is the minimal latest execution truth for the rebuild path.
type ExecutionResult struct {
	Action        string
	ExitStatus    string
	OutputSummary string
	LogRefs       []string
	ArtifactRefs  []string
	Assessment    string
	Signals       []string
	FailureClass  string
}

// OperatorState is the visible operator/runtime state in the context packet.
type OperatorState struct {
	ScopeState    string
	ApprovalState string
	ModeHint      string
	Model         string
	ContextUsage  string
	WorkingDir    string
	PendingAction string
	PendingMode   string
	PendingExec   string
	PendingLog    string
}

// WorkerPacket is the authoritative v1 worker context packet.
type WorkerPacket struct {
	BehaviorFrame            behavior.Frame
	SessionFoundation        session.Foundation
	CurrentStep              Step
	TaskRuntime              TaskRuntime
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

// RenderedSection is one named rendered packet section.
type RenderedSection struct {
	Name    string
	Content string
}

// Render produces a stable human-readable representation for inspection.
func (p WorkerPacket) Render() string {
	rendered := make([]string, 0, len(p.RenderSections()))
	for _, section := range p.RenderSections() {
		rendered = append(rendered, sectionBlock(section.Name, section.Content))
	}
	return strings.Join(rendered, "\n\n")
}

// RenderWithoutBehaviorFrame produces a stable human-readable packet render
// without the behavior_frame section. This is intended for model-facing user
// context where the behavior frame is already supplied as the system message.
func (p WorkerPacket) RenderWithoutBehaviorFrame() string {
	sections := p.RenderSections()
	rendered := make([]string, 0, len(sections))
	for _, section := range sections {
		if section.Name == "behavior_frame" {
			continue
		}
		rendered = append(rendered, sectionBlock(section.Name, section.Content))
	}
	return strings.Join(rendered, "\n\n")
}

// RenderSections returns the stable ordered rendered sections for the packet.
func (p WorkerPacket) RenderSections() []RenderedSection {
	return []RenderedSection{
		{Name: "behavior_frame", Content: p.BehaviorFrame.PromptText()},
		{Name: "session_foundation", Content: renderSessionFoundation(p.SessionFoundation)},
		{Name: "current_step", Content: renderStep(p.CurrentStep)},
		{Name: "task_runtime", Content: renderTaskRuntime(p.TaskRuntime)},
		{Name: "plan_state", Content: renderPlanState(p.PlanState)},
		{Name: "recent_conversation", Content: renderConversation(p.RecentConversation)},
		{Name: "older_conversation_summary", Content: blankOrValue(p.OlderConversationSummary)},
		{Name: "latest_execution_result", Content: renderExecutionResult(p.LatestExecutionResult)},
		{Name: "running_summary", Content: blankOrValue(p.RunningSummary)},
		{Name: "relevant_recent_results", Content: renderExecutionResults(p.RelevantRecentResults)},
		{Name: "memory_bank_retrievals", Content: renderList(p.MemoryBankRetrievals)},
		{Name: "capability_inputs", Content: renderList(p.CapabilityInputs)},
		{Name: "operator_state", Content: renderOperatorState(p.OperatorState)},
	}
}

func sectionBlock(name, content string) string {
	return fmt.Sprintf("[%s]\n%s", name, blankOrValue(content))
}

func renderSessionFoundation(f session.Foundation) string {
	lines := []string{
		"goal: " + f.Goal,
		"reporting_requirement: " + f.ReportingRequirement,
	}
	return strings.Join(lines, "\n")
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
		"mode: " + blankOrValue(p.Mode),
		"worker_goal: " + blankOrValue(p.WorkerGoal),
		"summary: " + blankOrValue(p.Summary),
		"steps: " + joinOrNone(p.Steps),
		"active_step: " + blankOrValue(p.ActiveStep),
		"blocked_step: " + blankOrValue(p.BlockedStep),
		"replan_conditions: " + joinOrNone(p.ReplanConditions),
	}, "\n")
}

func renderTaskRuntime(t TaskRuntime) string {
	return strings.Join([]string{
		"state: " + blankOrValue(t.State),
		"current_target: " + blankOrValue(t.CurrentTarget),
		"missing_fact: " + blankOrValue(t.MissingFact),
	}, "\n")
}

func renderExecutionResult(r ExecutionResult) string {
	return strings.Join([]string{
		"action: " + blankOrValue(r.Action),
		"exit_status: " + blankOrValue(r.ExitStatus),
		"output_summary: " + blankOrValue(r.OutputSummary),
		"log_refs: " + joinOrNone(r.LogRefs),
		"artifact_refs: " + joinOrNone(r.ArtifactRefs),
		"assessment: " + blankOrValue(r.Assessment),
		"signals: " + joinOrNone(r.Signals),
		"failure_class: " + blankOrValue(r.FailureClass),
	}, "\n")
}

func renderExecutionResults(results []ExecutionResult) string {
	if len(results) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(results))
	for i, r := range results {
		parts = append(parts, fmt.Sprintf("result_%d:\n%s", i+1, indent(renderRetainedExecutionResult(r), "  ")))
	}
	return strings.Join(parts, "\n")
}

func renderRetainedExecutionResult(r ExecutionResult) string {
	return strings.Join([]string{
		"action: " + blankOrValue(compactText(r.Action, 140)),
		"exit_status: " + blankOrValue(r.ExitStatus),
		"output_summary: " + blankOrValue(compactText(r.OutputSummary, 180)),
		"log_ref: " + firstOrNone(r.LogRefs),
		"artifact_ref: " + firstOrNone(r.ArtifactRefs),
		"assessment: " + blankOrValue(r.Assessment),
		"signals: " + joinOrNone(r.Signals),
		"failure_class: " + blankOrValue(r.FailureClass),
	}, "\n")
}

func renderOperatorState(s OperatorState) string {
	return strings.Join([]string{
		"scope_state: " + blankOrValue(s.ScopeState),
		"approval_state: " + blankOrValue(s.ApprovalState),
		"mode_hint: " + blankOrValue(s.ModeHint),
		"model: " + blankOrValue(s.Model),
		"context_usage: " + blankOrValue(s.ContextUsage),
		"working_dir: " + blankOrValue(s.WorkingDir),
		"pending_action: " + blankOrValue(s.PendingAction),
		"pending_mode: " + blankOrValue(s.PendingMode),
		"pending_exec: " + blankOrValue(s.PendingExec),
		"pending_log: " + blankOrValue(s.PendingLog),
	}, "\n")
}

func renderList(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return joinOrNone(items)
}

func renderConversation(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, blankOrValue(item))
	}
	return strings.Join(lines, "\n")
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, " | ")
}

func firstOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return blankOrValue(items[0])
}

func blankOrValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "(none)"
	}
	return v
}

func compactText(v string, max int) string {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\n", " | "))
	if v == "" || max <= 0 || len(v) <= max {
		return v
	}
	if max <= 3 {
		return v[:max]
	}
	return strings.TrimSpace(v[:max-3]) + "..."
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
