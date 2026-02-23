package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

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
	if trigger == "execution_failure" || trigger == "worker_recovery" || trigger == "budget_exhausted" || trigger == "repeated_step_loop" {
		goal = buildAdaptiveRecoveryGoal(trigger, evidenceTarget, payload)
		action = assistAction(goal)
		title = fmt.Sprintf("Adaptive recovery for %s", trigger)
		expectedArtifacts = []string{fmt.Sprintf("adaptive-recovery-%s.log", sanitizePathComponent(evidenceTarget))}
	}
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

func buildAdaptiveRecoveryGoal(trigger, evidenceTarget string, payload map[string]any) string {
	base := fmt.Sprintf("Recover from %s on task %s and continue the run with actionable evidence.", trigger, evidenceTarget)
	reason := strings.TrimSpace(toString(payload["reason"]))
	errText := strings.TrimSpace(toString(payload["error"]))
	cmd := strings.TrimSpace(toString(payload["command"]))
	args := payloadStringSlice(payload["args"])
	logPath := strings.TrimSpace(toString(payload["log_path"]))
	if cmd != "" {
		fullCmd := strings.TrimSpace(strings.Join(append([]string{cmd}, args...), " "))
		if fullCmd != "" {
			base += " Failed command: " + fullCmd + "."
		}
	}
	if reason != "" {
		base += " Failure reason: " + reason + "."
	}
	if errText != "" {
		base += " Error: " + errText + "."
	}
	if logPath != "" {
		base += " Failure log: " + logPath + "."
	}
	base += " Use recent artifacts/logs first. Avoid re-running the same failing command unchanged."
	base += " If a broad CIDR scan timed out, switch to host discovery first, then split into targeted host/service follow-up scans."
	base += " Worker mode is non-interactive: do not ask the operator questions; infer best-effort inputs from context and continue."
	base += " End with a concise summary and clear next steps."
	return base
}

func payloadStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
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
	if trigger == "execution_failure" {
		if keyBase := c.executionFailureMutationKeyBase(source); keyBase != "" {
			return "rp:" + hashKey(keyBase)
		}
	}
	keyBase := strings.Join([]string{c.runID, trigger, source.EventID, source.TaskID}, "|")
	return "rp:" + hashKey(keyBase)
}

func (c *Coordinator) executionFailureMutationKeyBase(source EventEnvelope) string {
	payload := map[string]any{}
	if len(source.Payload) > 0 {
		_ = json.Unmarshal(source.Payload, &payload)
	}
	reason := strings.TrimSpace(toString(payload["reason"]))
	if reason != WorkerFailureCommandFailed {
		return ""
	}
	command := strings.TrimSpace(toString(payload["command"]))
	args := payloadStringSlice(payload["args"])
	if command == "" {
		return ""
	}
	fingerprintParts := []string{reason, strings.ToLower(command)}
	for _, arg := range args {
		fingerprintParts = append(fingerprintParts, strings.TrimSpace(arg))
	}
	scopeKey := strings.TrimSpace(source.TaskID)
	if scopeKey == "" {
		scopeKey = "run"
	}
	if c.isAdaptiveReplanTask(source.TaskID) {
		scopeKey = "adaptive_replan_execution_failure"
	}
	return strings.Join([]string{
		c.runID,
		"execution_failure",
		scopeKey,
		hashKey(strings.Join(fingerprintParts, "\x1f")),
	}, "|")
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
	case WorkerFailureScopeDenied,
		WorkerFailurePolicyDenied,
		WorkerFailurePolicyInvalid,
		WorkerFailureInvalidTaskAction,
		WorkerFailureCommandInterrupted:
		return false
	default:
		return true
	}
}

func hasRepeatedTaskFailureReason(events []EventEnvelope, taskID, reason string) bool {
	taskID = strings.TrimSpace(taskID)
	reason = strings.TrimSpace(reason)
	if taskID == "" || reason == "" {
		return false
	}
	count := 0
	for _, event := range events {
		if event.Type != EventTypeTaskFailed || event.TaskID != taskID {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		eventReason := strings.TrimSpace(toString(payload["reason"]))
		if eventReason == "" {
			eventReason = "worker_exit"
		}
		if eventReason != reason {
			continue
		}
		count++
		if count >= 2 {
			return true
		}
	}
	return false
}

func allowAssistLoopRetry(task TaskSpec) bool {
	strategy := strings.ToLower(strings.TrimSpace(task.Strategy))
	if strategy == "" {
		return false
	}
	if strategy == "recon_seed" {
		return true
	}
	if strings.HasPrefix(strategy, "adaptive_replan_") {
		return false
	}
	return false
}
