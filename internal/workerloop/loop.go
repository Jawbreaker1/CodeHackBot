package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

type Inspector interface {
	Capture(step int, stage string, packet ctxpacket.WorkerPacket) error
}

type Loop struct {
	LLM       llmclient.Client
	Executor  execx.Executor
	Approver  approval.Approver
	Inspector Inspector
}

type Outcome struct {
	Summary string
	Packet  ctxpacket.WorkerPacket
}

func (l Loop) Run(ctx context.Context, packet ctxpacket.WorkerPacket, maxSteps int) (Outcome, error) {
	if maxSteps <= 0 {
		maxSteps = 1
	}
	current := packet
	if strings.TrimSpace(current.TaskRuntime.State) == "" {
		current.TaskRuntime = ctxpacket.InitialTaskRuntime(current.SessionFoundation.Goal)
	}
	for step := 1; step <= maxSteps; step++ {
		if err := captureIfConfigured(l.Inspector, step, "pre-llm", current); err != nil {
			return Outcome{Packet: current}, fmt.Errorf("capture pre-llm context: %w", err)
		}
		respText, err := l.LLM.Chat(ctx, []llmclient.Message{
			{Role: "system", Content: current.BehaviorFrame.PromptText()},
			{Role: "user", Content: buildUserPrompt(current)},
		})
		if err != nil {
			return Outcome{Packet: current}, fmt.Errorf("llm chat: %w", err)
		}

		resp, err := ParseResponse(respText)
		if err != nil {
			return Outcome{Packet: current}, fmt.Errorf("parse llm response: %w; raw=%q", err, respText)
		}

		switch resp.Type {
		case "step_complete":
			current.TaskRuntime.State = "done"
			current.TaskRuntime = ctxpacket.UpdateTaskRuntime(current.TaskRuntime, current.SessionFoundation.Goal, current.LatestExecutionResult)
			current.TaskRuntime.MissingFact = "(none)"
			current.RunningSummary = buildCompletionSummary(current.SessionFoundation.Goal, current.LatestExecutionResult, current.RelevantRecentResults)
			if err := captureIfConfigured(l.Inspector, step, "step-complete", current); err != nil {
				return Outcome{Packet: current}, fmt.Errorf("capture completion context: %w", err)
			}
			return Outcome{Summary: resp.Summary, Packet: current}, nil
		case "ask_user":
			current.TaskRuntime.State = "waiting_user"
			if err := captureIfConfigured(l.Inspector, step, "ask-user", current); err != nil {
				return Outcome{Packet: current}, fmt.Errorf("capture ask-user context: %w", err)
			}
			return Outcome{Packet: current}, fmt.Errorf("worker requires user input: %s", resp.Question)
		case "action":
			action, validationFailure := prepareAction(resp)
			if validationFailure != nil {
				current.RelevantRecentResults = updateRelevantRecentResults(current.RelevantRecentResults, current.LatestExecutionResult, current.TaskRuntime.CurrentTarget)
				current.LatestExecutionResult = *validationFailure
				truth := ctxpacket.ActiveTruthResult(current.LatestExecutionResult, current.RelevantRecentResults)
				current.TaskRuntime.State = "running"
				current.TaskRuntime = ctxpacket.UpdateTaskRuntime(current.TaskRuntime, current.SessionFoundation.Goal, truth)
				current.RunningSummary = buildRunningSummary(current.CurrentStep.Objective, current.LatestExecutionResult, current.RelevantRecentResults)
				if err := captureIfConfigured(l.Inspector, step, "post-validation", current); err != nil {
					return Outcome{Packet: current}, fmt.Errorf("capture validation context: %w", err)
				}
				continue
			}
			if l.Approver == nil {
				return Outcome{Packet: current}, fmt.Errorf("execution requires an approver")
			}
			decision, err := l.Approver.Approve(ctx, approval.Request{
				Command:  resp.Command,
				UseShell: resp.UseShell,
			})
			if err != nil {
				return Outcome{Packet: current}, fmt.Errorf("approval failed: %w", err)
			}
			current.OperatorState.ApprovalState = string(decision)
			if decision == approval.DecisionDeny {
				return Outcome{Packet: current}, fmt.Errorf("execution denied by user")
			}
			plan, err := l.Executor.Plan(action)
			if err != nil {
				return Outcome{Packet: current}, fmt.Errorf("prepare execution: %w", err)
			}
			current.OperatorState.PendingAction = plan.Requested
			current.OperatorState.PendingMode = plan.ExecutionMode
			current.OperatorState.PendingExec = plan.ActualExec
			current.OperatorState.PendingLog = plan.LogPath
			if err := captureIfConfigured(l.Inspector, step, "pre-action", current); err != nil {
				return Outcome{Packet: current}, fmt.Errorf("capture pre-action context: %w", err)
			}

			result, execErr := l.Executor.RunPlanned(ctx, plan)
			current.OperatorState.PendingAction = ""
			current.OperatorState.PendingMode = ""
			current.OperatorState.PendingExec = ""
			current.OperatorState.PendingLog = ""
			nextResult := ctxpacket.ExecutionResult{
				Action:        result.Action,
				ExitStatus:    fmt.Sprintf("%d", result.ExitStatus),
				OutputSummary: compactOutputSummary(combineSummaries(result.StdoutSummary, result.StderrSummary)),
				LogRefs:       []string{result.LogPath},
				ArtifactRefs:  result.ArtifactRefs,
				Assessment:    result.Assessment,
				Signals:       append([]string(nil), result.Signals...),
				FailureClass:  result.FailureClass,
			}
			current.RelevantRecentResults = updateRelevantRecentResults(current.RelevantRecentResults, current.LatestExecutionResult, current.TaskRuntime.CurrentTarget)
			current.LatestExecutionResult = nextResult
			truth := ctxpacket.ActiveTruthResult(current.LatestExecutionResult, current.RelevantRecentResults)
			current.TaskRuntime.State = "running"
			current.TaskRuntime = ctxpacket.UpdateTaskRuntime(current.TaskRuntime, current.SessionFoundation.Goal, truth)
			current.RunningSummary = buildRunningSummary(current.CurrentStep.Objective, current.LatestExecutionResult, current.RelevantRecentResults)
			if err := captureIfConfigured(l.Inspector, step, "post-action", current); err != nil {
				return Outcome{Packet: current}, fmt.Errorf("capture post-action context: %w", err)
			}
			if execErr != nil {
				// Still feed the failure result into the next turn.
			}
		}
	}
	current.TaskRuntime.State = "blocked"
	return Outcome{Packet: current}, fmt.Errorf("step did not complete within %d steps", maxSteps)
}

