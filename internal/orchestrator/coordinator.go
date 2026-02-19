package orchestrator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Coordinator struct {
	runID                       string
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

func (c *Coordinator) markFailedWithReplan(taskID, reason string, retryable bool, context map[string]any) error {
	if err := c.scheduler.MarkFailed(taskID, reason, retryable, c.maxAttempts); err != nil {
		return err
	}
	state, ok := c.scheduler.State(taskID)
	if !ok || state != TaskStateQueued {
		return nil
	}
	payload := map[string]any{
		"trigger":       "retry_scheduled",
		"task_id":       taskID,
		"reason":        "task retry scheduled after failure",
		"source_reason": reason,
		"outcome":       string(replanOutcomeForTrigger("retry_scheduled")),
	}
	for k, v := range context {
		payload[k] = v
	}
	return c.manager.EmitEvent(c.runID, orchestratorWorkerID, taskID, EventTypeRunReplanRequested, payload)
}

func (c *Coordinator) handleRunStateAndReplan() error {
	if err := c.manager.RefreshMemoryBank(c.runID); err != nil {
		return err
	}
	leases, err := c.manager.ReadLeases(c.runID)
	if err != nil {
		return err
	}
	blocked := make([]string, 0)
	for _, lease := range leases {
		if lease.Status == LeaseStatusBlocked {
			blocked = append(blocked, lease.TaskID)
		}
	}
	sort.Strings(blocked)

	artifactCount, findingCount, err := c.manager.CountEvidence(c.runID)
	if err != nil {
		return err
	}
	taskCounts := map[string]int{}
	for state, count := range c.scheduler.Summary() {
		taskCounts[string(state)] = count
	}
	snapshot := RunStateSnapshot{
		RunID:         c.runID,
		UpdatedAt:     c.manager.Now(),
		ActiveWorkers: c.workers.RunningCount(),
		TaskCounts:    taskCounts,
		ArtifactCount: artifactCount,
		FindingCount:  findingCount,
		BlockedTasks:  blocked,
	}
	if err := c.manager.WriteRunState(c.runID, snapshot); err != nil {
		return err
	}
	stateHash := runStateHash(snapshot)
	if stateHash != c.lastRunStateHash {
		if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, "", EventTypeRunStateUpdated, map[string]any{
			"state_hash":     stateHash,
			"active_workers": snapshot.ActiveWorkers,
			"task_counts":    snapshot.TaskCounts,
			"artifact_count": snapshot.ArtifactCount,
			"finding_count":  snapshot.FindingCount,
			"blocked_tasks":  snapshot.BlockedTasks,
		}); err != nil {
			return err
		}
		c.lastRunStateHash = stateHash
	}

	for _, taskID := range blocked {
		if _, seen := c.seenBlockedTasks[taskID]; seen {
			continue
		}
		if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, taskID, EventTypeRunReplanRequested, map[string]any{
			"trigger": "blocked_task",
			"task_id": taskID,
			"reason":  "task_blocked",
		}); err != nil {
			return err
		}
		c.seenBlockedTasks[taskID] = struct{}{}
	}

	findings, err := c.manager.ListFindings(c.runID)
	if err != nil {
		return err
	}
	for _, finding := range findings {
		key := finding.Metadata["dedupe_key"]
		if key == "" {
			key = FindingDedupeKey(finding)
		}
		if _, seen := c.seenFindingKeys[key]; seen {
			continue
		}
		if c.isAdaptiveReplanTask(finding.TaskID) {
			c.seenFindingKeys[key] = struct{}{}
			continue
		}
		if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, finding.TaskID, EventTypeRunReplanRequested, map[string]any{
			"trigger":     "new_finding",
			"finding_key": key,
			"target":      finding.Target,
			"title":       finding.Title,
			"severity":    finding.Severity,
			"confidence":  finding.Confidence,
		}); err != nil {
			return err
		}
		c.seenFindingKeys[key] = struct{}{}
	}

	if err := c.handleEventDrivenReplanTriggers(); err != nil {
		return err
	}

	return nil
}

