package context

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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

// PlanRevision records a planning transition, not proof about a target. The
// preceding execution reference makes its ordering inspectable after recovery.
type PlanRevision struct {
	Turn              int
	AfterExecutionLog string
	Plan              PlanState
}

// ExecutionResult is the minimal latest execution truth for the rebuild path.
type ExecutionResult struct {
	Action         string
	ActualExec     string
	ExecutionMode  string
	Cwd            string
	StartedAt      time.Time
	FinishedAt     time.Time
	ExitStatus     string
	OutputSummary  string
	OutputEvidence string
	LogRefs        []string
	ArtifactRefs   []string
	Assessment     string
	Signals        []string
	FailureClass   string
}

// OperatorState is the visible operator/runtime state in the context packet.
type OperatorState struct {
	ScopeState    string
	ApprovalState string
	ModeHint      string
	Model         string
	ContextUsage  string
	// ContextUsedBytes and ContextLimitBytes describe the latest model request
	// after context projection. They are application byte accounting, not a
	// provider tokenizer estimate.
	ContextUsedBytes  int
	ContextLimitBytes int
	WorkingDir        string
	PendingAction     string
	PendingMode       string
	PendingExec       string
	PendingLog        string
}

// WorkerPacket is the authoritative v1 worker context packet.
type WorkerPacket struct {
	BehaviorFrame            behavior.Frame
	SessionFoundation        session.Foundation
	CurrentStep              Step
	TaskRuntime              TaskRuntime
	PlanState                PlanState
	PlanHistory              []PlanRevision
	RecentConversation       []string
	OlderConversationSummary string
	LatestExecutionResult    ExecutionResult
	RunningSummary           string
	RelevantRecentResults    []ExecutionResult
	MemoryBankRetrievals     []string
	CapabilityInputs         []string
	OperatorState            OperatorState
	Budget                   TurnBudget
	ContextNotes             []string
}

// TurnBudget survives pause/resume. Each model decision, including a question
// or plan change, consumes one turn. Evaluation never grants another turn.
type TurnBudget struct {
	Limit int
	Used  int
}

func DefaultExecutionCapabilityInputs() []string {
	return []string{
		"operating_environment: standard Kali Linux environment (full Kali Linux assessment image); verify installed binaries, modules, formats, and data sources before relying on them",
		"tooling_preference: prefer established security tools and workflows over improvised shell logic when a standard tool fits the task",
		"tooling_examples: common tooling may include Nmap, Metasploit Framework, Burp Suite, Wireshark, Gobuster, ffuf, sqlmap, Hydra, John the Ripper, Hashcat, Aircrack-ng, enum4linux, smbclient, Impacket, SearchSploit, and Exploit-DB data",
		"browser_assessment: when a preprovisioned Playwright helper and browser are available, use them for explicitly scoped web application traversal, role flows, DOM/API observations, screenshots, traces, and network evidence; verify the helper and browser versions first, keep state in the worker workspace, and declare each produced artifact",
		"research_and_helpers: use approved local CVE/NVD or Exploit-DB data; connected mode may use verified curl/wget web fetches for advisory or documentation URLs with URL/status/time/artifact provenance; air-gapped mode must never fetch externally and uses only local snapshots; build and validate a small task-local helper when established tooling is insufficient",
		"parallel_work: when independent bounded searches or recovery strategies exist, recommend isolated task partitions and a later evidence-backed validation instead of repeating the same attempt",
		"execution_expectations: choose bounded, reproducible, evidence-producing actions and judge progress from real command output and artifacts",
	}
}

func NewInitialWorkerPacket(frame behavior.Frame, foundation session.Foundation, cwd, model, approvalState string, maxSteps int) WorkerPacket {
	if maxSteps <= 0 {
		maxSteps = 1
	}
	taskRuntime := TaskRuntime{State: "running"}
	return WorkerPacket{
		BehaviorFrame:     frame,
		SessionFoundation: foundation,
		CurrentStep: Step{
			Objective:        foundation.Goal,
			DoneCondition:    "the stated user goal has been satisfied with evidence",
			FailCondition:    "cannot make honest progress on the stated user goal",
			ExpectedEvidence: []string{"command logs", "artifacts if produced"},
			RemainingBudget:  fmt.Sprintf("%d steps", maxSteps),
		},
		TaskRuntime:        taskRuntime,
		Budget:             TurnBudget{Limit: maxSteps},
		RecentConversation: []string{"User: " + foundation.Goal},
		RunningSummary:     "Worker loop starting from the stated user goal.",
		CapabilityInputs:   DefaultExecutionCapabilityInputs(),
		OperatorState: OperatorState{
			ScopeState:    "not_enforced_by_runtime",
			ApprovalState: approvalState,
			Model:         model,
			ContextUsage:  "(unset)",
			WorkingDir:    cwd,
		},
	}
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
		{Name: "plan_history", Content: renderPlanHistory(p.PlanHistory)},
		{Name: "recent_conversation", Content: renderConversation(p.RecentConversation)},
		{Name: "older_conversation_summary", Content: blankOrValue(p.OlderConversationSummary)},
		{Name: "latest_execution_result", Content: renderExecutionResult(p.LatestExecutionResult)},
		{Name: "running_summary", Content: blankOrValue(p.RunningSummary)},
		{Name: "relevant_recent_results", Content: renderExecutionResults(p.RelevantRecentResults)},
		{Name: "memory_bank_retrievals", Content: renderList(p.MemoryBankRetrievals)},
		{Name: "capability_inputs", Content: renderList(p.CapabilityInputs)},
		{Name: "operator_state", Content: renderOperatorState(p.OperatorState)},
		{Name: "context_notes", Content: renderList(p.ContextNotes)},
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

func renderPlanHistory(history []PlanRevision) string {
	parts := make([]string, 0, len(history))
	for _, revision := range history {
		parts = append(parts, fmt.Sprintf("turn: %d\nafter_execution_log: %s\n%s", revision.Turn, blankOrValue(revision.AfterExecutionLog), renderPlanState(revision.Plan)))
	}
	return strings.Join(parts, "\n\n")
}

func renderExecutionResult(r ExecutionResult) string {
	return strings.Join([]string{
		"action: " + blankOrValue(r.Action),
		"actual_exec: " + blankOrValue(r.ActualExec),
		"execution_mode: " + blankOrValue(r.ExecutionMode),
		"cwd: " + blankOrValue(r.Cwd),
		"started_at: " + r.StartedAt.Format(time.RFC3339Nano),
		"finished_at: " + r.FinishedAt.Format(time.RFC3339Nano),
		"exit_status: " + blankOrValue(r.ExitStatus),
		// Quoted strings keep output lines from impersonating packet metadata.
		"output_summary: " + strconv.Quote(blankOrValue(r.OutputSummary)),
		"output_evidence: " + strconv.Quote(blankOrValue(r.OutputEvidence)),
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
	return renderExecutionResult(r)
}

func renderOperatorState(s OperatorState) string {
	return strings.Join([]string{
		"scope_state: " + blankOrValue(s.ScopeState),
		"approval_state: " + blankOrValue(s.ApprovalState),
		"mode_hint: " + blankOrValue(s.ModeHint),
		"model: " + blankOrValue(s.Model),
		"context_usage: " + blankOrValue(s.ContextUsage),
		"context_used_bytes: " + strconv.Itoa(s.ContextUsedBytes),
		"context_limit_bytes: " + strconv.Itoa(s.ContextLimitBytes),
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