func buildUserPrompt(packet ctxpacket.WorkerPacket) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond with JSON only.",
			"Choose exactly one of: action, step_complete, ask_user.",
			"If choosing action, use: {\"type\":\"action\",\"command\":\"...\",\"use_shell\":true|false}.",
			"If choosing step_complete, use: {\"type\":\"step_complete\",\"summary\":\"...\"}.",
			"If choosing ask_user, use: {\"type\":\"ask_user\",\"question\":\"...\"}.",
			"Use task_runtime.current_target as the concrete thing currently being worked.",
			"Use task_runtime.missing_fact as the primary description of what still needs to be learned or verified.",
			"If task_runtime.missing_fact is not '(none)', prefer an action that establishes that missing fact for the current target.",
			"Before choosing action, check whether the current goal is already satisfied by the latest execution result or relevant recent results.",
			"If the goal is already satisfied with evidence in the context packet, choose step_complete.",
			"Do not spend another turn re-reading or slicing the same log, command output, or artifact when the needed evidence is already present in the context packet.",
			"If the latest result clearly failed, first reconsider whether the failed command structure itself was necessary before repeating or elaborating it.",
			"After a clear failure, prefer a simpler next action that removes the failure cause or gathers the missing fact directly.",
			"Do not preserve self-invented scaffolding such as custom output files or new directories unless they are actually needed for the goal.",
			"Do not invent hidden steps.",
			"Do not use alternative key names or nested envelopes.",
		},
		"context_packet": packet.RenderWithoutBehaviorFrame(),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func combineSummaries(stdout, stderr string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(stdout) != "" && stdout != "(none)" {
		parts = append(parts, "stdout: "+stdout)
	}
	if strings.TrimSpace(stderr) != "" && stderr != "(none)" {
		parts = append(parts, "stderr: "+stderr)
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " | ")
}

func compactOutputSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "(none)" {
		return "(none)"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 2 {
		lines = lines[:2]
	}
	s = strings.Join(lines, "\n")
	const limit = 220
	if len(s) > limit {
		return strings.TrimSpace(s[:limit]) + "..."
	}
	return s
}

func updateRelevantRecentResults(current []ctxpacket.ExecutionResult, previousLatest ctxpacket.ExecutionResult, currentTarget string) []ctxpacket.ExecutionResult {
	if strings.TrimSpace(previousLatest.Action) == "" {
		return current
	}

	type scoredResult struct {
		result ctxpacket.ExecutionResult
		score  int
		order  int
	}

	candidates := append([]ctxpacket.ExecutionResult{previousLatest}, current...)
	scored := make([]scoredResult, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for i, candidate := range candidates {
		key := strings.TrimSpace(candidate.Action) + "|" + strings.TrimSpace(candidate.ExitStatus) + "|" + strings.TrimSpace(candidate.Assessment)
		if strings.TrimSpace(candidate.Action) == "" || seen[key] {
			continue
		}
		seen[key] = true
		scored = append(scored, scoredResult{
			result: candidate,
			score:  retainedEvidenceScore(candidate, currentTarget),
			order:  i,
		})
	}

	slices.SortStableFunc(scored, func(a, b scoredResult) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return a.order - b.order
	})

	next := make([]ctxpacket.ExecutionResult, 0, min(3, len(scored)))
	for _, item := range scored {
		next = append(next, item.result)
		if len(next) == 3 {
			break
		}
	}
	return next
}

