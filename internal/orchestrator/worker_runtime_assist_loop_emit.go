package orchestrator

import (
	"fmt"
	"strings"
)

func emitAssistCompletion(
	manager *Manager,
	cfg WorkerRunConfig,
	task TaskSpec,
	assistantModel string,
	statusReason string,
	final string,
	step int,
	turn int,
	toolCalls int,
	extraMeta map[string]any,
) error {
	statusReason = strings.TrimSpace(statusReason)
	if statusReason == "" {
		statusReason = "assist_complete"
	}
	allowFallbackWithoutFindings := statusReason == "assist_no_new_evidence"
	objectiveMet := !allowFallbackWithoutFindings
	final = strings.TrimSpace(final)
	if final == "" {
		final = "assistant marked task complete"
	}
	logPath, logErr := writeWorkerActionLog(cfg, fmt.Sprintf("%s-a%d-complete.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt), []byte(final+"\n"))
	if logErr != nil {
		_ = emitWorkerFailure(manager, cfg, task, logErr, WorkerFailureArtifactWrite, nil)
		return logErr
	}
	producedArtifacts := []string{logPath}
	derivedPaths, derivedErr := materializeAssistExpectedArtifacts(cfg, task, logPath, objectiveMet)
	if derivedErr != nil {
		_ = emitWorkerFailure(manager, cfg, task, derivedErr, WorkerFailureArtifactWrite, nil)
		return derivedErr
	}
	producedArtifacts = compactStrings(append(producedArtifacts, derivedPaths...))
	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskArtifact, map[string]any{
		"type":      "assist_summary",
		"title":     fmt.Sprintf("assistant completion summary (%s)", cfg.TaskID),
		"path":      logPath,
		"step":      step,
		"tool_call": toolCalls,
	})
	for _, derivedPath := range derivedPaths {
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskArtifact, map[string]any{
			"type":      "derived_assist_output",
			"title":     fmt.Sprintf("derived expected artifact (%s)", cfg.TaskID),
			"path":      derivedPath,
			"step":      step,
			"tool_call": toolCalls,
		})
	}
	requiredFindings := []string{"task_execution_result"}
	producedFindings := []string{"task_execution_result"}
	if allowFallbackWithoutFindings {
		requiredFindings = nil
		producedFindings = nil
	}
	findingMeta := map[string]any{
		"attempt":       cfg.Attempt,
		"reason":        statusReason,
		"assistant":     assistantModel,
		"steps":         step,
		"turns":         turn,
		"tool_calls":    toolCalls,
		"final_summary": final,
	}
	for k, v := range extraMeta {
		findingMeta[k] = v
	}
	if !allowFallbackWithoutFindings {
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFinding, map[string]any{
			"target":       primaryTaskTarget(task),
			"finding_type": "task_execution_result",
			"title":        "task action completed",
			"state":        FindingStateVerified,
			"severity":     "info",
			"confidence":   "medium",
			"source":       "worker_runtime_assist",
			"evidence":     producedArtifacts,
			"metadata":     findingMeta,
		})
	}
	verificationStatus := "reported_by_worker"
	if allowFallbackWithoutFindings {
		verificationStatus = "fallback_no_new_evidence"
	}
	whyMet := strings.TrimSpace(final)
	if whyMet == "" {
		whyMet = statusReason
	}
	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskCompleted, map[string]any{
		"attempt":   cfg.Attempt,
		"worker_id": cfg.WorkerID,
		"reason":    statusReason,
		"log_path":  logPath,
		"completion_contract": map[string]any{
			"status_reason":                   statusReason,
			"required_artifacts":              task.ExpectedArtifacts,
			"produced_artifacts":              producedArtifacts,
			"required_findings":               requiredFindings,
			"produced_findings":               producedFindings,
			"allow_fallback_without_findings": allowFallbackWithoutFindings,
			"verification_status":             verificationStatus,
			"objective_met":                   objectiveMet,
			"why_met":                         whyMet,
			"evidence_refs":                   producedArtifacts,
			"semantic_verifier":               "assistant_runtime_contract",
		},
	})
	return nil
}

func emitAssistNoProgressFailure(
	manager *Manager,
	cfg WorkerRunConfig,
	task TaskSpec,
	mode string,
	step int,
	turn int,
	toolCalls int,
	command string,
	args []string,
	reason string,
	extra map[string]any,
	lastExecFeedback *assistExecutionFeedback,
) error {
	lastAction := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	if lastAction == "" {
		lastAction = "unknown"
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "no evidence progress"
	}
	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
		"message":              "no progress after repeated low-value actions; failing task for replan",
		"step":                 step,
		"turn":                 turn,
		"tool_calls":           toolCalls,
		"mode":                 mode,
		"bounded_fallback":     "no_progress",
		"last_repeated_action": lastAction,
		"fallback_reason":      reason,
	})
	failErr := fmt.Errorf("no progress: repeated low-value action without new evidence (%s)", lastAction)
	details := map[string]any{
		"step":                 step,
		"turn":                 turn,
		"tool_calls":           toolCalls,
		"bounded_fallback":     "no_progress",
		"last_repeated_action": lastAction,
		"fallback_reason":      reason,
	}
	for k, v := range extra {
		details[k] = v
	}
	details = mergeFailureDetails(details, latestAssistFailureDetails(lastExecFeedback))
	_ = emitWorkerFailure(manager, cfg, task, failErr, WorkerFailureNoProgress, details)
	return failErr
}

