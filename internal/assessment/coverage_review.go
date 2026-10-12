package assessment

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// A second opinion checks the proposed plan against the latest worker
// observations. It can ask for one revision, but it never acts on the target
// or turns an unverified lead into a finding.
type coverageOmission struct {
	Question  string   `json:"question"`
	Evidence  []string `json:"evidence"`
	NextCheck string   `json:"next_check"`
}

type coverageReview struct {
	Omissions []coverageOmission `json:"material_omissions"`
}

type coverageQuestions struct {
	Questions []coverageOmission `json:"questions"`
}

const coverageQuestionInstructions = "Read the latest worker observations without a proposed plan. Use prior_worker_checks to avoid proposing a check already performed; a prior result settles only what it actually tested. Recent operator messages are authoritative about whether to continue testing or finish; unanswered questions may instead be report gaps. Return JSON only: {\"questions\":[{\"question\":\"...\",\"evidence\":[\"exact recorded ref\"],\"next_check\":\"...\"}]}. Identify at most three distinct, materially consequential unanswered questions supported by visible observations, ordered by plausible impact and the value of an in-scope discriminating check rather than ease or source order. Distinguish separate values, roles, and trust boundaries within a workflow; a conclusion about one does not settle another. Prefer a bounded check over a generic inventory or repeated low-impact test. Do not mistake an existing finding title, an open lead, or a general untested-area statement for a completed test. Cite exact recorded refs; a ref is a pointer, not proof of unseen contents. Do not invent a vulnerability, claim exfiltration, request a prohibited action, or expand scope. An empty array is valid."

const coverageReviewInstructions = "Compare the independent unanswered questions with the coordinator's proposed tasks. Use prior_worker_checks to avoid requesting work already performed; a prior result settles only what it actually tested. Respect recent operator messages: if the operator directed the assessment to finish after current work, do not request another worker merely to resolve an open question. A completed report may state that question as a gap. Return JSON only: {\"material_omissions\":[{\"question\":\"...\",\"evidence\":[\"exact recorded ref\"],\"next_check\":\"...\"}]}. Return at most two omissions, ordered by plausible impact and the value of a bounded in-scope check. Include a question only when that check could materially change the assessment and the proposed tasks do not answer it. A task covers a question only if its stated goal and done condition would resolve that specific question; mentioning the same page, workflow, value class, or risk category is not enough. An open_leads entry or a general limitation in gaps is not a test or a reason to skip an available bounded check; accept a deferral when it follows the operator's direction or explains why that particular check is infeasible or less valuable now. You may notice a material omission beyond the independent list, but support it with visible observations and exact recorded refs. Do not invent a vulnerability, claim exfiltration, request a prohibited action, or expand scope. An empty array is valid."

type coverageResult struct {
	TaskID    string   `json:"task_id"`
	Goal      string   `json:"goal"`
	Status    string   `json:"status"`
	Summary   string   `json:"summary"`
	LeadHints []string `json:"lead_hints,omitempty"`
	Evidence  []struct {
		Summary string   `json:"summary"`
		Refs    []string `json:"refs"`
	} `json:"evidence"`
}

type coveragePacket struct {
	Goal                 string               `json:"goal"`
	Scope                string               `json:"scope"`
	OperatorMessages     []string             `json:"recent_operator_messages,omitempty"`
	Results              []coverageResult     `json:"latest_worker_results"`
	PriorChecks          []coveragePriorCheck `json:"prior_worker_checks,omitempty"`
	OpenLeads            []Lead               `json:"prior_open_leads,omitempty"`
	IndependentQuestions []coverageOmission   `json:"independent_questions,omitempty"`
	Proposed             *Decision            `json:"proposed_decision,omitempty"`
}

type coveragePriorCheck struct {
	TaskID  string   `json:"task_id"`
	Goal    string   `json:"goal"`
	Status  string   `json:"status"`
	Summary string   `json:"summary"`
	Refs    []string `json:"refs,omitempty"`
}

func coverageEvidencePacket(state State) (coveragePacket, map[string]bool) {
	latest := map[string]bool{}
	if len(state.Plans) > 0 {
		plan := state.Plans[len(state.Plans)-1]
		ids := plan.ApprovedTaskIDs
		if len(ids) == 0 && len(plan.SkippedTaskIDs) == 0 {
			for _, task := range plan.Tasks {
				ids = append(ids, task.ID)
			}
		}
		for _, id := range ids {
			latest[id] = true
		}
	}
	var results []coverageResult
	payload := coveragePacket{}
	knownRefs := map[string]bool{}
	for _, result := range state.Results {
		if !latest[result.Task.ID] {
			prior := coveragePriorCheck{TaskID: result.Task.ID, Goal: promptExcerpt(result.Task.Goal, 300), Status: result.Status, Summary: promptExcerptEnds(result.Summary, 1200)}
			for _, evidence := range result.Evidence {
				for _, ref := range modelEvidenceRefs(evidence.LogRefs, evidence.ArtifactRefs) {
					if len(prior.Refs) < 3 {
						prior.Refs = append(prior.Refs, ref)
					}
				}
			}
			payload.PriorChecks = append(payload.PriorChecks, prior)
			continue
		}
		card := coverageResult{TaskID: result.Task.ID, Goal: promptExcerpt(result.Task.Goal, 500), Status: result.Status, Summary: promptExcerptEnds(result.Summary, 10000), LeadHints: boundedStrings(result.LeadHints, 6, 800)}
		for _, evidence := range result.Evidence {
			refs := modelEvidenceRefs(evidence.LogRefs, evidence.ArtifactRefs)
			for _, ref := range refs {
				knownRefs[ref] = true
			}
			if len(card.Evidence) == 10 {
				continue
			}
			card.Evidence = append(card.Evidence, struct {
				Summary string   `json:"summary"`
				Refs    []string `json:"refs"`
			}{Summary: promptExcerptEnds(evidence.OutputSummary, 800), Refs: boundedStrings(refs, 6, 500)})
		}
		results = append(results, card)
	}
	payload.Goal, payload.Scope, payload.Results = state.Goal, state.Scope, results
	if len(state.OperatorMessages) > 0 {
		messages := state.OperatorMessages
		if len(messages) > 4 {
			messages = messages[len(messages)-4:]
		}
		payload.OperatorMessages = boundedStrings(messages, 4, 1200)
	}
	if len(payload.PriorChecks) > 12 {
		payload.PriorChecks = payload.PriorChecks[len(payload.PriorChecks)-12:]
	}
	for _, prior := range payload.PriorChecks {
		for _, ref := range prior.Refs {
			knownRefs[ref] = true
		}
	}
	if len(state.Plans) > 0 {
		payload.OpenLeads = state.Plans[len(state.Plans)-1].OpenLeads
	}
	return payload, knownRefs
}

