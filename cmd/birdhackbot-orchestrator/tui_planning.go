package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

type planDiff struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
	Summary string   `json:"summary"`
}

type planRevisionRecord struct {
	Version      int       `json:"version"`
	TS           time.Time `json:"ts"`
	Action       string    `json:"action"`
	Summary      string    `json:"summary"`
	TaskCount    int       `json:"task_count"`
	Phase        string    `json:"phase"`
	PlannerMode  string    `json:"planner_mode,omitempty"`
	PromptHash   string    `json:"prompt_hash,omitempty"`
	PlanPath     string    `json:"plan_path"`
	DiffPath     string    `json:"diff_path"`
	OperatorText string    `json:"operator_text,omitempty"`
}

func isPlanningPhase(phase string) bool {
	return phase == orchestrator.RunPhasePlanning || phase == orchestrator.RunPhaseReview
}

func runPhaseFromPlan(plan orchestrator.RunPlan) string {
	phase := orchestrator.NormalizeRunPhase(plan.Metadata.RunPhase)
	if phase != "" {
		return phase
	}
	if len(plan.Tasks) == 0 {
		return orchestrator.RunPhasePlanning
	}
	return orchestrator.RunPhaseApproved
}

func planningInstructionToDraft(manager *orchestrator.Manager, runID, instruction string) (string, error) {
	trimmed := normalizeGoal(instruction)
	if trimmed == "" {
		return "", fmt.Errorf("instruction is required")
	}
	nextPlan, diff, err := applyPlanningPlanUpdate(manager, runID, "instruction_draft", trimmed, func(plan orchestrator.RunPlan) (orchestrator.RunPlan, error) {
		plannerMode := plannerModeForPlanMetadata(plan.Metadata.PlannerMode)
		maxParallelism := plan.MaxParallelism
		if maxParallelism <= 0 {
			maxParallelism = 1
		}
		success := append([]string{}, plan.SuccessCriteria...)
		stop := append([]string{}, plan.StopCriteria...)
		scope := plan.Scope
		constraints := append([]string{}, plan.Constraints...)

		nextPlan, plannerBuildNote, err := buildGoalPlanFromMode(
			context.Background(),
			plannerMode,
			detectWorkerConfigPath(),
			runID,
			trimmed,
			scope,
			constraints,
			success,
			stop,
			maxParallelism,
			5,
			time.Now().UTC(),
		)
		if err != nil {
			return orchestrator.RunPlan{}, err
		}
		nextPlan.Metadata.RunPhase = orchestrator.RunPhaseReview
		nextPlan.Metadata.PlannerVersion = plannerVersion
		nextPlan.Metadata.PlannerPromptHash = plannerPromptHash(trimmed, nextPlan.Metadata.PlannerMode, scope, constraints, nextPlan.SuccessCriteria, nextPlan.StopCriteria, maxParallelism, nextPlan.Metadata.PlannerPlaybooks)
		nextPlan.Metadata.PlannerDecision = "draft"
		nextPlan.Metadata.PlannerRationale = mergePlannerRationale("operator instruction: "+trimmed, plannerBuildNote)
		return nextPlan, nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("plan draft updated: %d tasks generated (phase=%s) | %s", len(nextPlan.Tasks), nextPlan.Metadata.RunPhase, diff.Summary), nil
}

func plannerModeForPlanMetadata(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "llm", plannerModeLLMV1:
		return "llm"
	case "static", plannerModeStaticV1:
		return "static"
	case "auto":
		return "auto"
	default:
		return "auto"
	}
}

func executeTUIPlanningCommand(manager *orchestrator.Manager, runID, action string) (string, bool, error) {
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		return "", false, err
	}
	phase := runPhaseFromPlan(plan)
	switch action {
	case "execute":
		if !isPlanningPhase(phase) && phase != orchestrator.RunPhaseApproved {
			return "", false, fmt.Errorf("run is in %s phase", phase)
		}
		if len(plan.Tasks) == 0 {
			return "", false, fmt.Errorf("no plan tasks to execute; use instruct to draft a plan first")
		}
		if err := validateDraftPlan(plan); err != nil {
			return "", false, fmt.Errorf("plan validation failed: %w", err)
		}
		if err := manager.SetRunPhase(runID, orchestrator.RunPhaseApproved); err != nil {
			return "", false, err
		}
		if err := writeFinalApprovedProvenance(manager, runID, plan); err != nil {
			return "", false, err
		}
		if err := appendPlanningTranscript(manager, runID, map[string]any{
			"action":       "execute",
			"phase_before": phase,
			"phase_after":  orchestrator.RunPhaseApproved,
			"task_count":   len(plan.Tasks),
			"summary":      "execution approved by operator",
		}); err != nil {
			return "", false, err
		}
		return "execution approved; leaving planning mode", true, nil
	case "regenerate":
		goal := strings.TrimSpace(plan.Metadata.Goal)
		if goal == "" {
			return "", false, fmt.Errorf("no goal available; use instruct <goal> to draft a plan")
		}
		msg, err := planningInstructionToDraft(manager, runID, goal)
		if err != nil {
			return "", false, err
		}
		return "plan regenerated: " + msg, false, nil
	case "discard":
		reset := buildInteractivePlanningPlan(runID, time.Now().UTC(), plan.Scope, plan.Constraints, plan.MaxParallelism)
		diff := computePlanDiff(plan, reset)
		if err := manager.ReplaceRunPlan(reset); err != nil {
			return "", false, err
		}
		if err := persistPlanningRevision(manager, runID, plan, reset, "discard", "", diff); err != nil {
			return "", false, err
		}
		return "plan discarded; back to planning mode", false, nil
	default:
		return "", false, fmt.Errorf("unknown planning action: %s", action)
	}
}