func (c *Coordinator) isAdaptiveReplanTask(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	if task, ok := c.scheduler.Task(taskID); ok {
		return strings.HasPrefix(task.Strategy, "adaptive_replan_")
	}
	task, err := c.manager.ReadTask(c.runID, taskID)
	if err != nil {
		return false
	}
	return strings.HasPrefix(task.Strategy, "adaptive_replan_")
}

func (c *Coordinator) handleEventDrivenReplanTriggers() error {
	events, err := c.manager.Events(c.runID, 0)
	if err != nil {
		return err
	}
	missingArtifactsFailureCount := map[string]int{}
	for _, event := range events {
		if event.Type == EventTypeTaskFailed {
			payload := map[string]any{}
			if len(event.Payload) > 0 {
				_ = json.Unmarshal(event.Payload, &payload)
			}
			reason := toString(payload["reason"])
			if reason == "missing_required_artifacts" {
				missingArtifactsFailureCount[event.TaskID]++
				if missingArtifactsFailureCount[event.TaskID] >= c.maxAttempts {
					if _, seen := c.seenMissingArtifactTasks[event.TaskID]; !seen {
						if err := c.emitReplanForSourceEvent(event, "missing_required_artifacts_after_retries", map[string]any{
							"task_id":       event.TaskID,
							"failure_count": missingArtifactsFailureCount[event.TaskID],
							"reason":        "required artifacts missing after retries",
						}); err != nil {
							return err
						}
						c.seenMissingArtifactTasks[event.TaskID] = struct{}{}
					}
				}
			}
		}
	}
	for _, event := range events {
		if _, seen := c.seenReplanSourceEvents[event.EventID]; seen {
			continue
		}
		var trigger string
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		switch event.Type {
		case EventTypeTaskFailed:
			reason := toString(payload["reason"])
			switch reason {
			case "repeated_step_loop":
				trigger = "repeated_step_loop"
			case "stale_lease", "worker_exit", "startup_sla_missed", "worker_reconcile_stale":
				trigger = "worker_recovery"
			case WorkerFailureCommandFailed, WorkerFailureCommandTimeout, WorkerFailureAssistTimeout, WorkerFailureAssistUnavailable:
				trigger = "execution_failure"
			case "budget_exhausted":
				trigger = "budget_exhausted"
			}
		case EventTypeApprovalDenied:
			trigger = "approval_denied"
		case EventTypeApprovalExpired:
			trigger = "approval_expired"
		case EventTypeOperatorInstruction:
			trigger = "operator_instruction"
		}
		if trigger == "" {
			continue
		}
		enriched := map[string]any{
			"task_id": event.TaskID,
		}
		for k, v := range payload {
			enriched[k] = v
		}
		if err := c.emitReplanForSourceEvent(event, trigger, enriched); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) emitReplanForSourceEvent(source EventEnvelope, trigger string, payload map[string]any) error {
	if _, seen := c.seenReplanSourceEvents[source.EventID]; seen {
		return nil
	}
	if payload == nil {
		payload = map[string]any{}
	}
	mutationKey := c.replanMutationKey(trigger, source)
	payload["idempotency_key"] = mutationKey
	for k, v := range c.maybeMutateTaskGraph(trigger, source, payload, mutationKey) {
		payload[k] = v
	}
	payload["trigger"] = trigger
	payload["source_event_id"] = source.EventID
	if _, ok := payload["outcome"]; !ok {
		payload["outcome"] = string(replanOutcomeForTrigger(trigger))
	}
	if _, ok := payload["reason"]; !ok {
		payload["reason"] = trigger
	}
	if err := c.manager.EmitEvent(c.runID, orchestratorWorkerID, source.TaskID, EventTypeRunReplanRequested, payload); err != nil {
		return err
	}
	c.seenReplanSourceEvents[source.EventID] = struct{}{}
	return nil
}

func (c *Coordinator) maybeMutateTaskGraph(trigger string, source EventEnvelope, payload map[string]any, mutationKey string) map[string]any {
	out := map[string]any{
		"graph_mutation": false,
	}
	outcome := replanOutcomeForTrigger(trigger)
	if outcome == ReplanOutcomeTerminate {
		out["mutation_status"] = "terminated_by_policy"
		out["outcome"] = string(outcome)
		return out
	}
	if !c.shouldMutateForTrigger(trigger) {
		out["mutation_status"] = "no_mutation_policy"
		return out
	}
	if _, seen := c.seenReplanMutationKeys[mutationKey]; seen {
		out["mutation_status"] = "duplicate_ignored"
		return out
	}
	if c.replanMutationBudget > 0 && c.replanMutationCount >= c.replanMutationBudget {
		out["mutation_status"] = "replan_budget_exhausted"
		out["outcome"] = string(ReplanOutcomeTerminate)
		_ = c.manager.Stop(c.runID)
		return out
	}
	task, err := c.buildReplanTask(trigger, source, payload, mutationKey)
	if err != nil {
		out["mutation_status"] = "task_build_failed"
		out["mutation_error"] = err.Error()
		return out
	}
	if err := c.scheduler.AddTask(task); err != nil {
		out["mutation_status"] = "task_add_failed"
		out["mutation_error"] = err.Error()
		return out
	}
	if err := c.manager.AddTask(c.runID, task); err != nil {
		_ = c.scheduler.ForceState(task.TaskID, TaskStateCanceled)
		out["mutation_status"] = "task_persist_failed"
		out["mutation_error"] = err.Error()
		return out
	}
	now := c.manager.Now()
	lease := TaskLease{
		TaskID:    task.TaskID,
		LeaseID:   fmt.Sprintf("lease-%s-%d", task.TaskID, now.UnixNano()),
		WorkerID:  "",
		Status:    LeaseStatusQueued,
		Attempt:   1,
		StartedAt: now,
		Deadline:  now.Add(c.startupTimeout),
	}
	if err := c.manager.WriteLease(c.runID, lease); err != nil {
		_ = c.scheduler.ForceState(task.TaskID, TaskStateCanceled)
		out["mutation_status"] = "lease_write_failed"
		out["mutation_error"] = err.Error()
		return out
	}
	c.replanMutationCount++
	c.seenReplanMutationKeys[mutationKey] = struct{}{}
	out["graph_mutation"] = true
	out["mutation_status"] = "task_added"
	out["added_task_id"] = task.TaskID
	out["added_task_strategy"] = task.Strategy
	return out
}

type ReplanOutcome string

const (
	ReplanOutcomeRefineTask ReplanOutcome = "refine_task"
	ReplanOutcomeSplitTask  ReplanOutcome = "split_task"
	ReplanOutcomeTerminate  ReplanOutcome = "terminate_run"
)

func replanOutcomeForTrigger(trigger string) ReplanOutcome {
	switch trigger {
	case "missing_required_artifacts_after_retries":
		return ReplanOutcomeSplitTask
	case "approval_denied", "approval_expired":
		return ReplanOutcomeTerminate
	case "budget_exhausted":
		return ReplanOutcomeRefineTask
	default:
		return ReplanOutcomeRefineTask
	}
}

func (c *Coordinator) shouldMutateForTrigger(trigger string) bool {
	switch trigger {
	case "new_finding", "missing_required_artifacts_after_retries", "execution_failure", "worker_recovery", "repeated_step_loop", "budget_exhausted", "blocked_task", "operator_instruction":
		return true
	default:
		return false
	}
}

func (c *Coordinator) buildReplanTask(trigger string, source EventEnvelope, payload map[string]any, mutationKey string) (TaskSpec, error) {
	baseTask, hasBase := c.scheduler.Task(source.TaskID)
	targets := []string{}
	if hasBase {
		targets = append(targets, baseTask.Targets...)
	}
	if len(targets) == 0 {
		targets = append(targets, c.runScopeTargets()...)
	}
	taskID := fmt.Sprintf("task-rp-%s", hashKey(mutationKey)[:10])
	risk := string(RiskReconReadonly)
	if trigger == "new_finding" {
		severity := strings.ToLower(strings.TrimSpace(toString(payload["severity"])))
		if severity == "high" || severity == "critical" {
			risk = string(RiskActiveProbe)
		}
	}
	operatorInstruction := ""
	if trigger == "operator_instruction" {
		operatorInstruction = strings.TrimSpace(toString(payload["instruction"]))
	}
	budget := budgetForRiskLevel(risk)
	if budget.MaxRuntime > 10*time.Minute {
		budget.MaxRuntime = 10 * time.Minute
	}
	done := []string{"replan_task_completed"}
	fail := []string{"replan_task_failed"}
	if hasBase {
		done = append(done, fmt.Sprintf("source_task_%s_followup_complete", baseTask.TaskID))
	}
	evidenceTarget := source.TaskID
	if evidenceTarget == "" {
		evidenceTarget = "run"
	}
	goal := fmt.Sprintf("Investigate trigger %s from task %s and produce actionable follow-up evidence", trigger, evidenceTarget)
	action := echoAction(fmt.Sprintf("adaptive_replan:%s:%s", trigger, evidenceTarget))
	title := fmt.Sprintf("Adaptive replan for %s", trigger)
	expectedArtifacts := []string{fmt.Sprintf("replan-%s.log", sanitizePathComponent(evidenceTarget))}
	if operatorInstruction != "" {
		goal = operatorInstruction
		action = assistAction(operatorInstruction)
		title = "Operator instruction"
		expectedArtifacts = []string{fmt.Sprintf("operator-instruction-%s.log", hashKey(operatorInstruction)[:8])}
	}
	return TaskSpec{
		TaskID:            taskID,
		Title:             title,
		IdempotencyKey:    mutationKey,
		Goal:              goal,
		Targets:           targets,
		Priority:          95,
		Strategy:          "adaptive_replan_" + sanitizePathComponent(trigger),
		Action:            action,
		DoneWhen:          done,
		FailWhen:          fail,
		ExpectedArtifacts: expectedArtifacts,
		RiskLevel:         risk,
		Budget:            budget,
	}, nil
}

func (c *Coordinator) runScopeTargets() []string {
	plan, err := c.manager.LoadRunPlan(c.runID)
	if err != nil {
		return nil
	}
	targets := append([]string{}, plan.Scope.Targets...)
	targets = append(targets, plan.Scope.Networks...)
	return targets
}

func (c *Coordinator) replanMutationKey(trigger string, source EventEnvelope) string {
	keyBase := strings.Join([]string{c.runID, trigger, source.EventID, source.TaskID}, "|")
	return "rp:" + hashKey(keyBase)
}

func (c *Coordinator) ensureReplanBudget() {
	if c.replanMutationBudget > 0 {
		return
	}
	c.replanMutationBudget = 8
	plan, err := c.manager.LoadRunPlan(c.runID)
	if err != nil {
		return
	}
	if budget := parseReplanBudget(plan.StopCriteria); budget > 0 {
		c.replanMutationBudget = budget
	}
}

func parseReplanBudget(stopCriteria []string) int {
	for _, criterion := range stopCriteria {
		raw := strings.ToLower(strings.TrimSpace(criterion))
		for _, prefix := range []string{"max_replans=", "replan_budget="} {
			if !strings.HasPrefix(raw, prefix) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
			var n int
			if _, err := fmt.Sscanf(value, "%d", &n); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

func (c *Coordinator) restoreSeenReplanState() error {
	events, err := c.manager.Events(c.runID, 0)
	if err != nil {
		return err
	}
	for _, event := range events {
		switch event.Type {
		case EventTypeRunStateUpdated:
			payload := map[string]any{}
			if len(event.Payload) > 0 {
				_ = json.Unmarshal(event.Payload, &payload)
			}
			hash := toString(payload["state_hash"])
			if hash != "" {
				c.lastRunStateHash = hash
			}
		case EventTypeRunReplanRequested:
			payload := map[string]any{}
			if len(event.Payload) > 0 {
				_ = json.Unmarshal(event.Payload, &payload)
			}
			if key := toString(payload["idempotency_key"]); key != "" {
				c.seenReplanMutationKeys[key] = struct{}{}
			}
			if mutated, ok := payload["graph_mutation"].(bool); ok && mutated {
				c.replanMutationCount++
			}
			sourceEventID := toString(payload["source_event_id"])
			if sourceEventID != "" {
				c.seenReplanSourceEvents[sourceEventID] = struct{}{}
			}
			switch toString(payload["trigger"]) {
			case "blocked_task":
				taskID := toString(payload["task_id"])
				if taskID == "" {
					taskID = event.TaskID
				}
				if taskID != "" {
					c.seenBlockedTasks[taskID] = struct{}{}
				}
			case "new_finding":
				key := toString(payload["finding_key"])
				if key != "" {
					c.seenFindingKeys[key] = struct{}{}
				}
			case "missing_required_artifacts_after_retries":
				taskID := toString(payload["task_id"])
				if taskID == "" {
					taskID = event.TaskID
				}
				if taskID != "" {
					c.seenMissingArtifactTasks[taskID] = struct{}{}
				}
			}
		}
	}
	return nil
}

func approvalPauseDuration(events []EventEnvelope, taskID string, startedAt, now time.Time) time.Duration {
	var total time.Duration
	var pauseStart time.Time
	paused := false
	for _, event := range events {
		if event.TaskID != taskID || event.TS.Before(startedAt) {
			continue
		}
		switch event.Type {
		case EventTypeApprovalRequested:
			if !paused {
				paused = true
				pauseStart = event.TS
			}
		case EventTypeApprovalGranted, EventTypeApprovalDenied, EventTypeApprovalExpired:
			if paused {
				total += event.TS.Sub(pauseStart)
				paused = false
			}
		}
	}
	if paused {
		total += now.Sub(pauseStart)
	}
	return total
}

func latestProgressCounters(events []EventEnvelope, taskID string, notBefore time.Time) (steps int, toolCalls int) {
	for _, event := range events {
		if event.Type != EventTypeTaskProgress || event.TaskID != taskID || event.TS.Before(notBefore) {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if v, ok := payloadInt(payload["steps"]); ok && v > steps {
			steps = v
		}
		if v, ok := payloadInt(payload["step"]); ok && v > steps {
			steps = v
		}
		if v, ok := payloadInt(payload["tool_calls"]); ok && v > toolCalls {
			toolCalls = v
		}
		if v, ok := payloadInt(payload["tool_call"]); ok && v > toolCalls {
			toolCalls = v
		}
	}
	return steps, toolCalls
}

func payloadInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

func latestWorkerFailureReason(events []EventEnvelope, taskID, workerID string) (string, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != EventTypeTaskFailed || event.TaskID != taskID || event.WorkerID != workerID {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		reason := toString(payload["reason"])
		if reason == "" {
			reason = "worker_exit"
		}
		return reason, true
	}
	return "", false
}

func hasWorkerTaskCompleted(events []EventEnvelope, taskID, workerID string) bool {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.TaskID == taskID && event.WorkerID == workerID && event.Type == EventTypeTaskCompleted {
			return true
		}
	}
	return false
}

func retryableWorkerFailureReason(reason string) bool {
	switch reason {
	case WorkerFailureScopeDenied, WorkerFailurePolicyDenied, WorkerFailurePolicyInvalid, WorkerFailureInvalidTaskAction, WorkerFailureCommandInterrupted:
		return false
	default:
		return true
	}
}
