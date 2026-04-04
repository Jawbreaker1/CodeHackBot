package interactivecli

import (
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

// StepVisualState is the compact UI-facing state for a semantic plan step.
type StepVisualState string

const (
	StepVisualPending    StepVisualState = "pending"
	StepVisualInProgress StepVisualState = "in_progress"
	StepVisualDone       StepVisualState = "done"
	StepVisualBlocked    StepVisualState = "blocked"
)

// ReviewVisualState is the compact UI-facing state for the latest pre-execution review.
type ReviewVisualState string

const (
	ReviewVisualUnknown ReviewVisualState = "unknown"
	ReviewVisualExecute ReviewVisualState = "execute"
	ReviewVisualRevise  ReviewVisualState = "revise"
	ReviewVisualBlocked ReviewVisualState = "blocked"
)

// PlanStepView is one semantic plan step for the worker TUI.
type PlanStepView struct {
	Label string
	State StepVisualState
}

// ViewState is the presentation contract for the worker interactive UI.
// It must be derived from current runtime/session state and must not add behavior.
type ViewState struct {
	SessionStatus string

	Goal                 string
	ReportingRequirement string
	CurrentObjective     string
	WorkerGoal           string
	PlanSummary          string
	PlanSteps            []PlanStepView
	ActiveStep           string
	CurrentStepState     StepVisualState

	TaskState     string
	CurrentTarget string
	MissingFact   string

	LatestCommand       string
	LatestResultSummary string
	LatestAssessment    string
	LatestFailureClass  string
	LatestSignals       []string
	LatestLog           string

	LatestActionReview ReviewVisualState
	LatestStepEval     StepVisualState
	LatestStepSummary  string

	ScopeState    string
	ApprovalState string
	Model         string
	ContextUsage  string
	WorkingDir    string
}

// BuildViewState projects the current worker packet into a UI-facing state shape.
func BuildViewState(packet ctxpacket.WorkerPacket, sessionStatus string) ViewState {
	planSteps := buildPlanStepViews(packet.PlanState)
	return ViewState{
		SessionStatus: sessionStatus,

		Goal:                 strings.TrimSpace(packet.SessionFoundation.Goal),
		ReportingRequirement: strings.TrimSpace(packet.SessionFoundation.ReportingRequirement),
		CurrentObjective:     strings.TrimSpace(packet.CurrentStep.Objective),
		WorkerGoal:           strings.TrimSpace(packet.PlanState.WorkerGoal),
		PlanSummary:          strings.TrimSpace(packet.PlanState.Summary),
		PlanSteps:            planSteps,
		ActiveStep:           strings.TrimSpace(packet.PlanState.ActiveStep),
		CurrentStepState:     deriveCurrentStepState(packet, planSteps),

		TaskState:     strings.TrimSpace(packet.TaskRuntime.State),
		CurrentTarget: strings.TrimSpace(packet.TaskRuntime.CurrentTarget),
		MissingFact:   strings.TrimSpace(packet.TaskRuntime.MissingFact),

		LatestCommand:       strings.TrimSpace(packet.LatestExecutionResult.Action),
		LatestResultSummary: strings.TrimSpace(packet.LatestExecutionResult.OutputSummary),
		LatestAssessment:    strings.TrimSpace(packet.LatestExecutionResult.Assessment),
		LatestFailureClass:  strings.TrimSpace(packet.LatestExecutionResult.FailureClass),
		LatestSignals:       append([]string(nil), packet.LatestExecutionResult.Signals...),
		LatestLog:           latestLogPath(packet),

		LatestActionReview: deriveLatestActionReview(packet.LatestExecutionResult),
		LatestStepEval:     deriveLatestStepEval(packet, planSteps),
		LatestStepSummary:  strings.TrimSpace(packet.RunningSummary),

		ScopeState:    strings.TrimSpace(packet.OperatorState.ScopeState),
		ApprovalState: strings.TrimSpace(packet.OperatorState.ApprovalState),
		Model:         strings.TrimSpace(packet.OperatorState.Model),
		ContextUsage:  strings.TrimSpace(packet.OperatorState.ContextUsage),
		WorkingDir:    strings.TrimSpace(packet.OperatorState.WorkingDir),
	}
}

func buildPlanStepViews(plan ctxpacket.PlanState) []PlanStepView {
	if len(plan.Steps) == 0 {
		return nil
	}
	active := strings.TrimSpace(plan.ActiveStep)
	blocked := strings.TrimSpace(plan.BlockedStep)
	activeIdx := -1
	for i, step := range plan.Steps {
		if strings.TrimSpace(step) == active {
			activeIdx = i
			break
		}
	}
	steps := make([]PlanStepView, 0, len(plan.Steps))
	for i, step := range plan.Steps {
		state := StepVisualPending
		switch {
		case blocked != "" && strings.TrimSpace(step) == blocked:
			state = StepVisualBlocked
		case activeIdx >= 0 && i < activeIdx:
			state = StepVisualDone
		case activeIdx >= 0 && i == activeIdx:
			state = StepVisualInProgress
		}
		steps = append(steps, PlanStepView{
			Label: strings.TrimSpace(step),
			State: state,
		})
	}
	return steps
}

func deriveCurrentStepState(packet ctxpacket.WorkerPacket, steps []PlanStepView) StepVisualState {
	if strings.TrimSpace(packet.PlanState.BlockedStep) != "" || strings.EqualFold(strings.TrimSpace(packet.TaskRuntime.State), "blocked") {
		return StepVisualBlocked
	}
	if strings.EqualFold(strings.TrimSpace(packet.TaskRuntime.State), "done") {
		return StepVisualDone
	}
	for _, step := range steps {
		if strings.TrimSpace(step.Label) == strings.TrimSpace(packet.PlanState.ActiveStep) {
			return step.State
		}
	}
	if strings.TrimSpace(packet.PlanState.ActiveStep) != "" || strings.TrimSpace(packet.CurrentStep.Objective) != "" {
		return StepVisualInProgress
	}
	return StepVisualPending
}

func deriveLatestStepEval(packet ctxpacket.WorkerPacket, steps []PlanStepView) StepVisualState {
	summary := strings.TrimSpace(packet.RunningSummary)
	switch {
	case strings.HasPrefix(summary, "Plan step blocked"):
		return StepVisualBlocked
	case strings.HasPrefix(summary, "Plan step complete"):
		return StepVisualDone
	case strings.HasPrefix(summary, "Plan step still in progress"):
		return StepVisualInProgress
	}
	return deriveCurrentStepState(packet, steps)
}

func deriveLatestActionReview(result ctxpacket.ExecutionResult) ReviewVisualState {
	if strings.TrimSpace(result.FailureClass) != "pre_execution_review" {
		return ReviewVisualUnknown
	}
	for _, signal := range result.Signals {
		switch strings.TrimSpace(signal) {
		case "action_needs_revision":
			return ReviewVisualRevise
		}
	}
	return ReviewVisualBlocked
}
