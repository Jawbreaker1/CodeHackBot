package orchestrator

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

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
	phase := NormalizeRunPhase(c.runPhase)
	snapshot := RunStateSnapshot{
		RunID:         c.runID,
		UpdatedAt:     c.manager.Now(),
		Phase:         phase,
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
			"phase":          snapshot.Phase,
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
	for idx, event := range events {
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
			case WorkerFailureCommandFailed, WorkerFailureCommandTimeout, WorkerFailureAssistTimeout, WorkerFailureAssistUnavailable, WorkerFailureAssistParseFailure, WorkerFailureNoProgress:
				trigger = "execution_failure"
			case WorkerFailureAssistLoopDetected:
				if c.shouldPromoteAssistLoopExecutionFailure(event.TaskID, event.EventID, events, idx) {
					trigger = "execution_failure"
				}
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
	if c.shouldRewireAssistLoopSeedDependencies(trigger, source) {
		rewiredTaskIDs, err := c.rewireDependentTasksForRecovery(source.TaskID, task.TaskID)
		if err != nil {
			out["dependency_rewire_status"] = "failed"
			out["dependency_rewire_error"] = err.Error()
		} else if len(rewiredTaskIDs) > 0 {
			out["dependency_rewire_status"] = "updated"
			out["dependency_rewire_source_task"] = source.TaskID
			out["dependency_rewire_task_ids"] = rewiredTaskIDs
		}
	}
	return out
}

func (c *Coordinator) rewireDependentTasksForRecovery(sourceTaskID, replacementTaskID string) ([]string, error) {
	sourceTaskID = strings.TrimSpace(sourceTaskID)
	replacementTaskID = strings.TrimSpace(replacementTaskID)
	if sourceTaskID == "" || replacementTaskID == "" || sourceTaskID == replacementTaskID {
		return nil, nil
	}
	plan, err := c.manager.LoadRunPlan(c.runID)
	if err != nil {
		return nil, err
	}
	rewired := make([]string, 0)
	updatedTasks := map[string]TaskSpec{}
	for idx := range plan.Tasks {
		task := plan.Tasks[idx]
		if task.TaskID == sourceTaskID || task.TaskID == replacementTaskID {
			continue
		}
		if !taskDependsOn(task, sourceTaskID) {
			continue
		}
		deps := make([]string, 0, len(task.DependsOn)+1)
		hasReplacement := false
		for _, dep := range task.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" || dep == sourceTaskID {
				continue
			}
			if dep == replacementTaskID {
				hasReplacement = true
			}
			deps = append(deps, dep)
		}
		if !hasReplacement {
			deps = append(deps, replacementTaskID)
		}
		task.DependsOn = deps
		plan.Tasks[idx] = task
		updatedTasks[task.TaskID] = task
		rewired = append(rewired, task.TaskID)
	}
	if len(rewired) == 0 {
		return nil, nil
	}
	if err := ValidateRunPlan(plan); err != nil {
		return nil, err
	}
	taskGraph := make(map[string]TaskSpec, len(c.scheduler.tasks))
	for taskID, task := range c.scheduler.tasks {
		if updated, ok := updatedTasks[taskID]; ok {
			taskGraph[taskID] = updated
			continue
		}
		taskGraph[taskID] = task
	}
	if err := validateAcyclic(taskGraph); err != nil {
		return nil, err
	}
	planPath := filepath.Join(BuildRunPaths(c.manager.SessionsDir, c.runID).PlanDir, "plan.json")
	if err := WriteJSONAtomic(planPath, plan); err != nil {
		return nil, err
	}
	for taskID, task := range updatedTasks {
		if err := c.manager.WriteTask(c.runID, task); err != nil {
			return nil, err
		}
		c.scheduler.tasks[taskID] = task
	}
	sort.Strings(rewired)
	return rewired, nil
}

func taskDependsOn(task TaskSpec, dep string) bool {
	dep = strings.TrimSpace(dep)
	if dep == "" {
		return false
	}
	for _, current := range task.DependsOn {
		if strings.TrimSpace(current) == dep {
			return true
		}
	}
	return false
}
