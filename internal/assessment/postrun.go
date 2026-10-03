package assessment

import (
	"encoding/json"
	"strings"
)

// PostRunPrompt is shared by the web and terminal coordinator conversations.
// The model decides whether a user is asking a question, requesting another
// bounded planning round, or exporting a saved report.
const PostRunPrompt = `Return one JSON object with a nonempty "text" field containing your plain-language reply. Include control fields when they match the operator request. The assessment is not currently running; its saved state may be complete, incomplete, stopped, paused, or interrupted. For a request to resume paused or interrupted work, or to investigate further within the recorded scope, set "continue_assessment":true. Briefly tell the operator that you will review saved evidence and propose the next bounded plan for review. The application reopens the same session and has workers execute only after normal plan review and action approvals; it never blindly repeats an unfinished action. Do not claim work has begun or finished in this chat reply. A question or discussion about saved results does not require continuation. If the operator asks to generate a report, set "report_format" to exactly "owasp-wstg" or "ptes" and "report_output" to "pdf" or "markdown" (the default); do not set continue_assessment for a report export. A PDF follow-up uses the recorded latest_report format. The application renders the selected template from saved findings and presents the artifact. Do not claim to have generated a report unless you set the report fields. The template contains an introduction, executive summary, recorded findings, task coverage, limitations, and reporting basis; it does not automatically map each OWASP or PTES test category. Describe only sections and tests actually present. These are reporting structures, not certification or proof that every standard test was performed. Do not invent a WSTG test ID, CVSS score, finding, or validation. If final_review_pending is true, say clearly that the assessment ended before the coordinator reviewed its last worker result.`

// PostRunFindingsContext keeps both UI adapters grounded in the saved final
// findings without copying the full execution transcript into each chat turn.
func PostRunFindingsContext(state State) string {
	var gaps []string
	lastResult, finalReviewPending := LatestUnreviewedResult(state)
	if len(state.Plans) > 0 && !finalReviewPending {
		gaps = ReportGaps(state)
	}
	latestResult := ""
	if finalReviewPending {
		latestResult = lastResult.Task.ID + " — " + lastResult.Status + ": " + postRunExcerpt(lastResult.Summary, 1200)
	}
	data, _ := json.Marshal(struct {
		Findings           []Finding `json:"current_findings"`
		Gaps               []string  `json:"unresolved_gaps"`
		FinalReviewPending bool      `json:"final_review_pending"`
		LatestResult       string    `json:"latest_worker_result,omitempty"`
	}{Findings: CurrentFindings(state.Plans), Gaps: gaps, FinalReviewPending: finalReviewPending, LatestResult: latestResult})
	return string(data)
}

func postRunExcerpt(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > limit {
		return value[:limit-3] + "..."
	}
	return value
}
