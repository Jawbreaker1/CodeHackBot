package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/workeraction"
)

func reviewPlannedAction(ctx context.Context, llm llmclient.Client, inspector Inspector, packet ctxpacket.WorkerPacket, resp Response) (workeraction.Review, error) {
	attempt := workeraction.AttemptRecord{
		Prompt: buildActionReviewPrompt(packet, resp),
	}
	respText, err := llm.Chat(ctx, []llmclient.Message{
		{Role: "system", Content: packet.BehaviorFrame.PromptText()},
		{Role: "user", Content: attempt.Prompt},
	})
	if err != nil {
		attempt.FinalError = fmt.Sprintf("action review chat: %v", err)
		_ = captureActionReviewAttemptIfConfigured(inspector, attempt)
		return workeraction.Review{}, err
	}
	attempt.RawResponse = respText
	review, err := workeraction.Parse(respText)
	if err != nil {
		attempt.FinalError = fmt.Sprintf("parse action review: %v", err)
		_ = captureActionReviewAttemptIfConfigured(inspector, attempt)
		return workeraction.Review{}, err
	}
	attempt.Parsed = review
	report := workeraction.Validate(review)
	attempt.Validation = report
	if !report.Valid() {
		attempt.FinalError = fmt.Sprintf("validate action review: %v", report.Error())
		_ = captureActionReviewAttemptIfConfigured(inspector, attempt)
		return workeraction.Review{}, report.Error()
	}
	attempt.Accepted = true
	_ = captureActionReviewAttemptIfConfigured(inspector, attempt)
	return review, nil
}

func buildActionReviewPrompt(packet ctxpacket.WorkerPacket, resp Response) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond with JSON only.",
			"Review the proposed action for the current active planned step.",
			"Use: {\"decision\":\"execute|revise|blocked\",\"reason\":\"...\"}.",
			"Choose execute when the action is a reasonable next action for the active step, even if it may take time.",
			"Choose revise when the action is avoidably overbuilt, redundant, or poorly scoped for the active step and a simpler or cleaner action should be chosen first.",
			"Choose blocked only when the active step cannot proceed without replanning or a missing prerequisite.",
			"Prefer revise when the action bundles multiple concerns that should be separated, such as running a command and immediately re-reading a saved file in the same step.",
			"Prefer revise when the action invents custom output-file scaffolding that is not necessary to advance the active step.",
			"Prefer revise when the action is materially broader than needed to establish the next evidence for the active step.",
			"Do not require the absolute smallest action; long-running but well-scoped actions may still be execute.",
			"Do not suggest exact replacement commands.",
			"Do not judge based on hidden preferences for a specific tool; judge fit, scope, and whether the action is unnecessarily elaborate for the active step.",
		},
		"context_packet": packet.RenderWithoutBehaviorFrame(),
		"proposed_action": map[string]any{
			"command":   strings.TrimSpace(resp.Command),
			"use_shell": resp.UseShell,
		},
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func captureActionReviewAttemptIfConfigured(inspector Inspector, attempt workeraction.AttemptRecord) error {
	if inspector == nil {
		return nil
	}
	return inspector.CaptureActionReviewAttempt(attempt)
}
