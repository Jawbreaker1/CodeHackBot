package orchestrator

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
	priority := deriveAdaptiveReplanPriority(trigger, source, payload, hasBase, baseTask)
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
	dependsOn := []string{}
	if trigger == "new_finding" {
		findingType := strings.ToLower(strings.TrimSpace(toString(payload["finding_type"])))
		if findingType == findingTypeCVECandidateClaim {
			cve := strings.ToUpper(strings.TrimSpace(toString(payload["cve"])))
			missing := compactStringSlice(strings.Split(strings.TrimSpace(toString(payload["missing_phases"])), ","))
			claimReason := strings.TrimSpace(toString(payload["claim_reason"]))
			goal = buildCandidateCVEFollowupGoal(cve, evidenceTarget, missing, claimReason)
			action = assistAction(goal)
			title = "Candidate CVE follow-up"
			if cve != "" {
				title = fmt.Sprintf("Candidate CVE follow-up (%s)", cve)
			}
			expectedName := "candidate-cve-followup.log"
			if cve != "" {
				expectedName = fmt.Sprintf("candidate-cve-%s-followup.log", sanitizePathComponent(strings.ToLower(cve)))
			}
			expectedArtifacts = []string{expectedName}
			risk = string(RiskActiveProbe)
			if sourceTaskID := strings.TrimSpace(source.TaskID); sourceTaskID != "" {
				dependsOn = append(dependsOn, sourceTaskID)
			}
		}
	}
	if trigger == "execution_failure" || trigger == "worker_recovery" || trigger == "budget_exhausted" || trigger == "repeated_step_loop" {
		goal = buildAdaptiveRecoveryGoal(trigger, evidenceTarget, payload)
		action = assistAction(goal)
		title = fmt.Sprintf("Adaptive recovery for %s", trigger)
		expectedArtifacts = []string{fmt.Sprintf("adaptive-recovery-%s.log", sanitizePathComponent(evidenceTarget))}
		if sourceTaskID := strings.TrimSpace(source.TaskID); sourceTaskID != "" {
			dependsOn = append(dependsOn, sourceTaskID)
		}
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
		Priority:          priority,
		Strategy:          "adaptive_replan_" + sanitizePathComponent(trigger),
		Action:            action,
		DoneWhen:          done,
		FailWhen:          fail,
		ExpectedArtifacts: expectedArtifacts,
		DependsOn:         compactStringSlice(dependsOn),
		RiskLevel:         risk,
		Budget:            budget,
	}, nil
}

func deriveAdaptiveReplanPriority(trigger string, source EventEnvelope, payload map[string]any, hasBase bool, baseTask TaskSpec) int {
	basePriority := 50
	if hasBase && baseTask.Priority > 0 {
		basePriority = baseTask.Priority
	}
	clamp := func(value int) int {
		if value < 1 {
			return 1
		}
		if value > 99 {
			return 99
		}
		return value
	}
	trigger = strings.TrimSpace(trigger)
	switch trigger {
	case "operator_instruction":
		return 99
	case "new_finding":
		findingType := strings.ToLower(strings.TrimSpace(toString(payload["finding_type"])))
		if findingType == findingTypeCVECandidateClaim {
			return clamp(basePriority + 1)
		}
		return clamp(basePriority)
	case "execution_failure", "worker_recovery", "repeated_step_loop":
		if taskID := strings.TrimSpace(source.TaskID); taskID != "" &&
			hasBase && strings.HasPrefix(strings.TrimSpace(baseTask.Strategy), "adaptive_replan_") {
			// Keep recursive adaptive recovery below regular queued tasks to avoid starvation.
			return clamp(basePriority - 10)
		}
		return clamp(basePriority)
	case "budget_exhausted":
		return clamp(basePriority - 1)
	default:
		return clamp(basePriority)
	}
}

