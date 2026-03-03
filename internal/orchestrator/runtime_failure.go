package orchestrator

func emitWorkerFailure(manager *Manager, cfg WorkerRunConfig, task TaskSpec, cause error, reason string, extra map[string]any) error {
	reason = CanonicalTaskFailureReason(reason)
	if reason == "" {
		reason = WorkerFailureCommandFailed
	}
	failureClass := classifyWorkerFailureReason(reason)
	payload := map[string]any{
		"attempt":       cfg.Attempt,
		"worker_id":     cfg.WorkerID,
		"reason":        reason,
		"failure_class": failureClass,
	}
	if cause != nil {
		payload["error"] = cause.Error()
	}
	for key, value := range extra {
		payload[key] = value
	}
	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFinding, map[string]any{
		"target":       primaryTaskTarget(task),
		"finding_type": "task_execution_failure",
		"title":        "task action failed",
		"state":        FindingStateVerified,
		"severity":     "low",
		"confidence":   "high",
		"source":       "worker_runtime",
		"metadata": map[string]any{
			"attempt":       cfg.Attempt,
			"reason":        reason,
			"failure_class": failureClass,
		},
	})
	return manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFailed, payload)
}

const (
	workerFailureClassContextLoss = "context_loss"
	workerFailureClassStrategy    = "strategy_failure"
	workerFailureClassContract    = "contract_failure"
)

func classifyWorkerFailureReason(reason string) string {
	reason = CanonicalTaskFailureReason(reason)
	switch reason {
	case WorkerFailureInvalidTaskAction,
		WorkerFailureScopeDenied,
		WorkerFailurePolicyDenied,
		WorkerFailurePolicyInvalid,
		WorkerFailureInvalidWorkingDir:
		return workerFailureClassContract
	case WorkerFailureAssistNoAction,
		WorkerFailureAssistLoopDetected,
		WorkerFailureAssistExhausted,
		WorkerFailureNoProgress:
		return workerFailureClassContextLoss
	default:
		return workerFailureClassStrategy
	}
}

func runErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
