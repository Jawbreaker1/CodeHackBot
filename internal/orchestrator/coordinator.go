package orchestrator

import (
	"encoding/json"
	"fmt"
	"time"
)

type Coordinator struct {
	runID                       string
	runPhase                    string
	manager                     *Manager
	workers                     *WorkerManager
	scheduler                   *Scheduler
	maxAttempts                 int
	staleTimeout                time.Duration
	softStallGrace              time.Duration
	startupTimeout              time.Duration
	specForTask                 func(TaskSpec, int, string) WorkerSpec
	workerToTask                map[string]string
	lastStallNote               map[string]time.Time
	broker                      *ApprovalBroker
	scopePolicy                 *ScopePolicy
	seenApprovalDecisionEvents  map[string]struct{}
	seenWorkerStopRequestEvents map[string]struct{}
	seenBlockedTasks            map[string]struct{}
	seenFindingKeys             map[string]struct{}
	seenReplanSourceEvents      map[string]struct{}
	seenReplanMutationKeys      map[string]struct{}
	seenMissingArtifactTasks    map[string]struct{}
	lastRunStateHash            string
	replanMutationCount         int
	replanMutationBudget        int
}

func NewCoordinator(runID string, scope Scope, manager *Manager, workers *WorkerManager, scheduler *Scheduler, maxAttempts int, startupTimeout, staleTimeout, softStallGrace time.Duration, specForTask func(TaskSpec, int, string) WorkerSpec, broker *ApprovalBroker) (*Coordinator, error) {
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	if manager == nil {
		return nil, fmt.Errorf("manager is required")
	}
	if workers == nil {
		return nil, fmt.Errorf("worker manager is required")
	}
	if scheduler == nil {
		return nil, fmt.Errorf("scheduler is required")
	}
	if maxAttempts <= 0 {
		return nil, fmt.Errorf("max attempts must be > 0")
	}
	if startupTimeout <= 0 {
		return nil, fmt.Errorf("startup timeout must be > 0")
	}
	if staleTimeout <= 0 {
		return nil, fmt.Errorf("stale timeout must be > 0")
	}
	if softStallGrace <= 0 {
		return nil, fmt.Errorf("soft stall grace must be > 0")
	}
	if specForTask == nil {
		return nil, fmt.Errorf("spec builder is required")
	}
	return &Coordinator{
		runID:                       runID,
		runPhase:                    RunPhaseExecuting,
		manager:                     manager,
		workers:                     workers,
		scheduler:                   scheduler,
		maxAttempts:                 maxAttempts,
		staleTimeout:                staleTimeout,
		softStallGrace:              softStallGrace,
		startupTimeout:              startupTimeout,
		specForTask:                 specForTask,
		workerToTask:                map[string]string{},
		lastStallNote:               map[string]time.Time{},
		broker:                      broker,
		scopePolicy:                 NewScopePolicy(scope),
		seenApprovalDecisionEvents:  map[string]struct{}{},
		seenWorkerStopRequestEvents: map[string]struct{}{},
		seenBlockedTasks:            map[string]struct{}{},
		seenFindingKeys:             map[string]struct{}{},
		seenReplanSourceEvents:      map[string]struct{}{},
		seenReplanMutationKeys:      map[string]struct{}{},
		seenMissingArtifactTasks:    map[string]struct{}{},
	}, nil
}

func (c *Coordinator) SetRunPhase(phase string) {
	if normalized := NormalizeRunPhase(phase); normalized != "" {
		c.runPhase = normalized
	}
}