func buildCandidateCVEFollowupGoal(cve, evidenceTarget string, missingPhases []string, claimReason string) string {
	targetLabel := strings.TrimSpace(evidenceTarget)
	if targetLabel == "" {
		targetLabel = "in-scope target"
	}
	base := "Validate candidate vulnerability evidence with bounded in-scope execution and continue testing if access is obtained."
	if cve != "" {
		base = fmt.Sprintf("Validate candidate vulnerability %s with bounded in-scope execution and continue testing if access is obtained.", cve)
	}
	base += " Source task: " + targetLabel + "."
	if claimReason != "" {
		base += " Candidate reason: " + claimReason + "."
	}
	if len(missingPhases) > 0 {
		base += " Missing verification phases: " + strings.Join(missingPhases, ", ") + "."
	}
	base += " Perform this sequence: (1) look up authoritative vulnerability source details and cite artifacts, (2) derive reproducible validation steps tied to target service/version, (3) run bounded confirmation checks in scope, and (4) if validation grants access, continue post-access security checks allowed by policy and capture evidence."
	base += " Do not rely on memory-only CVE claims; every vulnerability statement must be source-backed and target-applicable."
	base += " End with explicit state for the claim: validated, rejected, or still-candidate with blocker."
	return base
}

func buildAdaptiveRecoveryGoal(trigger, evidenceTarget string, payload map[string]any) string {
	base := fmt.Sprintf("Recover from %s on task %s and continue the run with actionable evidence.", trigger, evidenceTarget)
	reason := CanonicalTaskFailureReason(toString(payload["reason"]))
	errText := strings.TrimSpace(toString(payload["error"]))
	cmd := strings.TrimSpace(toString(payload["command"]))
	args := payloadStringSlice(payload["args"])
	logPath := strings.TrimSpace(toString(payload["log_path"]))
	latestResult := strings.TrimSpace(toString(payload["latest_result_summary"]))
	latestEvidence := payloadStringSlice(payload["latest_evidence_refs"])
	latestInputs := payloadStringSlice(payload["latest_input_refs"])
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
	if latestResult != "" {
		base += " Latest command result: " + latestResult + "."
	}
	if len(latestEvidence) > 0 {
		base += " Latest evidence refs: " + strings.Join(latestEvidence, ", ") + "."
	}
	if len(latestInputs) > 0 {
		base += " Latest input refs: " + strings.Join(latestInputs, ", ") + "."
	}
	if logPath != "" {
		base += " Failure log: " + logPath + "."
	}
	base += " Prefer the newest command result and newest evidence refs over older worker logs. Avoid re-running the same failing command unchanged."
	if shouldIncludeNetworkRecoveryHint(payload) {
		base += " If a broad CIDR scan timed out, switch to host discovery first, then split into targeted host/service follow-up scans."
	}
	base += " Worker mode is non-interactive: do not ask the operator questions; infer best-effort inputs from context and continue."
	base += " End with a concise summary and clear next steps."
	return base
}

func shouldIncludeNetworkRecoveryHint(payload map[string]any) bool {
	command := strings.TrimSpace(toString(payload["command"]))
	if command == "" {
		return false
	}
	if isNetworkSensitiveCommand(command) {
		return true
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "browse", "crawl", "dig", "nslookup", "whois", "curl", "wget":
		return true
	default:
		return false
	}
}

func (c *Coordinator) maybePromoteExecutionFailureRetryTask(source EventEnvelope, payload map[string]any) map[string]any {
	out := map[string]any{}
	taskID := strings.TrimSpace(source.TaskID)
	if taskID == "" || c.isAdaptiveReplanTask(taskID) {
		return out
	}
	reason := CanonicalTaskFailureReason(toString(payload["reason"]))
	if reason != WorkerFailureCommandFailed {
		return out
	}
	errText := strings.ToLower(strings.TrimSpace(toString(payload["error"])))
	if errText == "" || (!strings.Contains(errText, "executable file not found") && !strings.Contains(errText, "not found in $path")) {
		return out
	}
	failedAttempt, _ := payloadInt(payload["attempt"])
	if currentAttempt, ok := c.scheduler.Attempt(taskID); ok && failedAttempt > 0 && currentAttempt <= failedAttempt {
		return out
	}
	task, ok := c.scheduler.Task(taskID)
	if !ok {
		loaded, err := c.manager.ReadTask(c.runID, taskID)
		if err != nil {
			return out
		}
		task = loaded
	}
	if strings.ToLower(strings.TrimSpace(task.Action.Type)) != "command" {
		return out
	}
	failedCommand := strings.TrimSpace(toString(payload["command"]))
	if failedCommand == "" {
		failedCommand = strings.TrimSpace(task.Action.Command)
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(failedCommand)))
	if !isFragileVulnMapperCommand(base) && !taskRequiresVulnerabilityEvidence(task) {
		return out
	}
	originalTask := task
	prompt := strings.TrimSpace(task.Action.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(task.Goal)
	}
	if prompt == "" {
		prompt = "Validate discovered vulnerabilities with source-backed evidence."
	}
	failedArgs := payloadStringSlice(payload["args"])
	failedCommandLine := strings.TrimSpace(strings.Join(append([]string{failedCommand}, failedArgs...), " "))
	if failedCommandLine == "" {
		failedCommandLine = failedCommand
	}
	prompt = strings.TrimSpace(prompt +
		"\nPrevious command failed due to unavailable executable: " + failedCommandLine + "." +
		"\nPivot to available tools in the current environment and do not retry missing wrapper commands unchanged." +
		"\nValidate target applicability with artifact-backed evidence and cite artifact paths.")
	task.Action = TaskAction{
		Type:           "assist",
		Prompt:         prompt,
		WorkingDir:     originalTask.Action.WorkingDir,
		TimeoutSeconds: originalTask.Action.TimeoutSeconds,
	}
	if err := c.scheduler.UpdateTask(task); err != nil {
		out["source_task_mutation_status"] = "scheduler_update_failed"
		out["source_task_mutation_error"] = err.Error()
		return out
	}
	if err := c.manager.UpdateTask(c.runID, task); err != nil {
		_ = c.scheduler.UpdateTask(originalTask)
		out["source_task_mutation_status"] = "task_persist_failed"
		out["source_task_mutation_error"] = err.Error()
		return out
	}
	out["source_task_mutated"] = true
	out["source_task_mutation_status"] = "retry_action_promoted_to_assist"
	out["source_task_id"] = taskID
	out["source_task_previous_action"] = "command"
	out["source_task_next_action"] = "assist"
	out["source_task_failed_command"] = failedCommand
	return out
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