func emitAssistNoNewEvidenceCompletion(
	manager *Manager,
	cfg WorkerRunConfig,
	task TaskSpec,
	assistantModel string,
	mode string,
	step int,
	turn int,
	toolCalls int,
	command string,
	args []string,
	resultStreak int,
	reason string,
) error {
	lastAction := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	if lastAction == "" {
		lastAction = "unknown"
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "repeated identical command results"
	}
	if !allowAssistNoNewEvidenceCompletion(task) {
		if taskLikelyLocalFileWorkflow(task) || archiveTaskRequiresPositiveProof(task) {
			notMetErr := fmt.Errorf("objective not met: no new evidence for proof-sensitive local workflow (%s)", lastAction)
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":                    "objective_not_met: no new evidence for proof-sensitive local workflow",
				"step":                       step,
				"turn":                       turn,
				"tool_calls":                 toolCalls,
				"mode":                       mode,
				"result_streak":              resultStreak,
				"bounded_fallback":           "no_new_evidence",
				"last_repeated_action":       lastAction,
				"fallback_reason":            reason,
				"completion_terminalization": "objective_not_met",
			})
			_ = emitWorkerFailure(manager, cfg, task, notMetErr, TaskFailureReasonObjectiveNotMet, map[string]any{
				"step":                       step,
				"turn":                       turn,
				"tool_calls":                 toolCalls,
				"result_streak":              resultStreak,
				"bounded_fallback":           "no_new_evidence",
				"last_repeated_action":       lastAction,
				"fallback_reason":            reason,
				"completion_terminalization": "objective_not_met",
			})
			return notMetErr
		}
		return emitAssistNoProgressFailure(manager, cfg, task, mode, step, turn, toolCalls, command, args, reason, map[string]any{
			"result_streak":                 resultStreak,
			"bounded_fallback":              "no_new_evidence",
			"no_new_evidence_policy":        "disallowed_for_recon_validation_non_summary",
			"completion_terminalization":    "no_progress",
			"completion_reason_candidate":   "assist_no_new_evidence",
			"completion_reason_enforcement": "fallback_rejected",
		}, nil)
	}
	summary := fmt.Sprintf("No new evidence after repeated identical runtime results. Last action: %s (result_streak=%d, reason=%s).", lastAction, resultStreak, reason)
	if isLowValueListingCommand(command, args) {
		return emitAssistNoProgressFailure(manager, cfg, task, mode, step, turn, toolCalls, command, args, reason, map[string]any{
			"result_streak": resultStreak,
		}, nil)
	}
	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
		"message":              "no new evidence after repeated identical results; completing task with bounded fallback",
		"step":                 step,
		"turn":                 turn,
		"tool_calls":           toolCalls,
		"mode":                 mode,
		"result_streak":        resultStreak,
		"bounded_fallback":     "no_new_evidence",
		"last_repeated_action": lastAction,
		"fallback_reason":      reason,
	})
	return emitAssistCompletion(manager, cfg, task, assistantModel, "assist_no_new_evidence", summary, step, turn, toolCalls, map[string]any{
		"result_streak":        resultStreak,
		"bounded_fallback":     "no_new_evidence",
		"last_repeated_action": lastAction,
		"fallback_reason":      reason,
	})
}

func emitAssistSummaryFallback(
	manager *Manager,
	cfg WorkerRunConfig,
	task TaskSpec,
	assistantModel string,
	mode string,
	step int,
	turn int,
	toolCalls int,
	fallbackReason string,
) error {
	fallbackReason = strings.TrimSpace(fallbackReason)
	if fallbackReason == "" {
		fallbackReason = "summary recover fallback"
	}
	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
		"message":          "summary task recover fallback triggered; completing with available evidence",
		"step":             step,
		"turn":             turn,
		"tool_calls":       toolCalls,
		"mode":             mode,
		"bounded_fallback": "summary_autocomplete",
		"fallback_reason":  fallbackReason,
	})
	final := fmt.Sprintf("Completed summary synthesis from available run evidence with bounded recover fallback (%s).", fallbackReason)
	return emitAssistCompletion(manager, cfg, task, assistantModel, "assist_summary_autocomplete", final, step, turn, toolCalls, map[string]any{
		"bounded_fallback": "summary_autocomplete",
		"fallback_reason":  fallbackReason,
	})
}

func emitAssistAdaptiveBudgetFallback(
	manager *Manager,
	cfg WorkerRunConfig,
	task TaskSpec,
	assistantModel string,
	mode string,
	lastAction string,
	step int,
	turn int,
	toolCalls int,
	fallbackReason string,
) error {
	fallbackReason = strings.TrimSpace(fallbackReason)
	if fallbackReason == "" {
		fallbackReason = "adaptive replan budget cap"
	}
	lastAction = strings.TrimSpace(lastAction)
	if lastAction == "" {
		lastAction = "unknown"
	}
	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
		"message":              "adaptive replan budget cap reached; completing with bounded fallback",
		"step":                 step,
		"turn":                 turn,
		"tool_calls":           toolCalls,
		"mode":                 mode,
		"bounded_fallback":     "adaptive_replan_budget_cap",
		"last_repeated_action": lastAction,
		"fallback_reason":      fallbackReason,
	})
	final := fmt.Sprintf("Adaptive replan completed with bounded fallback after budget cap (%s). Last action: %s.", fallbackReason, lastAction)
	return emitAssistCompletion(manager, cfg, task, assistantModel, "assist_no_new_evidence", final, step, turn, toolCalls, map[string]any{
		"bounded_fallback":     "adaptive_replan_budget_cap",
		"last_repeated_action": lastAction,
		"fallback_reason":      fallbackReason,
	})
}
