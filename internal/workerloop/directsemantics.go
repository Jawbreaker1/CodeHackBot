package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerdirect"
)

func evaluateDirectExecution(ctx context.Context, llm llmclient.Client, inspector Inspector, packet ctxpacket.WorkerPacket) StepEvaluation {
	latest := packet.LatestExecutionResult
	if ctxpacket.IsInterruptedResult(latest) {
		return StepEvaluation{
			Status: StepInProgress,
			Reason: "direct execution was interrupted before completion",
		}
	}
	if ctxpacket.ImpactOfResult(latest) == ctxpacket.ResultImpactBlocking {
		return StepEvaluation{
			Status: StepBlocked,
			Reason: "latest result indicates the direct request is blocked",
		}
	}
	if blocked, reason := clearDirectExecutionBlock(packet); blocked {
		return StepEvaluation{
			Status: StepBlocked,
			Reason: reason,
		}
	}
	if !hasStepEvidence(packet) {
		return StepEvaluation{
			Status: StepInProgress,
			Reason: "no supported execution evidence yet",
		}
	}
	if satisfied, reason, summary := clearDirectExecutionSatisfaction(packet); satisfied {
		return StepEvaluation{
			Status:  StepSatisfied,
			Reason:  reason,
			Summary: summary,
		}
	}

	eval, err := judgeDirectExecution(ctx, llm, inspector, packet)
	if err != nil {
		return StepEvaluation{
			Status: StepInProgress,
			Reason: "need one more turn to confirm the direct request result",
		}
	}
	return StepEvaluation{
		Status:  StepStatus(eval.Status),
		Reason:  eval.Reason,
		Summary: eval.Summary,
	}
}

func clearDirectExecutionBlock(packet ctxpacket.WorkerPacket) (bool, string) {
	evidence := ctxpacket.ActiveTruthResult(packet.LatestExecutionResult, packet.RelevantRecentResults)
	if strings.TrimSpace(evidence.Action) == "" {
		return false, ""
	}
	if ctxpacket.IsInterruptedResult(evidence) {
		return false, ""
	}
	if ctxpacket.ImpactOfResult(evidence) == ctxpacket.ResultImpactBlocking {
		return true, "latest result indicates the direct request is blocked"
	}
	if hasSignal(evidence.Signals, "incorrect_password") {
		return true, "latest result indicates the direct request needs missing credentials"
	}
	return false, ""
}

func clearDirectExecutionSatisfaction(packet ctxpacket.WorkerPacket) (bool, string, string) {
	evidence := ctxpacket.ActiveTruthResult(packet.LatestExecutionResult, packet.RelevantRecentResults)
	if strings.TrimSpace(evidence.Action) == "" {
		return false, "", ""
	}
	if ctxpacket.IsInterruptedResult(evidence) || ctxpacket.ImpactOfResult(evidence) != ctxpacket.ResultImpactInformational {
		return false, "", ""
	}
	if strings.TrimSpace(evidence.Assessment) != "success" {
		return false, "", ""
	}
	evidenceText := preferredExecutionEvidence(evidence)
	if strings.TrimSpace(evidenceText) == "" || strings.TrimSpace(evidenceText) == "(none)" {
		return false, "", ""
	}
	reason := "latest successful execution already answers the direct request"
	return true, reason, ""
}

func hasSignal(signals []string, want string) bool {
	for _, signal := range signals {
		if strings.TrimSpace(signal) == want {
			return true
		}
	}
	return false
}

func judgeDirectExecution(ctx context.Context, llm llmclient.Client, inspector Inspector, packet ctxpacket.WorkerPacket) (workerdirect.Evaluation, error) {
	attempt := workerdirect.AttemptRecord{
		Prompt: buildDirectEvaluationPrompt(packet),
	}
	respText, err := llm.Chat(ctx, []llmclient.Message{
		{Role: "system", Content: packet.BehaviorFrame.PromptText()},
		{Role: "user", Content: attempt.Prompt},
	})
	if err != nil {
		attempt.FinalError = fmt.Sprintf("direct evaluation chat: %v", err)
		_ = captureDirectEvaluationAttemptIfConfigured(inspector, attempt)
		return workerdirect.Evaluation{}, err
	}
	attempt.RawResponse = respText

	eval, err := workerdirect.Parse(respText)
	if err != nil {
		attempt.FinalError = fmt.Sprintf("parse direct evaluation: %v", err)
		_ = captureDirectEvaluationAttemptIfConfigured(inspector, attempt)
		return workerdirect.Evaluation{}, err
	}
	attempt.Parsed = eval

	report := workerdirect.Validate(eval)
	attempt.Validation = report
	if !report.Valid() {
		attempt.FinalError = fmt.Sprintf("validate direct evaluation: %v", report.Error())
		_ = captureDirectEvaluationAttemptIfConfigured(inspector, attempt)
		return workerdirect.Evaluation{}, report.Error()
	}
	attempt.Accepted = true
	_ = captureDirectEvaluationAttemptIfConfigured(inspector, attempt)
	return eval, nil
}

func buildDirectEvaluationPrompt(packet ctxpacket.WorkerPacket) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond with JSON only.",
			"Evaluate whether the direct_execution operator request is already satisfied by the current evidence.",
			"Use: {\"status\":\"in_progress|satisfied|blocked\",\"reason\":\"...\",\"summary\":\"...\"}.",
			"status must be satisfied only when the operator request is already answered by the current structured evidence.",
			"status must be blocked only when the request cannot continue without a missing prerequisite or a clearly blocking failure.",
			"Otherwise use in_progress.",
			"Judge from structured evidence already present in the packet; do not invent commands, outputs, or new facts.",
			"Be strict about direct_execution: if one simple successful action already answered the request, return satisfied.",
		},
		"context_packet": packet.RenderWithoutBehaviorFrame(),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func captureDirectEvaluationAttemptIfConfigured(inspector Inspector, attempt workerdirect.AttemptRecord) error {
	if inspector == nil {
		return nil
	}
	return inspector.CaptureDirectEvaluationAttempt(attempt)
}