func (c *Coordinator) Tick() error {
	c.ensureReplanBudget()
	if _, err := c.manager.IngestEvidence(c.runID); err != nil {
		return err
	}
	if err := c.handleWorkerExits(); err != nil {
		return err
	}
	if err := c.handleStartupReclaims(); err != nil {
		return err
	}
	if err := c.handleStaleReclaims(); err != nil {
		return err
	}
	if err := c.handleSoftStall(); err != nil {
		return err
	}
	if err := c.handleApprovals(); err != nil {
		return err
	}
	if err := c.handleWorkerStopRequests(); err != nil {
		return err
	}
	if err := c.handleExecutionTimeouts(); err != nil {
		return err
	}
	if err := c.handleBudgetGuards(); err != nil {
		return err
	}
	if err := c.handleRunStateAndReplan(); err != nil {
		return err
	}

	ready := c.scheduler.NextLeasable()
	for _, task := range ready {
		if err := c.dispatchTask(task); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) Done() bool {
	return c.scheduler.IsDone()
}

func (c *Coordinator) Reconcile() error {
	leases, err := c.manager.ReadLeases(c.runID)
	if err != nil {
		return err
	}
	if err := c.restoreSeenReplanState(); err != nil {
		return err
	}
	c.workerToTask = map[string]string{}
	for _, lease := range leases {
		state := TaskStateQueued
		switch lease.Status {
		case LeaseStatusQueued:
			state = TaskStateQueued
		case LeaseStatusAwaitingApproval:
			state = TaskStateAwaitingApproval
		case LeaseStatusLeased:
			state = TaskStateLeased
		case LeaseStatusRunning:
			if lease.WorkerID != "" && c.workers.IsRunning(c.runID, lease.WorkerID) {
				state = TaskStateRunning
				c.workerToTask[lease.WorkerID] = lease.TaskID
			} else {
				// stale running lease on restart is treated as retryable failure.
				if err := c.markFailedWithReplan(lease.TaskID, "worker_reconcile_stale", true, map[string]any{
					"reason":    "worker_reconcile_stale",
					"worker_id": lease.WorkerID,
				}); err != nil {
					return err
				}
				_ = c.manager.UpdateLeaseStatus(c.runID, lease.TaskID, LeaseStatusQueued, "")
				continue
			}
		case LeaseStatusCompleted:
			state = TaskStateCompleted
		case LeaseStatusFailed:
			state = TaskStateFailed
		case LeaseStatusBlocked:
			state = TaskStateBlocked
		case LeaseStatusCanceled:
			state = TaskStateCanceled
		default:
			state = TaskStateQueued
		}
		if err := c.scheduler.ForceState(lease.TaskID, state); err != nil {
			return err
		}
		if lease.Attempt > 0 {
			if err := c.scheduler.SetAttempt(lease.TaskID, lease.Attempt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Coordinator) StopAll(grace time.Duration) error {
	for workerID := range c.workerToTask {
		if err := c.workers.Stop(c.runID, workerID, grace); err != nil {
			return err
		}
	}
	c.scheduler.StopAll()
	return nil
}

func (c *Coordinator) handleWorkerExits() error {
	events, err := c.manager.Events(c.runID, 0)
	if err != nil {
		return err
	}
	exits := c.workers.DrainCompleted(c.runID)
	for _, exit := range exits {
		taskID := c.workerToTask[exit.WorkerID]
		if taskID == "" {
			continue
		}
		delete(c.workerToTask, exit.WorkerID)

		if exit.Failed {
			reason := "worker_exit"
			retryable := true
			if workerReason, ok := latestWorkerFailureReason(events, taskID, WorkerSignalID(exit.WorkerID)); ok {
				reason = workerReason
				retryable = retryableWorkerFailureReason(workerReason)
				if retryable && workerReason == WorkerFailureAssistLoopDetected {
					taskSpec, hasTask := c.scheduler.Task(taskID)
					if !hasTask {
						if loaded, readErr := c.manager.ReadTask(c.runID, taskID); readErr == nil {
							taskSpec = loaded
							hasTask = true
						}
					}
					if !hasTask || !allowAssistLoopRetry(taskSpec) {
						retryable = false
					}
				}
				if retryable && hasRepeatedTaskFailureReason(events, taskID, workerReason) {
					retryable = false
				}
			} else {
				if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, taskID, EventTypeTaskFailed, map[string]any{
					"reason":    reason,
					"error":     exit.ErrorMsg,
					"attempts":  exit.Attempts,
					"worker_id": exit.WorkerID,
					"log_path":  exit.LogPath,
				}); err != nil {
					return err
				}
			}
			if err := c.markFailedWithReplan(taskID, reason, retryable, map[string]any{
				"reason":    reason,
				"worker_id": exit.WorkerID,
				"log_path":  exit.LogPath,
			}); err != nil {
				return err
			}
			if st, ok := c.scheduler.State(taskID); ok {
				leaseStatus := LeaseStatusFailed
				if st == TaskStateQueued {
					leaseStatus = LeaseStatusQueued
				}
				_ = c.manager.UpdateLeaseStatus(c.runID, taskID, leaseStatus, "")
			}
			continue
		}

		if !hasWorkerTaskCompleted(events, taskID, WorkerSignalID(exit.WorkerID)) {
			if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, taskID, EventTypeTaskCompleted, map[string]any{
				"attempts":  exit.Attempts,
				"worker_id": exit.WorkerID,
			}); err != nil {
				return err
			}
		}
		signalWorkerID := WorkerSignalID(exit.WorkerID)
		completionPayload, hasCompletionPayload := latestWorkerTaskCompletedPayload(events, taskID, signalWorkerID)
		if hasCompletionPayload {
			taskSpec, ok := c.scheduler.Task(taskID)
			if !ok {
				var readErr error
				taskSpec, readErr = c.manager.ReadTask(c.runID, taskID)
				if readErr != nil {
					return readErr
				}
			}
			check, err := c.validateTaskCompletionContract(taskSpec, taskID, signalWorkerID, events, completionPayload)
			if err != nil {
				return err
			}
			if check.Status != "satisfied" {
				failurePayload := map[string]any{
					"reason":              "missing_required_artifacts",
					"error":               "completion contract verification failed",
					"worker_id":           exit.WorkerID,
					"log_path":            exit.LogPath,
					"required_artifacts":  check.RequiredArtifacts,
					"produced_artifacts":  check.ProducedArtifacts,
					"verified_artifacts":  check.VerifiedArtifacts,
					"missing_artifacts":   check.MissingArtifacts,
					"required_findings":   check.RequiredFindings,
					"produced_findings":   check.ProducedFindings,
					"missing_findings":    check.MissingFindings,
					"verification_status": check.Status,
				}
				if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, taskID, EventTypeTaskFailed, failurePayload); err != nil {
					return err
				}
				if err := c.markFailedWithReplan(taskID, "missing_required_artifacts", true, failurePayload); err != nil {
					return err
				}
				if st, ok := c.scheduler.State(taskID); ok {
					leaseStatus := LeaseStatusFailed
					if st == TaskStateQueued {
						leaseStatus = LeaseStatusQueued
					}
					_ = c.manager.UpdateLeaseStatus(c.runID, taskID, leaseStatus, "")
				}
				continue
			}
		}
		if err := c.scheduler.MarkCompleted(taskID); err != nil {
			return err
		}
		_ = c.manager.UpdateLeaseStatus(c.runID, taskID, LeaseStatusCompleted, "")
	}
	return nil
}

func (c *Coordinator) handleStartupReclaims() error {
	reclaimed, err := c.manager.ReclaimMissedStartup(c.runID, c.startupTimeout)
	if err != nil {
		return err
	}
	for _, lease := range reclaimed {
		if err := c.markFailedWithReplan(lease.TaskID, "startup_sla_missed", true, map[string]any{
			"reason": "startup_sla_missed",
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) handleStaleReclaims() error {
	reclaimed, err := c.manager.ReclaimStaleLeases(c.runID, c.staleTimeout, func(workerID string) bool {
		return c.workers.IsRunning(c.runID, workerID)
	})
	if err != nil {
		return err
	}
	for _, lease := range reclaimed {
		if err := c.markFailedWithReplan(lease.TaskID, "stale_lease", true, map[string]any{
			"reason": "stale_lease",
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) handleSoftStall() error {
	leases, err := c.manager.ReadLeases(c.runID)
	if err != nil {
		return err
	}
	events, err := c.manager.Events(c.runID, 0)
	if err != nil {
		return err
	}
	now := c.manager.Now()
	for _, lease := range leases {
		if lease.Status != LeaseStatusRunning {
			continue
		}
		if !c.workers.IsRunning(c.runID, lease.WorkerID) {
			continue
		}
		lastSignal, ok := latestTaskSignal(events, lease.TaskID, lease.StartedAt)
		if !ok {
			lastSignal = lease.StartedAt
		}
		if now.Sub(lastSignal) < c.softStallGrace {
			continue
		}
		if prev, ok := c.lastStallNote[lease.TaskID]; ok && now.Sub(prev) < c.softStallGrace {
			continue
		}
		c.lastStallNote[lease.TaskID] = now
		if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, lease.TaskID, EventTypeTaskProgress, map[string]any{
			"message":       "soft stall grace exceeded; worker still running",
			"worker_id":     lease.WorkerID,
			"grace_seconds": int(c.softStallGrace.Seconds()),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) dispatchTask(task TaskSpec) error {
	if err := c.scheduler.MarkLeased(task.TaskID); err != nil {
		return err
	}
	attempt, _ := c.scheduler.Attempt(task.TaskID)
	workerID := fmt.Sprintf("worker-%s-a%d", task.TaskID, attempt)

	now := c.manager.Now()
	if err := c.scopePolicy.ValidateTaskTargets(task); err != nil {
		_ = c.manager.EmitEvent(c.runID, orchestratorWorkerID, task.TaskID, EventTypeTaskFailed, map[string]any{
			"reason":  "scope_denied",
			"error":   err.Error(),
			"attempt": attempt,
		})
		if err := c.markFailedWithReplan(task.TaskID, "scope_denied", false, map[string]any{
			"reason": "scope_denied",
		}); err != nil {
			return err
		}
		lease := TaskLease{
			TaskID:    task.TaskID,
			LeaseID:   fmt.Sprintf("lease-%s-%d", task.TaskID, now.UnixNano()),
			WorkerID:  "",
			Status:    LeaseStatusBlocked,
			Attempt:   attempt,
			StartedAt: now,
			Deadline:  now.Add(c.startupTimeout),
		}
		return c.manager.WriteLease(c.runID, lease)
	}

	if c.broker != nil {
		decision, reason, tier, err := c.broker.EvaluateTask(task, now)
		if err != nil {
			return err
		}
		switch decision {
		case ApprovalDeny:
			_ = c.manager.EmitEvent(c.runID, orchestratorWorkerID, task.TaskID, EventTypeApprovalDenied, map[string]any{
				"reason": reason,
				"tier":   string(tier),
				"actor":  "policy",
			})
			if err := c.markFailedWithReplan(task.TaskID, "scope_denied", false, map[string]any{
				"reason": "scope_denied",
			}); err != nil {
				return err
			}
			return c.manager.UpdateLeaseStatus(c.runID, task.TaskID, LeaseStatusBlocked, "")
		case ApprovalNeedsRequest:
			lease := TaskLease{
				TaskID:    task.TaskID,
				LeaseID:   fmt.Sprintf("lease-%s-%d", task.TaskID, now.UnixNano()),
				WorkerID:  "",
				Status:    LeaseStatusAwaitingApproval,
				Attempt:   attempt,
				StartedAt: now,
				Deadline:  now.Add(c.startupTimeout),
			}
			if err := c.manager.WriteLease(c.runID, lease); err != nil {
				return err
			}
			if err := c.scheduler.MarkAwaitingApproval(task.TaskID); err != nil {
				return err
			}
			req := c.broker.EnsureRequest(c.runID, task, tier, reason, now)
			return c.manager.EmitEvent(c.runID, orchestratorWorkerID, task.TaskID, EventTypeApprovalRequested, map[string]any{
				"approval_id": req.ID,
				"tier":        string(req.RiskTier),
				"reason":      req.Reason,
				"expires_at":  req.ExpiresAt,
			})
		}
	}

	lease := TaskLease{
		TaskID:    task.TaskID,
		LeaseID:   fmt.Sprintf("lease-%s-%d", task.TaskID, now.UnixNano()),
		WorkerID:  workerID,
		Status:    LeaseStatusLeased,
		Attempt:   attempt,
		StartedAt: now,
		Deadline:  now.Add(c.startupTimeout),
	}
	if err := c.manager.WriteLease(c.runID, lease); err != nil {
		return err
	}

	spec := c.specForTask(task, attempt, workerID)
	spec.WorkerID = workerID
	if err := c.workers.Launch(c.runID, spec); err != nil {
		_ = c.manager.EmitEvent(c.runID, orchestratorWorkerID, task.TaskID, EventTypeTaskFailed, map[string]any{
			"reason":    "launch_failed",
			"error":     err.Error(),
			"attempt":   attempt,
			"worker_id": workerID,
		})
		return c.markFailedWithReplan(task.TaskID, "launch_failed", true, map[string]any{
			"reason":    "launch_failed",
			"worker_id": workerID,
		})
	}
	if err := c.scheduler.MarkRunning(task.TaskID); err != nil {
		return err
	}
	_ = c.manager.UpdateLeaseStatus(c.runID, task.TaskID, LeaseStatusRunning, workerID)
	c.workerToTask[workerID] = task.TaskID

	return c.manager.EmitEvent(c.runID, orchestratorWorkerID, task.TaskID, EventTypeTaskStarted, map[string]any{
		"attempt":   attempt,
		"worker_id": workerID,
	})
}

func (c *Coordinator) handleApprovals() error {
	if c.broker == nil {
		return nil
	}
	if err := c.ingestExternalApprovalDecisions(); err != nil {
		return err
	}
	now := c.manager.Now()
	c.broker.Expire(now)
	for _, req := range c.broker.DrainExpired() {
		if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, req.TaskID, EventTypeApprovalExpired, map[string]any{
			"approval_id": req.ID,
			"tier":        string(req.RiskTier),
			"reason":      "approval_timeout",
		}); err != nil {
			return err
		}
		if err := c.markFailedWithReplan(req.TaskID, "approval_timeout", false, map[string]any{
			"reason": "approval_timeout",
		}); err != nil {
			return err
		}
		_ = c.manager.UpdateLeaseStatus(c.runID, req.TaskID, LeaseStatusBlocked, "")
	}
	for _, req := range c.broker.DrainDenied() {
		if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, req.TaskID, EventTypeApprovalDenied, map[string]any{
			"approval_id": req.ID,
			"tier":        string(req.RiskTier),
			"actor":       req.Actor,
			"reason":      req.Note,
		}); err != nil {
			return err
		}
		if err := c.markFailedWithReplan(req.TaskID, "approval_denied", false, map[string]any{
			"reason": "approval_denied",
		}); err != nil {
			return err
		}
		_ = c.manager.UpdateLeaseStatus(c.runID, req.TaskID, LeaseStatusBlocked, "")
	}
	for _, req := range c.broker.DrainGranted() {
		if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, req.TaskID, EventTypeApprovalGranted, map[string]any{
			"approval_id": req.ID,
			"tier":        string(req.RiskTier),
			"actor":       req.Actor,
			"reason":      req.Note,
		}); err != nil {
			return err
		}
		if err := c.scheduler.MarkApprovalResumed(req.TaskID); err != nil {
			return err
		}
		_ = c.manager.UpdateLeaseStatus(c.runID, req.TaskID, LeaseStatusQueued, "")
	}
	return nil
}

func (c *Coordinator) ingestExternalApprovalDecisions() error {
	events, err := c.manager.Events(c.runID, 0)
	if err != nil {
		return err
	}
	now := c.manager.Now()
	for _, event := range events {
		if event.Type != EventTypeApprovalGranted && event.Type != EventTypeApprovalDenied {
			continue
		}
		if _, seen := c.seenApprovalDecisionEvents[event.EventID]; seen {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if toString(payload["source"]) != "cli" {
			continue
		}
		approvalID := toString(payload["approval_id"])
		if approvalID == "" {
			continue
		}
		actor := toString(payload["actor"])
		if actor == "" {
			actor = "cli"
		}
		reason := toString(payload["reason"])
		scope := ApprovalScope(toString(payload["scope"]))
		if scope == "" {
			scope = ApprovalScopeTask
		}
		switch event.Type {
		case EventTypeApprovalGranted:
			expiresIn := 0 * time.Second
			if raw, ok := payload["expires_in_seconds"].(float64); ok && raw > 0 {
				expiresIn = time.Duration(raw) * time.Second
			}
			_ = c.broker.Approve(approvalID, scope, actor, reason, now, expiresIn)
		case EventTypeApprovalDenied:
			_ = c.broker.Deny(approvalID, actor, reason)
		}
		c.seenApprovalDecisionEvents[event.EventID] = struct{}{}
	}
	return nil
}

func (c *Coordinator) handleWorkerStopRequests() error {
	events, err := c.manager.Events(c.runID, 0)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.Type != EventTypeWorkerStopRequested {
			continue
		}
		if _, seen := c.seenWorkerStopRequestEvents[event.EventID]; seen {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		workerID := toString(payload["target_worker_id"])
		if workerID == "" {
			workerID = toString(payload["worker_id"])
		}
		if workerID == "" {
			c.seenWorkerStopRequestEvents[event.EventID] = struct{}{}
			continue
		}
		if !c.workers.IsRunning(c.runID, workerID) {
			c.seenWorkerStopRequestEvents[event.EventID] = struct{}{}
			continue
		}
		taskID := c.workerToTask[workerID]
		if err := c.workers.Stop(c.runID, workerID, 2*time.Second); err != nil {
			return err
		}
		if taskID != "" {
			if err := c.scheduler.MarkCanceled(taskID); err == nil {
				_ = c.manager.UpdateLeaseStatus(c.runID, taskID, LeaseStatusCanceled, "")
			}
			delete(c.workerToTask, workerID)
			_ = c.manager.EmitEvent(c.runID, orchestratorWorkerID, taskID, EventTypeTaskProgress, map[string]any{
				"message":   "worker stop requested by operator",
				"worker_id": workerID,
			})
		}
		c.seenWorkerStopRequestEvents[event.EventID] = struct{}{}
	}
	return nil
}

func (c *Coordinator) handleExecutionTimeouts() error {
	leases, err := c.manager.ReadLeases(c.runID)
	if err != nil {
		return err
	}
	events, err := c.manager.Events(c.runID, 0)
	if err != nil {
		return err
	}
	now := c.manager.Now()
	for _, lease := range leases {
		if lease.Status != LeaseStatusRunning && lease.Status != LeaseStatusAwaitingApproval {
			continue
		}
		task, ok := c.scheduler.Task(lease.TaskID)
		if !ok || task.Budget.MaxRuntime <= 0 {
			continue
		}
		paused := approvalPauseDuration(events, lease.TaskID, lease.StartedAt, now)
		elapsed := now.Sub(lease.StartedAt) - paused
		if elapsed < 0 {
			elapsed = 0
		}
		if lease.Status == LeaseStatusAwaitingApproval {
			// Execution timeout is paused while waiting for approvals.
			continue
		}
		if elapsed <= task.Budget.MaxRuntime {
			continue
		}

		if lease.WorkerID != "" && c.workers.IsRunning(c.runID, lease.WorkerID) {
			_ = c.workers.Stop(c.runID, lease.WorkerID, 2*time.Second)
		}
		delete(c.workerToTask, lease.WorkerID)
		_ = c.manager.EmitEvent(c.runID, orchestratorWorkerID, lease.TaskID, EventTypeTaskFailed, map[string]any{
			"reason":          "execution_timeout",
			"elapsed_seconds": int(elapsed.Seconds()),
			"budget_seconds":  int(task.Budget.MaxRuntime.Seconds()),
			"worker_id":       lease.WorkerID,
		})
		if err := c.markFailedWithReplan(lease.TaskID, "execution_timeout", false, map[string]any{
			"reason":          "execution_timeout",
			"elapsed_seconds": int(elapsed.Seconds()),
		}); err != nil {
			return err
		}
		_ = c.manager.UpdateLeaseStatus(c.runID, lease.TaskID, LeaseStatusFailed, "")
	}
	return nil
}

func (c *Coordinator) handleBudgetGuards() error {
	leases, err := c.manager.ReadLeases(c.runID)
	if err != nil {
		return err
	}
	events, err := c.manager.Events(c.runID, 0)
	if err != nil {
		return err
	}
	for _, lease := range leases {
		if lease.Status != LeaseStatusRunning {
			continue
		}
		task, ok := c.scheduler.Task(lease.TaskID)
		if !ok {
			continue
		}
		steps, toolCalls := latestProgressCounters(events, lease.TaskID, lease.StartedAt)

		exceeded := false
		dimension := ""
		limit := 0
		value := 0
		if task.Budget.MaxSteps > 0 && steps > task.Budget.MaxSteps {
			exceeded = true
			dimension = "steps"
			limit = task.Budget.MaxSteps
			value = steps
		}
		if !exceeded && task.Budget.MaxToolCalls > 0 && toolCalls > task.Budget.MaxToolCalls {
			exceeded = true
			dimension = "tool_calls"
			limit = task.Budget.MaxToolCalls
			value = toolCalls
		}
		if !exceeded {
			continue
		}

		if lease.WorkerID != "" && c.workers.IsRunning(c.runID, lease.WorkerID) {
			_ = c.workers.Stop(c.runID, lease.WorkerID, 2*time.Second)
		}
		delete(c.workerToTask, lease.WorkerID)
		_ = c.manager.EmitEvent(c.runID, orchestratorWorkerID, lease.TaskID, EventTypeTaskFailed, map[string]any{
			"reason":      "budget_exhausted",
			"dimension":   dimension,
			"limit":       limit,
			"value":       value,
			"worker_id":   lease.WorkerID,
			"budget_type": dimension,
		})
		if err := c.markFailedWithReplan(lease.TaskID, "budget_exhausted", false, map[string]any{
			"reason":    "budget_exhausted",
			"dimension": dimension,
			"limit":     limit,
			"value":     value,
		}); err != nil {
			return err
		}
		_ = c.manager.UpdateLeaseStatus(c.runID, lease.TaskID, LeaseStatusFailed, "")
	}
	return nil
}
