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
	instructions := []string{
		"Respond with JSON only.",
		"Evaluate whether the original worker goal and its done condition are fully satisfied by the recorded evidence.",
		"Use: {\"status\":\"in_progress|satisfied|blocked\",\"reason\":\"...\",\"summary\":\"...\",\"lead_hints\":[]}.",
		"status must be satisfied only when the operator request is already answered by the current structured evidence.",
		"status must be blocked only when progress needs a missing prerequisite. A failed command alone does not establish a blocker; explain what remains so the worker can adapt.",
		"Otherwise use in_progress.",
		"Judge from structured evidence already present in the packet; do not invent commands, outputs, or new facts.",
		"If proposed_answer is present, evaluate that answer against the recorded evidence and the task's done condition. It is a model claim, not new execution evidence. An analysis answer need not be written to another file unless the task requires that artifact.",
		"Judge whether the substantive goal and done condition are supported. A missing or imperfect lead_hints handoff, report heading, or other presentation detail does not make an otherwise supported investigation in_progress. On a satisfied result, independently supply up to three concise, materially distinct unresolved lead_hints when recorded evidence supports them. Distinguish separate sensitive values in one workflow by source and lifetime: checking a returned session credential does not settle an earlier URL, form, or redirect value. Prioritize an untraced material trust boundary over a routine lower-impact inventory. Keep each hint under 800 UTF-8 bytes; cite an exact recorded log or artifact reference, explain why it may matter, and name one bounded next check. Treat a possible data flow as a hypothesis until the entry, destination, and transmission are observed. Do not invent leads or turn them into findings; an empty array is valid.",
		"When the task requires exact contents or values, compare the proposed answer with the requested data in output_evidence or an exact registered context_recall slice, including every requested output line. Missing requested content is in_progress even if a log reference exists. output_summary and running_summary are lossy previews and cannot establish exactness.",
		"A context_recall with an exact registered ref, stdout or stderr stream, and byte offset is a bounded read from that original command log. Its content may support an exact observation within that slice; a context_recall index search is only navigation, not evidence. The source command's target and status still determine what the observation establishes.",
		"Plans, summaries, external retrieved text and proposed answers are claims, not execution evidence. Judge the original goal even if the active plan omits requirements.",
		"plan_history records actual runtime planning transitions and their order relative to execution logs. It proves that a plan was recorded or revised, never that its intended actions happened or its target claims are true.",
		"Output assessments and signals are heuristic hints, not authoritative conclusions. Negative tests and nonzero exits may be valid evidence; interpret their actual content and provenance.",
		"Do not require needless extra work if the whole request and done condition are supported. State limitations; do not treat absence of evidence as a verified negative.",
	}
	payload := map[string]any{
		"role":            "goal_evaluator",
		"instructions":    instructions,
		"context_packet":  packet.RenderWithoutBehaviorFrame(),
		"proposed_answer": strings.TrimSpace(proposedAnswer),
	}
	if claim := strings.TrimSpace(packet.BehaviorFrame.Parameters["verification_claim"]); claim != "" {
		payload["verification_claim"] = claim
		payload["alternative_explanation"] = packet.BehaviorFrame.Parameters["verification_alternative"]
		instructions = append(instructions,
			"This is a challenge task. Include verification:{verdict:'supported|refuted|inconclusive',alternative_result:'what the independent check showed about the alternative',reason:'why the verdict follows',evidence:['exact execution log path']} in the JSON response.",
			"Use supported only when your own recorded execution tests the claim and rules out the stated alternative. Use refuted when evidence contradicts the claim; inconclusive when a safe check was unavailable or results cannot distinguish them. Cite your own execution log, never a worker-written note or prior worker's log.",
		)
		payload["instructions"] = instructions
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

func boundedLeadHints(hints []string) []string {
	var accepted []string
	for _, hint := range hints {
		hint = strings.TrimSpace(hint)
		if hint == "" || len(hint) > 800 {
			continue
		}
		accepted = append(accepted, hint)
		if len(accepted) == 3 {
			break
		}
	}
	return accepted
}

// The worker and its independent goal evaluator can notice different adjacent
// questions. Preserve both ranked views for the coordinator instead of letting
// a full worker handoff silently discard the evaluator's observations.
func mergeLeadHints(worker, evaluator []string) []string {
	worker = boundedLeadHints(worker)
	evaluator = boundedLeadHints(evaluator)
	var merged []string
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		for _, list := range [][]string{evaluator, worker} {
			if i >= len(list) || seen[list[i]] {
				continue
			}
			seen[list[i]] = true
			merged = append(merged, list[i])
		}
	}
	return merged
}
