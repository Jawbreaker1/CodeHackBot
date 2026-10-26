package webapp

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
)

// A progress reply sees the same live observations as the worker inspector,
// without copying its entire execution history into the model request.
type workerProgress struct {
	ID             string                   `json:"id"`
	Phase          string                   `json:"phase"`
	ActiveStep     string                   `json:"active_step"`
	PlanSummary    string                   `json:"plan_summary,omitempty"`
	EvidenceCount  int                      `json:"evidence_count"`
	LatestEvidence *assessment.EvidenceView `json:"latest_evidence,omitempty"`
	RecentOutput   string                   `json:"recent_output,omitempty"`
}

const webCoordinatorDisplayPrompt = `Return one JSON object with "text" (your plain-language reply; it may be empty only when requesting an advertised research tool), optional "display_artifact_refs" (up to three exact paths from available_images), and optional "revise_plan":true. Explain what the recorded evidence establishes, why the current step matters, and the most useful next step when relevant; keep the reply short and respect a different path chosen by the operator. If pending_plan is present and the operator directs a different course before workers start, set revise_plan:true and explain that you are preparing a revised proposal. Do not set it for a question about the plan, or when no plan is awaiting review. A revised proposal still needs the normal plan review and action permissions. Select images only when they help answer the operator; never invent a path. The application validates each reference and displays accepted images beneath your reply. If no image helps, omit display_artifact_refs. Do not put Markdown image syntax in text.`

type coordinatorChatReply struct {
	Text                string                  `json:"text"`
	Message             string                  `json:"message,omitempty"`
	DisplayArtifactRefs []string                `json:"display_artifact_refs,omitempty"`
	ReportFormat        assessment.ReportFormat `json:"report_format,omitempty"`
	ReportOutput        reportOutput            `json:"report_output,omitempty"`
	ContinueAssessment  bool                    `json:"continue_assessment,omitempty"`
	RevisePlan          bool                    `json:"revise_plan,omitempty"`
	Tool                *intake.ToolCall        `json:"tool,omitempty"`
}

func parseCoordinatorChatReply(raw string) (coordinatorChatReply, error) {
	var reply coordinatorChatReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		// Providers that do not honor structured control still return usable prose.
		reply.Text = strings.TrimSpace(raw)
	}
	if strings.TrimSpace(reply.Text) == "" {
		reply.Text = reply.Message
	}
	reply.Text = strings.TrimSpace(reply.Text)
	if reply.Text == "" && reply.Tool == nil {
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
func compactRunState(root string, state assessment.State, pending []string, workers map[string]workerView, mode approval.Mode, images []string, proposed *assessment.Decision) string {
	results := make([]string, 0, len(state.Results))
	for _, result := range state.Results {
		results = append(results, result.Task.ID+"="+result.Status+": "+conversationExcerpt(result.Summary, 240))
	}
	progress := make([]workerProgress, 0, len(workers))
	for _, worker := range workers {
		item := workerProgress{ID: worker.ID, Phase: worker.Phase, ActiveStep: conversationExcerpt(worker.ActiveStep, 240), PlanSummary: conversationExcerpt(worker.PlanSummary, 400), EvidenceCount: worker.EvidenceCount}
		if n := len(worker.Evidence); n > 0 {
			evidence := worker.Evidence[n-1]
			evidence.Command = conversationExcerpt(evidence.Command, 400)
			evidence.Summary = conversationExcerpt(evidence.Summary, 600)
			evidence.ArtifactURLs = nil
			item.LatestEvidence = &evidence
			item.RecentOutput = recentWorkerOutput(root, evidence.LogRefs)
		}
		progress = append(progress, item)
	}
	sort.Slice(progress, func(i, j int) bool { return progress[i].ID < progress[j].ID })
	var latestPlan, pendingPlan *assessment.PlanBrief
	if len(state.Plans) > 0 {
		brief := assessment.BriefPlan(state.Plans[len(state.Plans)-1])
		latestPlan = &brief
	}
	if proposed != nil {
		brief := assessment.BriefPlan(*proposed)
		pendingPlan = &brief
	}
	data, _ := json.Marshal(struct {
		ApprovalMode    approval.Mode         `json:"approval_mode"`
		Status          string                `json:"status"`
		Goal            string                `json:"goal"`
		Scope           string                `json:"scope"`
		Plans           int                   `json:"plans"`
		Results         []string              `json:"results"`
		Workers         []workerProgress      `json:"live_workers"`
		Pending         []string              `json:"pending_operator_actions"`
		AvailableImages []string              `json:"available_images,omitempty"`
		LatestPlan      *assessment.PlanBrief `json:"latest_plan,omitempty"`
		PendingPlan     *assessment.PlanBrief `json:"pending_plan,omitempty"`
		ModelCalls      int                   `json:"model_calls"`
	}{ApprovalMode: mode.Normalized(), Status: state.Status, Goal: state.Goal, Scope: state.Scope, Plans: len(state.Plans), Results: results, Workers: progress, Pending: pending, AvailableImages: images, LatestPlan: latestPlan, PendingPlan: pendingPlan, ModelCalls: state.Usage.Calls})
	return string(data)
}

// Live chat receives a bounded view of the saved tool output as well as the
// worker's latest interpretation. A short execution summary can end before the
// actual observations, especially when a wrapper reports an error after its
// underlying tool completed. These bytes are untrusted evidence, not commands.
func recentWorkerOutput(root string, refs []string) string {
	parts := make([]string, 0, 2)
	for _, suffix := range []string{".stdout", ".stderr"} {
		for _, ref := range refs {
			if !strings.HasSuffix(ref, suffix) {
				continue
			}
			rootReal, rootErr := filepath.EvalSymlinks(root)
			refReal, refErr := filepath.EvalSymlinks(ref)
			if rootErr != nil || refErr != nil || !pathWithin(rootReal, refReal) {
				continue
			}
			limit := int64(1400)
			if suffix == ".stderr" {
				limit = 400
			}
			output := readOutputTail(refReal, limit)
			if output != "" {
				parts = append(parts, strings.TrimPrefix(suffix, ".")+": "+output)
			}
			break
		}
	}
	return strings.Join(parts, " | ")
}

func readOutputTail(path string, limit int64) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return ""
	}
	start := max(int64(0), info.Size()-limit)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return ""
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	if start > 0 {
		return "[tail] " + text
	}
	return text
}

func conversationExcerpt(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > limit {
		return text[:limit-3] + "..."
	}
	return text
}

// Project a readable final statement without losing the complete model-authored
// conclusion retained in the plan and report.
func assessmentConclusion(state assessment.State) string {
	if len(state.Plans) == 0 {
		return ""
	}
	last := state.Plans[len(state.Plans)-1]
	if !last.Complete {
		return ""
	}
	if assessment.HasDeniedExecution(state) {
		text := []rune(strings.Join(strings.Fields(assessment.ReviewSummary(state)), " "))
		if len(text) > 360 {
			return string(text[:359]) + "…"
		}
		return string(text)
	}
	text := strings.TrimSpace(last.PlainSummary)
	if text == "" {
		text = last.Summary
	}
	text = strings.Join(strings.Fields(text), " ")
	characters := []rune(text)
	if len(characters) > 360 {
		return string(characters[:359]) + "…"
	}
	return text
}

func assessmentConclusionDetail(state assessment.State) string {
	if len(state.Plans) == 0 || !state.Plans[len(state.Plans)-1].Complete {
		return ""
	}
	if assessment.HasDeniedExecution(state) {
		return assessment.ReviewSummary(state)
	}
	return state.Plans[len(state.Plans)-1].Summary
}
