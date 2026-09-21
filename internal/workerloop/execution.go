package workerloop

import (
	"context"
	"fmt"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func (l Loop) execute(ctx context.Context, current *ctxpacket.WorkerPacket, response Response) (bool, error) {
	if err := l.emitWithRationale(EventActionProposed, *current, response.Command, response.Summary); err != nil {
		return false, err
	}
	action, failure := prepareAction(response, current.OperatorState.WorkingDir)
	if failure != nil {
		// Validation failures never replace actual execution evidence.
		current.RunningSummary = "Action rejected before execution: " + failure.OutputSummary
		return false, l.capture(current.Budget.Used, "post-validation", *current)
	}
	if l.Approver == nil {
		return false, fmt.Errorf("execution requires an approver")
	}
	plan, err := l.Executor.Plan(action)
	if err != nil {
		return false, fmt.Errorf("prepare execution: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	decision, err := l.Approver.Approve(ctx, approval.Request{Command: plan.ActualExec, UseShell: plan.Action.UseShell, Cwd: plan.Action.Cwd, Impact: response.Impact})
	if err != nil {
		return false, fmt.Errorf("approval failed: %w", err)
	}
	current.OperatorState.ApprovalState = string(decision)
	if decision != approval.DecisionApproveOnce && decision != approval.DecisionApproveSession {
		return false, fmt.Errorf("execution denied by user")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	current.OperatorState.PendingAction = plan.Requested
	current.OperatorState.PendingMode = plan.ExecutionMode
	current.OperatorState.PendingExec = plan.ActualExec
	current.OperatorState.PendingLog = plan.LogPath
	if err := l.capture(current.Budget.Used, "pre-action", *current); err != nil {
		return false, err
	}
	// Recording failure is a stop condition before any external side effect.
	if err := l.emit(EventExecutionStarted, *current, "execution started"); err != nil {
		return false, err
	}
	result, execErr := l.Executor.RunPlanned(ctx, plan)
	if execErr != nil && result.LogPath == "" {
		return false, fmt.Errorf("execution evidence unavailable: %w", execErr)
	}
	current.OperatorState.PendingAction, current.OperatorState.PendingMode = "", ""
	current.OperatorState.PendingExec, current.OperatorState.PendingLog = "", ""
	evidence := combineSummaries(result.StdoutSummary, result.StderrSummary)
	if current.LatestExecutionResult.Action != "" {
		current.RelevantRecentResults = append([]ctxpacket.ExecutionResult{current.LatestExecutionResult}, current.RelevantRecentResults...)
	}
	current.LatestExecutionResult = ctxpacket.ExecutionResult{
		Action: result.Action, ActualExec: result.ActualExec, ExecutionMode: result.ExecutionMode,
		Cwd: result.Cwd, StartedAt: result.StartedAt, FinishedAt: result.FinishedAt,
		ExitStatus: fmt.Sprint(result.ExitStatus), OutputSummary: compactOutputSummary(evidence),
		OutputEvidence: evidence, LogRefs: []string{result.LogPath}, ArtifactRefs: result.ArtifactRefs,
		Assessment: result.Assessment, Signals: result.Signals, FailureClass: result.FailureClass,
	}
	current.RunningSummary = buildRunningSummary(current.SessionFoundation.Goal, current.LatestExecutionResult, nil)
	if err := l.emit(EventExecutionFinished, *current, "execution finished"); err != nil {
		return false, err
	}
	if err := l.capture(current.Budget.Used, "post-action", *current); err != nil {
		return false, err
	}
	// A failed command is an observation for the model, not a worker failure.
	return true, ctx.Err()
}
