package orchestrator

import (
	"fmt"
	"strings"
	"time"
)

func enforceWorkerRiskPolicy(task TaskSpec, cfg WorkerRunConfig) (string, error) {
	broker, err := NewApprovalBroker(cfg.Permission, cfg.Disruptive, time.Hour)
	if err != nil {
		return WorkerFailurePolicyInvalid, fmt.Errorf("%s: %w", WorkerFailurePolicyInvalid, err)
	}
	decision, _, tier, err := broker.EvaluateTask(task, time.Now().UTC())
	if err != nil {
		return WorkerFailurePolicyInvalid, fmt.Errorf("%s: %w", WorkerFailurePolicyInvalid, err)
	}
	if decision == ApprovalDeny {
		return WorkerFailurePolicyDenied, fmt.Errorf("%s: mode=%s tier=%s", WorkerFailurePolicyDenied, cfg.Permission, tier)
	}
	return "", nil
}

func EmitWorkerBootstrapFailure(cfg WorkerRunConfig, cause error) error {
	if strings.TrimSpace(cfg.SessionsDir) == "" || strings.TrimSpace(cfg.RunID) == "" || strings.TrimSpace(cfg.TaskID) == "" || strings.TrimSpace(cfg.WorkerID) == "" {
		return nil
	}
	manager := NewManager(cfg.SessionsDir)
	recorded, err := workerFailureAlreadyRecorded(manager, cfg)
	if err == nil && recorded {
		return nil
	}
	payload := map[string]any{
		"attempt":       cfg.Attempt,
		"worker_id":     cfg.WorkerID,
		"reason":        WorkerFailureBootstrapFailed,
		"failure_class": classifyWorkerFailureReason(WorkerFailureBootstrapFailed),
	}
	if cause != nil {
		payload["error"] = cause.Error()
	}
	return manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFailed, payload)
}
