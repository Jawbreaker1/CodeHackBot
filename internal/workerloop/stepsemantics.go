package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerstep"
)

type StepStatus string

const (
	StepInProgress StepStatus = "in_progress"
	StepSatisfied  StepStatus = "satisfied"
	StepBlocked    StepStatus = "blocked"
)

type StepEvaluation struct {
	Status  StepStatus
	Reason  string
	Summary string
}

func evaluateActivePlanStep(ctx context.Context, llm llmclient.Client, inspector Inspector, packet ctxpacket.WorkerPacket, completedSummary string) StepEvaluation {
	if strings.TrimSpace(packet.PlanState.Mode) != string(workerplan.ModePlannedExecution) || strings.TrimSpace(packet.PlanState.ActiveStep) == "" {
		return StepEvaluation{Status: StepInProgress}
	}

	latest := packet.LatestExecutionResult
	if blockedReason := stepBlockedReason(packet, latest, completedSummary); blockedReason != "" {
		return StepEvaluation{Status: StepBlocked, Reason: blockedReason}
	}
	if ctxpacket.IsInterruptedResult(latest) {
		return StepEvaluation{
			Status: StepInProgress,
			Reason: "active step work was interrupted after execution started",
		}
	}

	stepEval, err := judgeActivePlanStep(ctx, llm, inspector, packet, completedSummary)
	if err == nil {
		return StepEvaluation{
			Status:  StepStatus(stepEval.Status),
			Reason:  stepEval.Reason,
			Summary: stepEval.Summary,
		}
	}
	if strings.TrimSpace(completedSummary) != "" && hasStepEvidence(packet) {
		return StepEvaluation{Status: StepSatisfied, Reason: "step_complete is supported by execution evidence", Summary: completedSummary}
	}
	return StepEvaluation{Status: StepInProgress, Reason: "more evidence is needed before advancing the active step"}
}

func stepBlockedReason(packet ctxpacket.WorkerPacket, latest ctxpacket.ExecutionResult, completedSummary string) string {
	searchSpace := strings.ToLower(strings.Join([]string{
		packet.RunningSummary,
		completedSummary,
		preferredExecutionEvidence(latest),
		latest.Action,
	}, "\n"))
	for _, condition := range packet.PlanState.ReplanConditions {
		condition = strings.TrimSpace(condition)
		if condition == "" {
			continue
		}
		if strings.Contains(searchSpace, strings.ToLower(condition)) {
			return condition
		}
	}
	return ""
}

func hasStepEvidence(packet ctxpacket.WorkerPacket) bool {
	candidates := append([]ctxpacket.ExecutionResult{packet.LatestExecutionResult}, packet.RelevantRecentResults...)
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Action) == "" {
			continue
		}
		if ctxpacket.ImpactOfResult(candidate) == ctxpacket.ResultImpactBlocking {
			continue
		}
		if strings.TrimSpace(candidate.Assessment) == "failed" || strings.TrimSpace(candidate.FailureClass) != "" || nonzeroExitStatus(candidate.ExitStatus) {
			continue
		}
		return true
	}
	return false
}

func nonzeroExitStatus(exitStatus string) bool {
	exitStatus = strings.TrimSpace(exitStatus)
	return exitStatus != "" && exitStatus != "(none)" && exitStatus != "0"
}

func judgeActivePlanStep(ctx context.Context, llm llmclient.Client, inspector Inspector, packet ctxpacket.WorkerPacket, completionClaim string) (workerstep.Evaluation, error) {
	attempt := workerstep.AttemptRecord{
		Prompt: buildStepEvaluationPrompt(packet, completionClaim),
	}
	respText, err := llm.Chat(ctx, []llmclient.Message{
		{Role: "system", Content: packet.BehaviorFrame.PromptText()},
		{Role: "user", Content: attempt.Prompt},
	})
	if err != nil {
		attempt.FinalError = fmt.Sprintf("step evaluation chat: %v", err)
		_ = captureStepEvaluationAttemptIfConfigured(inspector, attempt)
		return workerstep.Evaluation{}, err
	}
	attempt.RawResponse = respText
	eval, err := workerstep.Parse(respText)
	if err != nil {
		attempt.FinalError = fmt.Sprintf("parse step evaluation: %v", err)
		_ = captureStepEvaluationAttemptIfConfigured(inspector, attempt)
		return workerstep.Evaluation{}, err
	}
	attempt.Parsed = eval
	report := workerstep.Validate(eval)
	attempt.Validation = report
	if !report.Valid() {
		attempt.FinalError = fmt.Sprintf("validate step evaluation: %v", report.Error())
		_ = captureStepEvaluationAttemptIfConfigured(inspector, attempt)
		return workerstep.Evaluation{}, report.Error()
	}
	attempt.Accepted = true
	_ = captureStepEvaluationAttemptIfConfigured(inspector, attempt)
	return eval, nil
}

func buildStepEvaluationPrompt(packet ctxpacket.WorkerPacket, completionClaim string) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond with JSON only.",
			"Evaluate the current active planned step using the context packet.",
			"Use: {\"status\":\"in_progress|satisfied|blocked\",\"reason\":\"...\",\"summary\":\"...\"}.",
			"status must be satisfied only when the active step objective is already met by the current evidence.",
			"status must be blocked only when the active step cannot continue without replanning, a missing prerequisite, or a clearly blocking failure.",
			"Otherwise use in_progress.",
			"Judge from structured evidence already present in the packet; do not invent commands, outputs, or new facts.",
			"If a completion_claim is present, decide whether the current evidence supports it.",
		},
		"context_packet":   packet.RenderWithoutBehaviorFrame(),
		"completion_claim": blank(completionClaim, "(none)"),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func captureStepEvaluationAttemptIfConfigured(inspector Inspector, attempt workerstep.AttemptRecord) error {
	if inspector == nil {
		return nil
	}
	return inspector.CaptureStepEvaluationAttempt(attempt)
}
