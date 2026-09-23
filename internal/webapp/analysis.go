package webapp

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

// analysisView is a read model for the separate analysis surface. It is
// derived from persisted assessment state and never becomes a second source of
// truth for findings or execution evidence.
type analysisView struct {
	Kind             string                   `json:"kind"`
	ID               string                   `json:"id"`
	Customer         string                   `json:"customer,omitempty"`
	Goal             string                   `json:"goal,omitempty"`
	Scope            string                   `json:"scope,omitempty"`
	Model            string                   `json:"model,omitempty"`
	ReportURL        string                   `json:"report_url,omitempty"`
	Status           string                   `json:"status"`
	Summary          string                   `json:"summary,omitempty"`
	Conclusion       string                   `json:"conclusion,omitempty"`
	ConclusionDetail string                   `json:"conclusion_detail,omitempty"`
	SessionCount     int                      `json:"session_count"`
	Risk             analysisRisk             `json:"risk"`
	Findings         []analysisFinding        `json:"findings"`
	NextActions      []string                 `json:"next_actions"`
	Gaps             []string                 `json:"gaps"`
	Sessions         []analysisSessionSummary `json:"sessions,omitempty"`
	GeneratedAt      time.Time                `json:"generated_at"`
}

type analysisRisk struct {
	Critical   int `json:"critical"`
	High       int `json:"high"`
	Medium     int `json:"medium"`
	Low        int `json:"low"`
	Info       int `json:"info"`
	Unrated    int `json:"unrated"`
	Candidates int `json:"candidates"`
	Reproduced int `json:"reproduced"`
}

type analysisFinding struct {
	SessionID        string   `json:"session_id,omitempty"`
	Title            string   `json:"title"`
	Status           string   `json:"status"`
	Severity         string   `json:"severity,omitempty"`
	Confidence       string   `json:"confidence,omitempty"`
	Priority         string   `json:"priority"`
	PriorityScore    int      `json:"priority_score"`
	PriorityReason   string   `json:"priority_reason"`
	ValidationTask   string   `json:"validation_task,omitempty"`
	Impact           string   `json:"impact"`
	Steps            []string `json:"steps"`
	Evidence         []string `json:"evidence"`
	Remediation      []string `json:"remediation"`
	CVEIDs           []string `json:"cve_ids,omitempty"`
	AffectedSoftware []string `json:"affected_software,omitempty"`
	References       []string `json:"references,omitempty"`
}