func planningTaskAdd(manager *orchestrator.Manager, runID, text string) (string, error) {
	trimmed := normalizeGoal(text)
	if trimmed == "" {
		return "", fmt.Errorf("task text is required")
	}
	nextPlan, diff, err := applyPlanningPlanUpdate(manager, runID, "task_add", trimmed, func(plan orchestrator.RunPlan) (orchestrator.RunPlan, error) {
		next := plan
		taskID := nextManualTaskID(plan.Tasks)
		next.Tasks = append(next.Tasks, orchestrator.TaskSpec{
			TaskID:            taskID,
			Title:             trimmed,
			Goal:              trimmed,
			Targets:           defaultPlanTargets(plan.Scope),
			Priority:          50,
			Strategy:          "manual_edit",
			Action:            orchestrator.TaskAction{Type: "assist", Prompt: trimmed},
			DoneWhen:          []string{"manual_task_completed"},
			FailWhen:          []string{"manual_task_failed", "manual_task_timeout"},
			ExpectedArtifacts: []string{taskID + ".log"},
			RiskLevel:         string(orchestrator.RiskReconReadonly),
			Budget: orchestrator.TaskBudget{
				MaxSteps:     12,
				MaxToolCalls: 20,
				MaxRuntime:   8 * time.Minute,
			},
		})
		next.Metadata.PlannerDecision = "edited"
		return next, nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task added: %s | total=%d | %s", nextPlan.Tasks[len(nextPlan.Tasks)-1].TaskID, len(nextPlan.Tasks), diff.Summary), nil
}

func planningTaskRemove(manager *orchestrator.Manager, runID, taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task id is required")
	}
	nextPlan, diff, err := applyPlanningPlanUpdate(manager, runID, "task_remove", taskID, func(plan orchestrator.RunPlan) (orchestrator.RunPlan, error) {
		next := plan
		index := -1
		for i, task := range next.Tasks {
			if task.TaskID == taskID {
				index = i
				break
			}
		}
		if index < 0 {
			return orchestrator.RunPlan{}, fmt.Errorf("task not found: %s", taskID)
		}
		next.Tasks = append(next.Tasks[:index], next.Tasks[index+1:]...)
		for i := range next.Tasks {
			next.Tasks[i].DependsOn = filterOut(next.Tasks[i].DependsOn, taskID)
		}
		next.Metadata.PlannerDecision = "edited"
		return next, nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task removed: %s | total=%d | %s", taskID, len(nextPlan.Tasks), diff.Summary), nil
}

func planningTaskSetField(manager *orchestrator.Manager, runID, taskID, field, value string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	field = strings.ToLower(strings.TrimSpace(field))
	value = strings.TrimSpace(value)
	if taskID == "" || field == "" || value == "" {
		return "", fmt.Errorf("usage: task set <task-id> <field> <value>")
	}
	_, diff, err := applyPlanningPlanUpdate(manager, runID, "task_set", taskID+" "+field+"="+value, func(plan orchestrator.RunPlan) (orchestrator.RunPlan, error) {
		next := plan
		index := -1
		for i, task := range next.Tasks {
			if task.TaskID == taskID {
				index = i
				break
			}
		}
		if index < 0 {
			return orchestrator.RunPlan{}, fmt.Errorf("task not found: %s", taskID)
		}
		task := next.Tasks[index]
		switch field {
		case "title":
			task.Title = value
		case "goal":
			task.Goal = value
		case "strategy":
			task.Strategy = value
		case "risk":
			task.RiskLevel = strings.ToLower(value)
		case "targets":
			task.Targets = parseCommaList(value)
		case "depends_on":
			task.DependsOn = parseCommaList(value)
		case "priority":
			n, err := strconv.Atoi(value)
			if err != nil {
				return orchestrator.RunPlan{}, fmt.Errorf("priority must be integer")
			}
			task.Priority = n
		case "budget_steps":
			n, err := strconv.Atoi(value)
			if err != nil {
				return orchestrator.RunPlan{}, fmt.Errorf("budget_steps must be integer")
			}
			task.Budget.MaxSteps = n
		case "budget_tools":
			n, err := strconv.Atoi(value)
			if err != nil {
				return orchestrator.RunPlan{}, fmt.Errorf("budget_tools must be integer")
			}
			task.Budget.MaxToolCalls = n
		case "budget_runtime":
			d, err := time.ParseDuration(value)
			if err != nil {
				return orchestrator.RunPlan{}, fmt.Errorf("budget_runtime must be duration (e.g. 10m)")
			}
			task.Budget.MaxRuntime = d
		default:
			return orchestrator.RunPlan{}, fmt.Errorf("unsupported field: %s", field)
		}
		next.Tasks[index] = task
		next.Metadata.PlannerDecision = "edited"
		return next, nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task updated: %s (%s) | %s", taskID, field, diff.Summary), nil
}

func planningTaskMove(manager *orchestrator.Manager, runID, taskID string, position int) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || position <= 0 {
		return "", fmt.Errorf("usage: task move <task-id> <position>")
	}
	_, diff, err := applyPlanningPlanUpdate(manager, runID, "task_move", fmt.Sprintf("%s->%d", taskID, position), func(plan orchestrator.RunPlan) (orchestrator.RunPlan, error) {
		next := plan
		if len(next.Tasks) == 0 {
			return orchestrator.RunPlan{}, fmt.Errorf("plan has no tasks")
		}
		from := -1
		for i, task := range next.Tasks {
			if task.TaskID == taskID {
				from = i
				break
			}
		}
		if from < 0 {
			return orchestrator.RunPlan{}, fmt.Errorf("task not found: %s", taskID)
		}
		to := position - 1
		if to >= len(next.Tasks) {
			to = len(next.Tasks) - 1
		}
		if from == to {
			return next, nil
		}
		task := next.Tasks[from]
		next.Tasks = append(next.Tasks[:from], next.Tasks[from+1:]...)
		prefix := append([]orchestrator.TaskSpec{}, next.Tasks[:to]...)
		suffix := append([]orchestrator.TaskSpec{}, next.Tasks[to:]...)
		next.Tasks = append(prefix, append([]orchestrator.TaskSpec{task}, suffix...)...)
		next.Metadata.PlannerDecision = "edited"
		return next, nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task moved: %s -> %d | %s", taskID, position, diff.Summary), nil
}

func applyPlanningPlanUpdate(manager *orchestrator.Manager, runID, action, operatorInput string, mutate func(plan orchestrator.RunPlan) (orchestrator.RunPlan, error)) (orchestrator.RunPlan, planDiff, error) {
	current, err := manager.LoadRunPlan(runID)
	if err != nil {
		return orchestrator.RunPlan{}, planDiff{}, err
	}
	if !isPlanningPhase(runPhaseFromPlan(current)) {
		return orchestrator.RunPlan{}, planDiff{}, fmt.Errorf("run is not in planning/review phase")
	}
	next, err := mutate(current)
	if err != nil {
		return orchestrator.RunPlan{}, planDiff{}, err
	}
	ensureReviewCriteriaDefaults(&next)
	next.RunID = current.RunID
	if len(next.Tasks) == 0 {
		next.Metadata.RunPhase = orchestrator.RunPhasePlanning
	} else if runPhaseFromPlan(next) == orchestrator.RunPhasePlanning {
		next.Metadata.RunPhase = orchestrator.RunPhaseReview
	}
	if err := validatePlanningUpdate(next); err != nil {
		return orchestrator.RunPlan{}, planDiff{}, err
	}
	diff := computePlanDiff(current, next)
	if err := manager.ReplaceRunPlan(next); err != nil {
		return orchestrator.RunPlan{}, planDiff{}, err
	}
	if err := persistPlanningRevision(manager, runID, current, next, action, operatorInput, diff); err != nil {
		return orchestrator.RunPlan{}, planDiff{}, err
	}
	return next, diff, nil
}

func ensureReviewCriteriaDefaults(plan *orchestrator.RunPlan) {
	if plan == nil || len(plan.Tasks) == 0 {
		return
	}
	if len(plan.SuccessCriteria) == 0 {
		plan.SuccessCriteria = []string{"manual_plan_completed"}
	}
	if len(plan.StopCriteria) == 0 {
		plan.StopCriteria = []string{"manual_stop", "out_of_scope", "budget_exhausted"}
	}
}

func validatePlanningUpdate(plan orchestrator.RunPlan) error {
	if len(plan.Tasks) == 0 {
		return orchestrator.ValidateRunPlan(plan)
	}
	return validateDraftPlan(plan)
}

func validateDraftPlan(plan orchestrator.RunPlan) error {
	if err := orchestrator.ValidateRunPlan(plan); err != nil {
		return err
	}
	if len(plan.Tasks) == 0 {
		return fmt.Errorf("draft plan has no tasks")
	}
	if err := orchestrator.ValidateSynthesizedPlan(plan); err != nil {
		return err
	}
	return nil
}

func computePlanDiff(previous, next orchestrator.RunPlan) planDiff {
	prevByID := make(map[string]orchestrator.TaskSpec, len(previous.Tasks))
	for _, task := range previous.Tasks {
		prevByID[task.TaskID] = task
	}
	nextByID := make(map[string]orchestrator.TaskSpec, len(next.Tasks))
	for _, task := range next.Tasks {
		nextByID[task.TaskID] = task
	}
	diff := planDiff{}
	for taskID, task := range nextByID {
		prevTask, ok := prevByID[taskID]
		if !ok {
			diff.Added = append(diff.Added, taskID)
			continue
		}
		if !equalTaskSpec(prevTask, task) {
			diff.Changed = append(diff.Changed, taskID)
		}
	}
	for taskID := range prevByID {
		if _, ok := nextByID[taskID]; !ok {
			diff.Removed = append(diff.Removed, taskID)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Strings(diff.Changed)
	diff.Summary = fmt.Sprintf("diff +%d/-%d/~%d tasks", len(diff.Added), len(diff.Removed), len(diff.Changed))
	return diff
}

func equalTaskSpec(left, right orchestrator.TaskSpec) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftBytes) == string(rightBytes)
}

func persistPlanningRevision(manager *orchestrator.Manager, runID string, previous, next orchestrator.RunPlan, action, operatorInput string, diff planDiff) error {
	if err := writePlanDiffArtifact(manager, runID, diff); err != nil {
		return err
	}
	record, err := writePlanRevisionFiles(manager, runID, next, action, operatorInput, diff)
	if err != nil {
		return err
	}
	return appendPlanningTranscript(manager, runID, map[string]any{
		"action":         action,
		"operator_input": operatorInput,
		"phase_before":   runPhaseFromPlan(previous),
		"phase_after":    runPhaseFromPlan(next),
		"summary":        diff.Summary,
		"task_count":     len(next.Tasks),
		"revision":       record.Version,
	})
}

func writePlanDiffArtifact(manager *orchestrator.Manager, runID string, diff planDiff) error {
	paths := orchestrator.BuildRunPaths(manager.SessionsDir, runID)
	path := filepath.Join(paths.PlanDir, "plan.diff.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(diff.Summary)
	b.WriteByte('\n')
	if len(diff.Added) > 0 {
		b.WriteString("added: ")
		b.WriteString(strings.Join(diff.Added, ", "))
		b.WriteByte('\n')
	}
	if len(diff.Removed) > 0 {
		b.WriteString("removed: ")
		b.WriteString(strings.Join(diff.Removed, ", "))
		b.WriteByte('\n')
	}
	if len(diff.Changed) > 0 {
		b.WriteString("changed: ")
		b.WriteString(strings.Join(diff.Changed, ", "))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writePlanRevisionFiles(manager *orchestrator.Manager, runID string, plan orchestrator.RunPlan, action, operatorInput string, diff planDiff) (planRevisionRecord, error) {
	paths := orchestrator.BuildRunPaths(manager.SessionsDir, runID)
	revisionDir := filepath.Join(paths.PlanDir, "revisions")
	if err := os.MkdirAll(revisionDir, 0o755); err != nil {
		return planRevisionRecord{}, err
	}
	indexPath := filepath.Join(revisionDir, "index.json")
	index := []planRevisionRecord{}
	if data, err := os.ReadFile(indexPath); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &index)
	}
	version := len(index) + 1
	planFilename := fmt.Sprintf("plan.v%03d.json", version)
	diffFilename := fmt.Sprintf("plan.v%03d.diff.json", version)
	planPath := filepath.Join(revisionDir, planFilename)
	diffPath := filepath.Join(revisionDir, diffFilename)
	if err := orchestrator.WriteJSONAtomic(planPath, plan); err != nil {
		return planRevisionRecord{}, err
	}
	if err := orchestrator.WriteJSONAtomic(diffPath, diff); err != nil {
		return planRevisionRecord{}, err
	}
	record := planRevisionRecord{
		Version:      version,
		TS:           time.Now().UTC(),
		Action:       action,
		Summary:      diff.Summary,
		TaskCount:    len(plan.Tasks),
		Phase:        runPhaseFromPlan(plan),
		PlannerMode:  plan.Metadata.PlannerMode,
		PromptHash:   plan.Metadata.PlannerPromptHash,
		PlanPath:     filepath.Base(planPath),
		DiffPath:     filepath.Base(diffPath),
		OperatorText: operatorInput,
	}
	index = append(index, record)
	if err := orchestrator.WriteJSONAtomic(indexPath, index); err != nil {
		return planRevisionRecord{}, err
	}
	return record, nil
}

func appendPlanningTranscript(manager *orchestrator.Manager, runID string, payload map[string]any) error {
	paths := orchestrator.BuildRunPaths(manager.SessionsDir, runID)
	path := filepath.Join(paths.PlanDir, "planner_transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := map[string]any{
		"ts":     time.Now().UTC(),
		"run_id": runID,
	}
	for k, v := range payload {
		entry[k] = v
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func writeFinalApprovedProvenance(manager *orchestrator.Manager, runID string, plan orchestrator.RunPlan) error {
	paths := orchestrator.BuildRunPaths(manager.SessionsDir, runID)
	path := filepath.Join(paths.PlanDir, "final_approved_provenance.json")
	approvals := collectApprovalProvenance(manager, runID)
	provenance := map[string]any{
		"run_id":              runID,
		"approved_at":         time.Now().UTC().Format(time.RFC3339),
		"approved_via":        "tui_execute",
		"phase":               orchestrator.RunPhaseApproved,
		"task_count":          len(plan.Tasks),
		"planner_mode":        plan.Metadata.PlannerMode,
		"planner_version":     plan.Metadata.PlannerVersion,
		"planner_playbooks":   plan.Metadata.PlannerPlaybooks,
		"planner_prompt_hash": plan.Metadata.PlannerPromptHash,
		"planner_decision":    plan.Metadata.PlannerDecision,
		"planner_model":       plannerModelForProvenance(plan),
		"operator_approvals":  approvals,
		"approval_count":      len(approvals),
	}
	return orchestrator.WriteJSONAtomic(path, provenance)
}

func plannerModelForProvenance(plan orchestrator.RunPlan) string {
	if trimmed := strings.TrimSpace(plan.Metadata.PlannerModel); trimmed != "" {
		return trimmed
	}
	mode := strings.ToLower(strings.TrimSpace(plan.Metadata.PlannerMode))
	if mode != plannerModeLLMV1 && mode != "llm" && mode != "auto" {
		return ""
	}
	cfg, err := loadPlannerLLMConfig(detectWorkerConfigPath())
	if err != nil {
		return ""
	}
	model := strings.TrimSpace(cfg.LLM.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.Model)
	}
	return model
}

func collectApprovalProvenance(manager *orchestrator.Manager, runID string) []map[string]any {
	events, err := manager.Events(runID, 0)
	if err != nil {
		return nil
	}
	out := make([]map[string]any, 0)
	for _, event := range events {
		if event.Type != orchestrator.EventTypeApprovalGranted && event.Type != orchestrator.EventTypeApprovalDenied {
			continue
		}
		payload := decodeEventPayload(event.Payload)
		out = append(out, map[string]any{
			"event_id":    event.EventID,
			"ts":          event.TS.Format(time.RFC3339),
			"type":        event.Type,
			"worker_id":   event.WorkerID,
			"task_id":     event.TaskID,
			"approval_id": strings.TrimSpace(stringFromAny(payload["approval_id"])),
			"scope":       strings.TrimSpace(stringFromAny(payload["scope"])),
			"actor":       strings.TrimSpace(stringFromAny(payload["actor"])),
			"reason":      strings.TrimSpace(stringFromAny(payload["reason"])),
		})
	}
	return out
}

func defaultPlanTargets(scope orchestrator.Scope) []string {
	targets := append([]string{}, scope.Targets...)
	targets = append(targets, scope.Networks...)
	if len(targets) == 0 {
		return []string{"local"}
	}
	return compactStringSlice(targets)
}

func nextManualTaskID(tasks []orchestrator.TaskSpec) string {
	maxID := 0
	for _, task := range tasks {
		id := strings.TrimSpace(task.TaskID)
		if !strings.HasPrefix(id, "task-manual-") {
			continue
		}
		suffix := strings.TrimPrefix(id, "task-manual-")
		n, err := strconv.Atoi(suffix)
		if err == nil && n > maxID {
			maxID = n
		}
	}
	return fmt.Sprintf("task-manual-%03d", maxID+1)
}

func parseCommaList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return compactStringSlice(out)
}

func filterOut(values []string, target string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			continue
		}
		out = append(out, value)
	}
	return out
}

func compactStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
