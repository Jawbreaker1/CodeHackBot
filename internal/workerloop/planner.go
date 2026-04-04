package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
)

func shouldUseWorkerPlanner(packet ctxpacket.WorkerPacket) bool {
	if strings.TrimSpace(packet.PlanState.Mode) == string(workerplan.ModePlannedExecution) {
		return false
	}
	switch strings.TrimSpace(packet.OperatorState.ModeHint) {
	case string(workerplan.ModePlannedExecution):
		return true
	case string(workerplan.ModeDirectExecution), string(workerplan.ModeConversation):
		return false
	}
	goal := strings.ToLower(strings.TrimSpace(packet.SessionFoundation.Goal))
	if goal == "" {
		return false
	}

	trivialPrefixes := []string{
		"pwd",
		"ls",
		"list ",
		"show ",
		"cat ",
		"what files",
		"show me the files",
	}
	for _, prefix := range trivialPrefixes {
		if strings.HasPrefix(goal, prefix) {
			return false
		}
	}

	multiPhaseSignals := []string{
		" and ",
		"extract ",
		"identify ",
		"report",
		"verify ",
		"assess ",
		"reconnaissance",
		"scan ",
		"gain access",
		"find ",
	}
	for _, signal := range multiPhaseSignals {
		if strings.Contains(goal, signal) {
			return true
		}
	}
	return false
}

func ensureWorkerPlan(ctx context.Context, llm llmclient.Client, inspector Inspector, packet ctxpacket.WorkerPacket) (ctxpacket.WorkerPacket, error) {
	if !shouldUseWorkerPlanner(packet) {
		return packet, nil
	}

	attempt := workerplan.AttemptRecord{
		Prompt: buildPlannerPrompt(packet),
	}
	respText, err := llm.Chat(ctx, []llmclient.Message{
		{Role: "system", Content: packet.BehaviorFrame.PromptText()},
		{Role: "user", Content: attempt.Prompt},
	})
	if err != nil {
		attempt.FinalError = fmt.Sprintf("planner chat: %v", err)
		_ = capturePlannerAttemptIfConfigured(inspector, attempt)
		return packet, fmt.Errorf("planner chat: %w", err)
	}
	attempt.RawResponse = respText

	plan, err := workerplan.Parse(respText)
	if err != nil {
		attempt.FinalError = fmt.Sprintf("parse worker plan: %v", err)
		_ = capturePlannerAttemptIfConfigured(inspector, attempt)
		return packet, fmt.Errorf("parse worker plan: %w; raw=%q", err, respText)
	}
	attempt.Parsed = plan
	report := workerplan.Validate(plan)
	attempt.Validation = report
	if !report.Valid() {
		attempt.FinalError = fmt.Sprintf("validate worker plan: %v", report.Error())
		_ = capturePlannerAttemptIfConfigured(inspector, attempt)
		return packet, fmt.Errorf("validate worker plan: %w", report.Error())
	}
	if plan.Mode != workerplan.ModePlannedExecution {
		attempt.FinalError = fmt.Sprintf("unexpected worker planner mode %q", plan.Mode)
		_ = capturePlannerAttemptIfConfigured(inspector, attempt)
		return packet, fmt.Errorf("unexpected worker planner mode %q", plan.Mode)
	}
	attempt.Accepted = true
	_ = capturePlannerAttemptIfConfigured(inspector, attempt)

	packet.PlanState.Mode = string(plan.Mode)
	packet.PlanState.WorkerGoal = plan.WorkerGoal
	packet.PlanState.Summary = plan.PlanSummary
	packet.PlanState.Steps = append([]string(nil), plan.PlanSteps...)
	packet.PlanState.ActiveStep = plan.ActiveStep
	packet.PlanState.BlockedStep = ""
	packet.PlanState.ReplanConditions = append([]string(nil), plan.ReplanConditions...)
	packet.CurrentStep.Objective = plan.ActiveStep
	if strings.TrimSpace(packet.CurrentStep.DoneCondition) == "" {
		packet.CurrentStep.DoneCondition = "complete the current planned step with evidence"
	}
	packet.CurrentStep.FailCondition = joinOrDefault(plan.ReplanConditions, "current step is genuinely blocked or the task changed materially")
	if strings.TrimSpace(packet.CurrentStep.RemainingBudget) == "" {
		packet.CurrentStep.RemainingBudget = "unbounded"
	}
	packet.RunningSummary = "Plan established: " + blank(plan.PlanSummary, strings.Join(plan.PlanSteps, " -> "))
	packet = normalizePlannedAuthorizationScope(packet)
	return packet, nil
}

