package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/workergoal"
)

func judgeGoalCompletion(ctx context.Context, llm llmclient.Client, inspector Inspector, packet ctxpacket.WorkerPacket, proposedAnswer string) (workergoal.Evaluation, error) {
	attempt := workergoal.AttemptRecord{
		Prompt: buildGoalEvaluationPrompt(packet, proposedAnswer),
	}
	completion, err := llm.Complete(ctx, []llmclient.Message{
		{Role: "system", Content: packet.BehaviorFrame.PromptText()},
		{Role: "user", Content: attempt.Prompt},
	}, llmclient.ChatOptions{Profile: llmclient.ProfileStructuredControl})
	attempt.ResponseSource = string(completion.Source)
	respText := completion.Text
	if err != nil {
		attempt.FinalError = fmt.Sprintf("goal evaluation chat: %v", err)
		if recordErr := captureGoalEvaluationAttemptIfConfigured(inspector, attempt); recordErr != nil {
			return workergoal.Evaluation{}, recordErr
		}
		return workergoal.Evaluation{}, err
	}
	attempt.RawResponse = respText

	eval, err := workergoal.Parse(respText)
	if err != nil {
		attempt.FinalError = fmt.Sprintf("parse goal evaluation: %v", err)
		if recordErr := captureGoalEvaluationAttemptIfConfigured(inspector, attempt); recordErr != nil {
			return workergoal.Evaluation{}, recordErr
		}
		return workergoal.Evaluation{}, err
	}
	attempt.Parsed = eval

	report := workergoal.Validate(eval)
	attempt.Validation = report
	if !report.Valid() {
		attempt.FinalError = fmt.Sprintf("validate goal evaluation: %v", report.Error())
		if recordErr := captureGoalEvaluationAttemptIfConfigured(inspector, attempt); recordErr != nil {
			return workergoal.Evaluation{}, recordErr
		}
		return workergoal.Evaluation{}, report.Error()
	}
	attempt.Accepted = true
	if recordErr := captureGoalEvaluationAttemptIfConfigured(inspector, attempt); recordErr != nil {
		return workergoal.Evaluation{}, recordErr
	}
	return eval, nil
}

func buildGoalEvaluationPrompt(packet ctxpacket.WorkerPacket, proposedAnswer string) string {
	payload := map[string]any{
		"role": "goal_evaluator",
		"instructions": []string{
			"Respond with JSON only.",
			"Evaluate whether the original worker goal and its done condition are fully satisfied by the recorded evidence.",
			"Use: {\"status\":\"in_progress|satisfied|blocked\",\"reason\":\"...\",\"summary\":\"...\"}.",
			"status must be satisfied only when the operator request is already answered by the current structured evidence.",
			"status must be blocked only when progress needs a missing prerequisite. A failed command alone does not establish a blocker; explain what remains so the worker can adapt.",
			"Otherwise use in_progress.",
			"Judge from structured evidence already present in the packet; do not invent commands, outputs, or new facts.",
			"If proposed_answer is present, evaluate that answer against the recorded evidence and the task's done condition. It is a model claim, not new execution evidence. An analysis answer need not be written to another file unless the task requires that artifact.",
			"When the task requires exact contents or values, compare the proposed answer with all requested data in output_evidence, including every output line. Missing requested content is in_progress even if a log contains it. output_summary and running_summary are lossy previews and cannot establish exactness.",
			"Plans, summaries, retrieved text and proposed answers are claims, not execution evidence. Judge the original goal even if the active plan omits requirements.",
			"plan_history records actual runtime planning transitions and their order relative to execution logs. It proves that a plan was recorded or revised, never that its intended actions happened or its target claims are true.",
			"Output assessments and signals are heuristic hints, not authoritative conclusions. Negative tests and nonzero exits may be valid evidence; interpret their actual content and provenance.",
			"Do not require needless extra work if the whole request and done condition are supported. State limitations; do not treat absence of evidence as a verified negative.",
		},
		"context_packet":  packet.RenderWithoutBehaviorFrame(),
		"proposed_answer": strings.TrimSpace(proposedAnswer),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func captureGoalEvaluationAttemptIfConfigured(inspector Inspector, attempt workergoal.AttemptRecord) error {
	if inspector == nil {
		return nil
	}
	return inspector.CaptureGoalEvaluationAttempt(attempt)
}
