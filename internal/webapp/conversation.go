package webapp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

// A progress reply sees the same live observations as the worker inspector,
// without copying its entire execution history into the model request.
type workerProgress struct {
	ID             string                   `json:"id"`
	Phase          string                   `json:"phase"`
	ActiveStep     string                   `json:"active_step"`
	EvidenceCount  int                      `json:"evidence_count"`
	LatestEvidence *assessment.EvidenceView `json:"latest_evidence,omitempty"`
}

// Called under run.mu so completed results and live workers form one snapshot.
func compactRunState(state assessment.State, pending []string, workers map[string]workerView, mode approval.Mode) string {
	results := make([]string, 0, len(state.Results))
	for _, result := range state.Results {
		results = append(results, result.Task.ID+"="+result.Status+": "+conversationExcerpt(result.Summary, 240))
	}
	progress := make([]workerProgress, 0, len(workers))
	for _, worker := range workers {
		item := workerProgress{ID: worker.ID, Phase: worker.Phase, ActiveStep: conversationExcerpt(worker.ActiveStep, 240), EvidenceCount: worker.EvidenceCount}
		if n := len(worker.Evidence); n > 0 {
			evidence := worker.Evidence[n-1]
			evidence.Command = conversationExcerpt(evidence.Command, 400)
			evidence.Summary = conversationExcerpt(evidence.Summary, 600)
			evidence.ArtifactURLs = nil
			item.LatestEvidence = &evidence
		}
		progress = append(progress, item)
	}
	sort.Slice(progress, func(i, j int) bool { return progress[i].ID < progress[j].ID })
	data, _ := json.Marshal(struct {
		ApprovalMode approval.Mode    `json:"approval_mode"`
		Status       string           `json:"status"`
		Goal         string           `json:"goal"`
		Scope        string           `json:"scope"`
		Plans        int              `json:"plans"`
		Results      []string         `json:"results"`
		Workers      []workerProgress `json:"live_workers"`
		Pending      []string         `json:"pending_operator_actions"`
		ModelCalls   int              `json:"model_calls"`
	}{ApprovalMode: mode.Normalized(), Status: state.Status, Goal: state.Goal, Scope: state.Scope, Plans: len(state.Plans), Results: results, Workers: progress, Pending: pending, ModelCalls: state.Usage.Calls})
	return string(data)
}

func conversationExcerpt(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > limit {
		return text[:limit-3] + "..."
	}
	return text
}

// Project the final model-authored conclusion without manufacturing a new chat
// turn or relying on the bounded event feed retaining the final plan event.
func assessmentConclusion(state assessment.State) string {
	if len(state.Plans) == 0 {
		return ""
	}
	last := state.Plans[len(state.Plans)-1]
	if !last.Complete {
		return ""
	}
	return last.Summary
}