func retainedEvidenceScore(result ctxpacket.ExecutionResult, currentTarget string) int {
	score := 0
	if mentionsTarget(result, currentTarget) {
		score += 100
	}
	switch strings.TrimSpace(result.Assessment) {
	case "failed":
		score += 40
	case "suspicious":
		score += 30
	case "ambiguous":
		score += 20
	case "success":
		score += 10
	}
	if strings.TrimSpace(result.FailureClass) != "" {
		score += 15
	}
	if strings.TrimSpace(result.ExitStatus) != "" && strings.TrimSpace(result.ExitStatus) != "0" && strings.TrimSpace(result.ExitStatus) != "(none)" {
		score += 20
	}
	score += len(result.Signals) * 5
	if strings.TrimSpace(result.OutputSummary) == "" || strings.TrimSpace(result.OutputSummary) == "(none)" {
		score -= 5
	}
	return score
}

func mentionsTarget(result ctxpacket.ExecutionResult, currentTarget string) bool {
	currentTarget = strings.TrimSpace(currentTarget)
	if currentTarget == "" {
		return false
	}
	fields := []string{
		result.Action,
		result.OutputSummary,
		strings.Join(result.ArtifactRefs, " "),
		strings.Join(result.LogRefs, " "),
	}
	for _, field := range fields {
		if strings.Contains(field, currentTarget) {
			return true
		}
	}
	return false
}

func buildRunningSummary(objective string, latest ctxpacket.ExecutionResult, recent []ctxpacket.ExecutionResult) string {
	truth := ctxpacket.ActiveTruthResult(latest, recent)
	status := "in progress"
	if strings.TrimSpace(truth.Assessment) == "failed" || strings.TrimSpace(truth.FailureClass) != "" || strings.TrimSpace(truth.ExitStatus) == "-1" {
		status = "encountered a failure"
	}
	if strings.TrimSpace(truth.ExitStatus) != "" && strings.TrimSpace(truth.ExitStatus) != "(none)" && strings.TrimSpace(truth.ExitStatus) != "0" {
		status = "encountered a failure"
	}
	if strings.TrimSpace(truth.Assessment) == "suspicious" || strings.TrimSpace(truth.Assessment) == "ambiguous" {
		status = "needs interpretation"
	}

	parts := []string{fmt.Sprintf("Status: %s.", status)}
	if strings.TrimSpace(truth.Action) != "" {
		parts = append(parts, fmt.Sprintf("Evidence: %q exited with %s.", compactInline(truth.Action, 120), blankOrFallback(strings.TrimSpace(truth.ExitStatus), "(none)")))
	}
	if strings.TrimSpace(truth.Assessment) != "" && strings.TrimSpace(truth.Assessment) != "(none)" {
		parts = append(parts, fmt.Sprintf("Assessment: %s.", truth.Assessment))
	}
	if len(truth.Signals) > 0 {
		parts = append(parts, fmt.Sprintf("Signals: %s.", strings.Join(truth.Signals, ", ")))
	}
	if strings.TrimSpace(truth.OutputSummary) != "" && strings.TrimSpace(truth.OutputSummary) != "(none)" {
		parts = append(parts, fmt.Sprintf("Key output: %s.", compactInline(singleLine(truth.OutputSummary), 220)))
	}
	if truth.Action != latest.Action && strings.TrimSpace(truth.Action) != "" {
		parts = append(parts, "Latest result was weaker than retained evidence.")
	}
	return strings.Join(parts, " ")
}

func buildCompletionSummary(goal string, latest ctxpacket.ExecutionResult, recent []ctxpacket.ExecutionResult) string {
	truth := ctxpacket.ActiveTruthResult(latest, recent)
	parts := []string{"Status: done."}
	if strings.TrimSpace(goal) != "" {
		parts = append(parts, fmt.Sprintf("Goal: %s.", compactInline(goal, 160)))
	}
	if strings.TrimSpace(truth.Action) != "" {
		parts = append(parts, fmt.Sprintf("Completion evidence: %q exited with %s.", compactInline(truth.Action, 120), blankOrFallback(strings.TrimSpace(truth.ExitStatus), "(none)")))
	}
	if strings.TrimSpace(truth.Assessment) != "" && strings.TrimSpace(truth.Assessment) != "(none)" {
		parts = append(parts, fmt.Sprintf("Assessment: %s.", truth.Assessment))
	}
	if len(truth.Signals) > 0 {
		parts = append(parts, fmt.Sprintf("Signals: %s.", strings.Join(truth.Signals, ", ")))
	}
	if strings.TrimSpace(truth.OutputSummary) != "" && strings.TrimSpace(truth.OutputSummary) != "(none)" {
		parts = append(parts, fmt.Sprintf("Key output: %s.", compactInline(singleLine(truth.OutputSummary), 220)))
	}
	return strings.Join(parts, " ")
}

func singleLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " | ")
	return s
}

func blankOrFallback(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func compactInline(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return strings.TrimSpace(s[:max-3]) + "..."
}

func captureIfConfigured(inspector Inspector, step int, stage string, packet ctxpacket.WorkerPacket) error {
	if inspector == nil {
		return nil
	}
	return inspector.Capture(step, stage, packet)
}