func coverageReviewInput(state State, decision Decision, questions ...coverageOmission) ([]byte, map[string]bool) {
	payload, knownRefs := coverageEvidencePacket(state)
	payload.Proposed = &decision
	payload.IndependentQuestions = questions
	data, _ := json.Marshal(payload)
	return data, knownRefs
}

func boundedStrings(values []string, count, bytes int) []string {
	if len(values) > count {
		values = values[:count]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, promptExcerpt(value, bytes))
	}
	return out
}

func (c Coordinator) reviewPlanCoverage(ctx context.Context, root string, round, proposal int, state State, decision Decision) string {
	payload, knownRefs := coverageEvidencePacket(state)
	hasLatestEvidence := false
	for _, result := range payload.Results {
		for _, evidence := range result.Evidence {
			hasLatestEvidence = hasLatestEvidence || len(evidence.Refs) > 0
		}
	}
	if !hasLatestEvidence {
		return ""
	}
	stem := filepath.Join(root, fmt.Sprintf("coordinator-%02d-coverage", round))
	if proposal > 0 {
		stem += fmt.Sprintf("-revision-%02d", proposal)
	}
	var questions []coverageOmission
	questionInput, _ := json.Marshal(payload)
	if len(questionInput)+len(coverageQuestionInstructions) <= c.LLM.InputByteLimit() {
		messages := []llmclient.Message{{Role: "system", Content: coverageQuestionInstructions}, {Role: "user", Content: string(questionInput)}}
		if err := saveJSON(stem+"-questions-request.json", messages); err == nil {
			response, err := c.LLM.ChatStructured(ctx, messages)
			if err != nil {
				_ = saveJSON(stem+"-questions-error.json", err.Error())
			} else {
				_ = saveJSON(stem+"-questions-response.json", response)
				var parsed coverageQuestions
				if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &parsed); err != nil {
					_ = saveJSON(stem+"-questions-error.json", "parse independent questions: "+err.Error())
				} else {
					questions = validCoverageQuestions(parsed.Questions, knownRefs, 3)
				}
			}
		}
	}
	input, _ := coverageReviewInput(state, decision, questions...)
	if len(input)+len(coverageReviewInstructions) > c.LLM.InputByteLimit() {
		_ = saveJSON(stem+"-error.json", "coverage comparison exceeds model input budget")
		return ""
	}
	messages := []llmclient.Message{{Role: "system", Content: coverageReviewInstructions}, {Role: "user", Content: string(input)}}
	if err := saveJSON(stem+"-request.json", messages); err != nil {
		return ""
	}
	response, err := c.LLM.ChatStructured(ctx, messages)
	if err != nil {
		_ = saveJSON(stem+"-error.json", err.Error())
		return ""
	}
	if err := saveJSON(stem+"-response.json", response); err != nil {
		return ""
	}
	var review coverageReview
	if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &review); err != nil {
		_ = saveJSON(stem+"-error.json", "parse coverage review: "+err.Error())
		return ""
	}
	var notes []string
	for _, omission := range validCoverageQuestions(review.Omissions, knownRefs, 2) {
		notes = append(notes, fmt.Sprintf("Question: %s; evidence: %s; bounded next check: %s", omission.Question, strings.Join(omission.Evidence, ", "), omission.NextCheck))
	}
	return strings.Join(notes, "\n")
}

func validCoverageQuestions(candidates []coverageOmission, knownRefs map[string]bool, limit int) []coverageOmission {
	var validQuestions []coverageOmission
	for _, candidate := range candidates {
		if len(validQuestions) == limit {
			break
		}
		if len(candidate.Evidence) == 0 || len(candidate.Evidence) > 3 || len(candidate.Question) > 500 || len(candidate.NextCheck) > 500 || strings.TrimSpace(candidate.Question) == "" || strings.TrimSpace(candidate.NextCheck) == "" {
			continue
		}
		valid := true
		for _, ref := range candidate.Evidence {
			if !knownRefs[ref] {
				valid = false
				break
			}
		}
		if valid {
			validQuestions = append(validQuestions, candidate)
		}
	}
	return validQuestions
}
