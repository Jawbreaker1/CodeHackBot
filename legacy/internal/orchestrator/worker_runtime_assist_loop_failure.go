package orchestrator

import (
	"context"
	"strings"
)

func handleAssistExecutionFailure(
	ctx context.Context,
	manager *Manager,
	cfg WorkerRunConfig,
	task TaskSpec,
	workDir string,
	actionSteps int,
	turn int,
	toolCalls int,
	command string,
	args []string,
	runErr error,
	output []byte,
	summary string,
	logPath string,
	lastExecFeedback *assistExecutionFeedback,
	mode *string,
	recoverHint *string,
	missingToolInstallAttempts map[string]struct{},
	missingToolRetryCount map[string]int,
	addObservation func(string),
	markRecoverTransition func(int, string, map[string]any) error,
	transitionCause string,
	checkScopeDenied bool,
) (bool, error) {
	missingToolHandled, missingToolErr := handleAssistMissingToolRecovery(
		ctx,
		manager,
		cfg,
		task,
		workDir,
		actionSteps,
		turn,
		toolCalls,
		command,
		args,
		runErr,
		output,
		summary,
		mode,
		recoverHint,
		missingToolInstallAttempts,
		missingToolRetryCount,
		addObservation,
		markRecoverTransition,
	)
	if missingToolErr != nil {
		return false, missingToolErr
	}
	if missingToolHandled {
		return true, nil
	}
	if checkScopeDenied && strings.Contains(strings.ToLower(runErr.Error()), WorkerFailureScopeDenied) {
		_ = emitWorkerFailure(manager, cfg, task, runErr, WorkerFailureScopeDenied, map[string]any{
			"step":       actionSteps,
			"turn":       turn,
			"tool_calls": toolCalls,
			"command":    command,
			"args":       args,
			"log_path":   logPath,
		})
		return false, runErr
	}
	if *mode == "recover" {
		reason := WorkerFailureCommandFailed
		if errorsIsTimeout(runErr) {
			reason = WorkerFailureCommandTimeout
		}
		_ = emitWorkerFailure(manager, cfg, task, runErr, reason, mergeFailureDetails(map[string]any{
			"step":       actionSteps,
			"turn":       turn,
			"tool_calls": toolCalls,
			"command":    command,
			"args":       args,
			"log_path":   logPath,
		}, latestAssistFailureDetails(lastExecFeedback)))
		return false, runErr
	}
	*mode = "recover"
	*recoverHint = summary
	if recErr := markRecoverTransition(turn, transitionCause, map[string]any{
		"command": command,
		"args":    args,
	}); recErr != nil {
		return false, recErr
	}
	return true, nil
}