func buildPlannerPrompt(packet ctxpacket.WorkerPacket) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond with JSON only.",
			"Emit a worker planner output object.",
			"For this planner invocation, mode must be planned_execution.",
			"Use: {\"mode\":\"planned_execution\",\"worker_goal\":\"...\",\"plan_summary\":\"...\",\"plan_steps\":[...],\"active_step\":\"...\",\"replan_conditions\":[...]}",
			"plan_steps must be short, sequential, and semantic.",
			"Do not emit commands, retries, parallel branches, worker assignments, or procedural detail.",
			"Keep the plan human-readable and short.",
			"active_step must be the first current step to execute.",
		},
		"context_packet": packet.RenderWithoutBehaviorFrame(),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func advancePlanStep(packet ctxpacket.WorkerPacket, completedSummary string) (ctxpacket.WorkerPacket, bool) {
	if strings.TrimSpace(packet.PlanState.Mode) != string(workerplan.ModePlannedExecution) {
		return packet, false
	}
	if len(packet.PlanState.Steps) == 0 {
		return packet, false
	}
	idx := -1
	for i, step := range packet.PlanState.Steps {
		if step == packet.PlanState.ActiveStep {
			idx = i
			break
		}
	}
	if idx < 0 || idx == len(packet.PlanState.Steps)-1 {
		return packet, false
	}
	nextStep := packet.PlanState.Steps[idx+1]
	packet.PlanState.ActiveStep = nextStep
	packet.CurrentStep.Objective = nextStep
	packet.CurrentStep.FailCondition = joinOrDefault(packet.PlanState.ReplanConditions, packet.CurrentStep.FailCondition)
	packet.RunningSummary = "Plan step complete: " + blank(completedSummary, packet.PlanState.Steps[idx]) + ". Next step: " + nextStep
	return packet, true
}

func normalizePlannedAuthorizationScope(packet ctxpacket.WorkerPacket) ctxpacket.WorkerPacket {
	if strings.TrimSpace(packet.PlanState.Mode) != string(workerplan.ModePlannedExecution) {
		return packet
	}
	if !isAuthorizationScopeStep(packet.PlanState.ActiveStep) {
		return packet
	}
	if !authorizationScopeEstablished(packet.TaskRuntime.CurrentTarget) {
		return packet
	}
	if nextPacket, advanced := advancePlanStep(packet, "authorization scope already established by session context"); advanced {
		return nextPacket
	}
	return packet
}

func isAuthorizationScopeStep(step string) bool {
	step = strings.ToLower(strings.TrimSpace(step))
	if step == "" {
		return false
	}
	hasScopeWord := strings.Contains(step, "scope") ||
		strings.Contains(step, "authorized") ||
		strings.Contains(step, "authorization") ||
		strings.Contains(step, "in-scope")
	if !hasScopeWord {
		return false
	}
	hasVerificationWord := strings.Contains(step, "verify") ||
		strings.Contains(step, "confirm") ||
		strings.Contains(step, "check")
	hasTargetWord := strings.Contains(step, "target") ||
		strings.Contains(step, "scope")
	return hasVerificationWord && hasTargetWord
}

func authorizationScopeEstablished(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if ip := net.ParseIP(target); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback()
	}
	for _, prefix := range []string{"/", "./", "../"} {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return false
	}
	return false
}

func joinOrDefault(items []string, fallback string) string {
	if len(items) == 0 {
		return fallback
	}
	return strings.Join(items, " | ")
}

func blank(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func capturePlannerAttemptIfConfigured(inspector Inspector, attempt workerplan.AttemptRecord) error {
	if inspector == nil {
		return nil
	}
	return inspector.CapturePlannerAttempt(attempt)
}