func (c *Coordinator) shouldPromoteAssistLoopExecutionFailure(taskID, eventID string, events []EventEnvelope, idx int) bool {
	taskID = strings.TrimSpace(taskID)
	eventID = strings.TrimSpace(eventID)
	if taskID == "" || eventID == "" {
		return false
	}
	if !hasRepeatedTaskFailureReason(events, taskID, WorkerFailureAssistLoopDetected) {
		return false
	}
	if hasLaterTaskFailureReasonEvent(events, taskID, WorkerFailureAssistLoopDetected, idx) {
		return false
	}
	state, ok := c.scheduler.State(taskID)
	if !ok || state != TaskStateFailed {
		return false
	}
	task, ok := c.scheduler.Task(taskID)
	if !ok {
		loaded, err := c.manager.ReadTask(c.runID, taskID)
		if err != nil {
			return false
		}
		task = loaded
	}
	return strings.EqualFold(strings.TrimSpace(task.Strategy), "recon_seed")
}

func hasLaterTaskFailureReasonEvent(events []EventEnvelope, taskID, reason string, idx int) bool {
	taskID = strings.TrimSpace(taskID)
	reason = CanonicalTaskFailureReason(reason)
	if taskID == "" || reason == "" {
		return false
	}
	if idx < 0 {
		idx = -1
	}
	for i := len(events) - 1; i > idx; i-- {
		event := events[i]
		if event.Type != EventTypeTaskFailed || event.TaskID != taskID {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if CanonicalTaskFailureReason(toString(payload["reason"])) == reason {
			return true
		}
	}
	return false
}

func (c *Coordinator) shouldRewireAssistLoopSeedDependencies(trigger string, source EventEnvelope) bool {
	if strings.TrimSpace(trigger) != "execution_failure" {
		return false
	}
	taskID := strings.TrimSpace(source.TaskID)
	if taskID == "" {
		return false
	}
	payload := map[string]any{}
	if len(source.Payload) > 0 {
		_ = json.Unmarshal(source.Payload, &payload)
	}
	if CanonicalTaskFailureReason(toString(payload["reason"])) != WorkerFailureAssistLoopDetected {
		return false
	}
	task, ok := c.scheduler.Task(taskID)
	if !ok {
		loaded, err := c.manager.ReadTask(c.runID, taskID)
		if err != nil {
			return false
		}
		task = loaded
	}
	return strings.EqualFold(strings.TrimSpace(task.Strategy), "recon_seed")
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
	reason := CanonicalTaskFailureReason(toString(payload["reason"]))
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
	c.replanMutationBucketBudgets = map[string]int{}
	plan, err := c.manager.LoadRunPlan(c.runID)
	if err != nil {
		c.ensureDefaultReplanBucketBudgets()
		return
	}
	if budget := parseReplanBudget(plan.StopCriteria); budget > 0 {
		c.replanMutationBudget = budget
	}
	for bucket, budget := range parseReplanBucketBudgets(plan.StopCriteria) {
		c.replanMutationBucketBudgets[bucket] = budget
	}
	c.ensureDefaultReplanBucketBudgets()
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

const (
	replanMutationBucketDefault            = "default"
	replanMutationBucketCandidateCVEFollow = "candidate_cve_followup"
)

func canonicalReplanMutationBucket(raw string) string {
	token := strings.ToLower(strings.TrimSpace(raw))
	switch token {
	case "", replanMutationBucketDefault:
		return replanMutationBucketDefault
	case replanMutationBucketCandidateCVEFollow, "candidate_cve", "cve_candidate":
		return replanMutationBucketCandidateCVEFollow
	default:
		return token
	}
}

func parseReplanBucketBudgets(stopCriteria []string) map[string]int {
	out := map[string]int{}
	for _, criterion := range stopCriteria {
		raw := strings.ToLower(strings.TrimSpace(criterion))
		for _, prefix := range []string{"max_replans_", "replan_budget_"} {
			if !strings.HasPrefix(raw, prefix) {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
			if rest == "" {
				continue
			}
			parts := strings.SplitN(rest, "=", 2)
			if len(parts) != 2 {
				continue
			}
			bucket := canonicalReplanMutationBucket(parts[0])
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &n); err == nil && n > 0 {
				out[bucket] = n
			}
		}
	}
	return out
}

func (c *Coordinator) ensureDefaultReplanBucketBudgets() {
	if c.replanMutationBucketBudgets == nil {
		c.replanMutationBucketBudgets = map[string]int{}
	}
	if _, ok := c.replanMutationBucketBudgets[replanMutationBucketCandidateCVEFollow]; ok {
		return
	}
	// Reserve part of adaptive budget for non-CVE-replan work so candidate follow-ups cannot monopolize mutations.
	candidateBudget := c.replanMutationBudget / 2
	if candidateBudget < 2 {
		candidateBudget = 2
	}
	if candidateBudget > c.replanMutationBudget && c.replanMutationBudget > 0 {
		candidateBudget = c.replanMutationBudget
	}
	c.replanMutationBucketBudgets[replanMutationBucketCandidateCVEFollow] = candidateBudget
}

func replanMutationBucket(trigger string, payload map[string]any) string {
	trigger = strings.TrimSpace(trigger)
	if trigger == "new_finding" {
		findingType := strings.ToLower(strings.TrimSpace(toString(payload["finding_type"])))
		if findingType == findingTypeCVECandidateClaim {
			return replanMutationBucketCandidateCVEFollow
		}
	}
	return replanMutationBucketDefault
}

func (c *Coordinator) replanMutationBucketCount(bucket string) int {
	bucket = canonicalReplanMutationBucket(bucket)
	if c.replanMutationBucketCounts == nil {
		c.replanMutationBucketCounts = map[string]int{}
	}
	return c.replanMutationBucketCounts[bucket]
}

func (c *Coordinator) incrementReplanMutationBucket(bucket string) int {
	bucket = canonicalReplanMutationBucket(bucket)
	if c.replanMutationBucketCounts == nil {
		c.replanMutationBucketCounts = map[string]int{}
	}
	c.replanMutationBucketCounts[bucket]++
	return c.replanMutationBucketCounts[bucket]
}

func (c *Coordinator) replanMutationBucketBudget(bucket string) int {
	bucket = canonicalReplanMutationBucket(bucket)
	if c.replanMutationBucketBudgets == nil {
		c.replanMutationBucketBudgets = map[string]int{}
	}
	if budget, ok := c.replanMutationBucketBudgets[bucket]; ok {
		return budget
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
				bucket := strings.TrimSpace(toString(payload["mutation_bucket"]))
				if bucket == "" {
					bucket = replanMutationBucket(toString(payload["trigger"]), payload)
				}
				c.incrementReplanMutationBucket(bucket)
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
		reason := CanonicalTaskFailureReason(toString(payload["reason"]))
		if reason == "" {
			reason = TaskFailureReasonWorkerExit
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
	reason = CanonicalTaskFailureReason(reason)
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
	reason = CanonicalTaskFailureReason(reason)
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
		eventReason := CanonicalTaskFailureReason(toString(payload["reason"]))
		if eventReason == "" {
			eventReason = TaskFailureReasonWorkerExit
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
