package orchestrator

import (
	"context"
	"fmt"
)

func handleAssistMissingToolRecovery(
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
	mode *string,
	recoverHint *string,
	missingToolInstallAttempts map[string]struct{},
	missingToolRetryCount map[string]int,
	addObservation func(string),
	markRecoverTransition func(int, string, map[string]any) error,
) (bool, error) {
	missingTool := detectMissingExecutable(command, runErr, output)
	if missingTool == "" {
		return false, nil
	}
	remediation := handleMissingToolRemediation(
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
		missingTool,
		missingToolInstallAttempts,
	)
	if !remediation.Handled {
		return false, nil
	}
	toolKey := normalizeToolToken(remediation.ToolName)
	if toolKey != "" {
		missingToolRetryCount[toolKey]++
		if missingToolRetryCount[toolKey] > 1 {
			return false, emitAssistNoProgressFailure(manager, cfg, task, *mode, actionSteps, turn, toolCalls, command, args, fmt.Sprintf("repeated missing-tool retry blocked for %s", toolKey), map[string]any{
				"missing_tool":                   toolKey,
				"missing_tool_retry_count":       missingToolRetryCount[toolKey],
				"missing_tool_available_tools":   discoverAvailableFallbackTools(),
				"missing_tool_contract_enforced": true,
			}, nil)
		}
	}
	if remediation.Observation != "" {
		addObservation(remediation.Observation)
	}
	if remediation.RecoverHint != "" {
		*recoverHint = remediation.RecoverHint
	} else {
		*recoverHint = summary
	}
	if *mode != "recover" {
		*mode = "recover"
		if recErr := markRecoverTransition(turn, "missing_tool", map[string]any{
			"command": command,
			"args":    args,
			"tool":    remediation.ToolName,
		}); recErr != nil {
			return false, recErr
		}
	}
	return true, nil
}