type analysisSessionSummary struct {
	ID        string    `json:"id"`
	Goal      string    `json:"goal"`
	Status    string    `json:"status"`
	Model     string    `json:"model,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	ReportURL string    `json:"report_url"`
}

type analysisFindingInput struct {
	SessionID string
	Finding   assessment.Finding
}

func (r *run) analysis() analysisView {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return buildAnalysis(r.id, r.customer, r.state, nil)
}

func buildAnalysis(id, customer string, state assessment.State, inputs []analysisFindingInput) analysisView {
	view := analysisView{
		Kind: "assessment", ID: id, Customer: customer, Goal: state.Goal,
		Scope: state.Scope, Model: state.Model, Status: state.Status, SessionCount: 1,
		GeneratedAt: time.Now().UTC(),
	}
	view.ReportURL = "/api/v1/assessments/" + id + "/report"
	if len(inputs) == 0 {
		for _, finding := range assessment.CurrentFindings(state.Plans) {
			inputs = append(inputs, analysisFindingInput{SessionID: id, Finding: finding})
		}
	}
	view.Findings = prioritizeFindings(inputs)
	view.Risk = summarizeRisk(view.Findings)
	view.Gaps = latestGaps(state.Plans)
	view.Conclusion = assessmentConclusion(state)
	view.ConclusionDetail = assessmentConclusionDetail(state)
	view.Summary = analysisSummary(view.Status, view.Risk, len(view.Findings), len(view.Gaps))
	view.NextActions = nextActions(view.Findings, view.Gaps)
	return view
}

func buildCustomerAnalysis(id string, sessions []analysisView) analysisView {
	view := analysisView{Kind: "customer", ID: id, Status: "no_sessions", ReportURL: "/api/v1/customers/" + id + "/report", GeneratedAt: time.Now().UTC()}
	var inputs []analysisFindingInput
	for _, session := range sessions {
		view.Sessions = append(view.Sessions, analysisSessionSummary{ID: session.ID, Goal: session.Goal, Status: session.Status, Model: session.Model, ReportURL: "/api/v1/assessments/" + session.ID + "/report"})
		if session.Status == "running" || session.Status == "starting" {
			view.Status = "active"
		} else if view.Status == "no_sessions" {
			view.Status = session.Status
		}
		for _, finding := range session.Findings {
			inputs = append(inputs, analysisFindingInput{SessionID: finding.SessionID, Finding: finding.toFinding()})
		}
	}
	view.SessionCount = len(sessions)
	if len(sessions) == 0 {
		view.Summary = "No assessment sessions have been recorded yet."
		return view
	}
	view.Findings = dedupeAnalysisFindings(prioritizeFindings(inputs))
	view.Risk = summarizeRisk(view.Findings)
	for _, session := range sessions {
		view.Gaps = append(view.Gaps, session.Gaps...)
	}
	view.Gaps = uniqueStrings(view.Gaps)
	view.Summary = analysisSummary(view.Status, view.Risk, len(view.Findings), len(view.Gaps))
	view.NextActions = nextActions(view.Findings, view.Gaps)
	sort.Slice(view.Sessions, func(i, j int) bool { return view.Sessions[i].ID < view.Sessions[j].ID })
	return view
}

// analysisFindingView is intentionally converted back to the shared finding
// contract only for customer aggregation; evidence ownership remains in the
// assessment state and is never reconstructed from prose.
func (f analysisFinding) toFinding() assessment.Finding {
	return assessment.Finding{Title: f.Title, Status: f.Status, Severity: f.Severity, Confidence: f.Confidence, CVEIDs: f.CVEIDs, AffectedSoftware: f.AffectedSoftware, References: f.References, ValidationTask: f.ValidationTask, Impact: f.Impact, Steps: f.Steps, Evidence: f.Evidence, Remediation: f.Remediation}
}

func prioritizeFindings(inputs []analysisFindingInput) []analysisFinding {
	findings := make([]analysisFinding, 0, len(inputs))
	seen := make(map[string]struct{})
	for _, input := range inputs {
		f := input.Finding
		key := strings.Join([]string{input.SessionID, f.Title, f.Status, strings.Join(f.Evidence, "\x00")}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		score, priority, reason := findingPriority(f)
		findings = append(findings, analysisFinding{SessionID: input.SessionID, Title: f.Title, Status: f.Status, Severity: f.Severity, Confidence: f.Confidence, Priority: priority, PriorityScore: score, PriorityReason: reason, ValidationTask: f.ValidationTask, Impact: f.Impact, Steps: append([]string(nil), f.Steps...), Evidence: append([]string(nil), f.Evidence...), Remediation: append([]string(nil), f.Remediation...), CVEIDs: append([]string(nil), f.CVEIDs...), AffectedSoftware: append([]string(nil), f.AffectedSoftware...), References: append([]string(nil), f.References...)})
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].PriorityScore != findings[j].PriorityScore {
			return findings[i].PriorityScore > findings[j].PriorityScore
		}
		return findings[i].Title < findings[j].Title
	})
	return findings
}

func dedupeAnalysisFindings(findings []analysisFinding) []analysisFinding {
	seen := make(map[string]struct{}, len(findings))
	result := findings[:0]
	for _, finding := range findings {
		key := strings.Join([]string{finding.Title, finding.Status, strings.Join(finding.CVEIDs, "\x00"), strings.Join(finding.Evidence, "\x00")}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, finding)
	}
	return result
}

func findingPriority(f assessment.Finding) (int, string, string) {
	severity := map[string]int{"critical": 40, "high": 30, "medium": 20, "low": 10, "info": 1}[f.Severity]
	status := map[string]int{"reproduced": 6, "candidate": 0}[f.Status]
	confidence := map[string]int{"high": 3, "medium": 2, "low": 1}[f.Confidence]
	score := severity + status + confidence
	label := "review"
	switch {
	case f.Severity == "critical":
		label = "critical"
	case f.Severity == "high":
		label = "high"
	case f.Severity == "medium":
		label = "medium"
	case f.Severity == "low":
		label = "low"
	case f.Severity == "info":
		label = "info"
	}
	reasonParts := []string{}
	if f.Severity != "" {
		reasonParts = append(reasonParts, f.Severity+" severity")
	} else {
		reasonParts = append(reasonParts, "severity not rated")
	}
	if f.Status != "" {
		reasonParts = append(reasonParts, f.Status)
	}
	if f.Confidence != "" {
		reasonParts = append(reasonParts, f.Confidence+" confidence")
	}
	return score, label, strings.Join(reasonParts, " · ")
}

func summarizeRisk(findings []analysisFinding) analysisRisk {
	var risk analysisRisk
	for _, finding := range findings {
		switch finding.Severity {
		case "critical":
			risk.Critical++
		case "high":
			risk.High++
		case "medium":
			risk.Medium++
		case "low":
			risk.Low++
		case "info":
			risk.Info++
		default:
			risk.Unrated++
		}
		if finding.Status == "candidate" {
			risk.Candidates++
		}
		if finding.Status == "reproduced" {
			risk.Reproduced++
		}
	}
	return risk
}

func latestGaps(plans []assessment.Decision) []string {
	if len(plans) == 0 {
		return nil
	}
	// Decisions replace the current gap list, just as in the formal report.
	// Earlier plans stay in the audit history, including gaps since resolved.
	return uniqueStrings(plans[len(plans)-1].Gaps)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func analysisSummary(status string, risk analysisRisk, findings, gaps int) string {
	if findings == 0 {
		if gaps > 0 {
			return fmt.Sprintf("No findings are recorded yet; %d assessment gap(s) still need attention.", gaps)
		}
		return "No model-authored findings are recorded. This is not evidence that the target is secure."
	}
	if risk.Reproduced > 0 {
		return fmt.Sprintf("%d finding(s) include target validation evidence; %d candidate(s) still need validation or review.", risk.Reproduced, risk.Candidates)
	}
	return fmt.Sprintf("%d candidate finding(s) require operator review and, where warranted, target validation.", findings)
}

func nextActions(findings []analysisFinding, gaps []string) []string {
	var actions []string
	for _, finding := range findings {
		action := "Review evidence and decide whether to validate " + finding.Title
		if finding.Status == "reproduced" {
			action = "Triage and remediate " + finding.Title
		}
		actions = append(actions, action)
	}
	if len(gaps) > 0 {
		actions = append(actions, fmt.Sprintf("Review %d untested or unresolved areas before deciding on follow-up work.", len(gaps)))
	}
	return uniqueStrings(actions)
}
