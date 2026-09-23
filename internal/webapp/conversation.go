package webapp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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

const webCoordinatorDisplayPrompt = `Return one JSON object with "text" (your plain-language reply) and optional "display_artifact_refs" (up to three exact paths from available_images in the current assessment state). Select images only when they help answer the operator; never invent a path. The application validates each reference and displays accepted images beneath your reply. If no image helps, omit display_artifact_refs. Do not put Markdown image syntax in text.`

type coordinatorChatReply struct {
	Text                string   `json:"text"`
	DisplayArtifactRefs []string `json:"display_artifact_refs,omitempty"`
}

func parseCoordinatorChatReply(raw string) (coordinatorChatReply, error) {
	var reply coordinatorChatReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		// Providers that do not honor structured control still return usable prose.
		reply.Text = strings.TrimSpace(raw)
	}
	reply.Text = strings.TrimSpace(reply.Text)
	if reply.Text == "" {
		return coordinatorChatReply{}, fmt.Errorf("coordinator response was empty")
	}
	return reply, nil
}

func recordedImageRefs(root string, state assessment.State, workers map[string]workerView) []string {
	const limit = 16
	refs := make([]string, 0, limit)
	seen := make(map[string]bool)
	add := func(items []string) {
		for _, ref := range items {
			if len(refs) == limit {
				return
			}
			if !seen[ref] && validPresentedImage(root, ref) {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	// Current worker captures lead the list; completed task evidence follows.
	ids := make([]string, 0, len(workers))
	for id := range workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for i := len(workers[id].Evidence) - 1; i >= 0; i-- {
			add(workers[id].Evidence[i].ArtifactRefs)
		}
	}
	for i := len(state.Results) - 1; i >= 0; i-- {
		for j := len(state.Results[i].Evidence) - 1; j >= 0; j-- {
			add(state.Results[i].Evidence[j].ArtifactRefs)
		}
	}
	return refs
}

func imageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

// Called under run.mu so completed results and live workers form one snapshot.
func compactRunState(state assessment.State, pending []string, workers map[string]workerView, mode approval.Mode, images []string) string {
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
		ApprovalMode    approval.Mode    `json:"approval_mode"`
		Status          string           `json:"status"`
		Goal            string           `json:"goal"`
		Scope           string           `json:"scope"`
		Plans           int              `json:"plans"`
		Results         []string         `json:"results"`
		Workers         []workerProgress `json:"live_workers"`
		Pending         []string         `json:"pending_operator_actions"`
		AvailableImages []string         `json:"available_images,omitempty"`
		ModelCalls      int              `json:"model_calls"`
	}{ApprovalMode: mode.Normalized(), Status: state.Status, Goal: state.Goal, Scope: state.Scope, Plans: len(state.Plans), Results: results, Workers: progress, Pending: pending, AvailableImages: images, ModelCalls: state.Usage.Calls})
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
