package webapp

import (
	"crypto/sha256"
	"encoding/hex"
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
	Target           string                   `json:"target,omitempty"`
	Model            string                   `json:"model,omitempty"`
	ReportURL        string                   `json:"report_url,omitempty"`
	Status           string                   `json:"status"`
	Summary          string                   `json:"summary,omitempty"`
	Conclusion       string                   `json:"conclusion,omitempty"`
	ConclusionDetail string                   `json:"conclusion_detail,omitempty"`
	LatestResult     string                   `json:"latest_result,omitempty"`
	ReviewPending    bool                     `json:"review_pending,omitempty"`
	SessionCount     int                      `json:"session_count"`
	Risk             analysisRisk             `json:"risk"`
	Findings         []analysisFinding        `json:"findings"`
	PriorReviews     []analysisPriorReview    `json:"prior_reviews,omitempty"`
	Challenges       []analysisChallenge      `json:"challenges"`
	Coverage         []analysisCoverage       `json:"coverage"`
	Correlations     []analysisCorrelation    `json:"correlations"`
	NextActions      []string                 `json:"next_actions"`
	NextChecks       []string                 `json:"next_checks,omitempty"`
	Gaps             []string                 `json:"gaps"`
	Sessions         []analysisSessionSummary `json:"sessions,omitempty"`
	WebPages         []analysisWebPage        `json:"web_pages,omitempty"`
	WebTransitions   []analysisWebTransition  `json:"web_transitions,omitempty"`
	GeneratedAt      time.Time                `json:"generated_at"`
	FinishedAt       time.Time                `json:"finished_at,omitempty"`
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
	Remediated int `json:"remediated"`
}

type analysisFinding struct {
	ID               string                   `json:"id"`
	SessionID        string                   `json:"session_id,omitempty"`
	Target           string                   `json:"target,omitempty"`
	Scope            string                   `json:"scope,omitempty"`
	Title            string                   `json:"title"`
	RelatedPrior     []string                 `json:"related_prior,omitempty"`
	LatestReview     *analysisPriorReview     `json:"latest_review,omitempty"`
	Status           string                   `json:"status"`
	RecordedStatus   string                   `json:"recorded_status,omitempty"`
	Verification     *analysisVerification    `json:"verification,omitempty"`
	Severity         string                   `json:"severity,omitempty"`
	Confidence       string                   `json:"confidence,omitempty"`
	Priority         string                   `json:"priority"`
	PriorityScore    int                      `json:"priority_score"`
	PriorityReason   string                   `json:"priority_reason"`
	ValidationTask   string                   `json:"validation_task,omitempty"`
	Impact           string                   `json:"impact"`
	Steps            []string                 `json:"steps"`
	Evidence         []string                 `json:"evidence"`
	Remediation      []string                 `json:"remediation"`
	CVEIDs           []string                 `json:"cve_ids,omitempty"`
	AffectedSoftware []string                 `json:"affected_software,omitempty"`
	References       []string                 `json:"references,omitempty"`
	SourceLocations  []analysisSourceLocation `json:"source_locations,omitempty"`
}

type analysisPriorReview struct {
	SessionID string `json:"session_id"`
	assessment.PriorReview
}

type analysisVerification struct {
	Claim             string   `json:"claim"`
	Alternative       string   `json:"alternative"`
	Verdict           string   `json:"verdict"`
	AlternativeResult string   `json:"alternative_result"`
	Reason            string   `json:"reason"`
	Evidence          []string `json:"evidence"`
}

// A challenge remains useful assessment evidence even when the coordinator
// correctly declines to promote the observation to a finding.
type analysisChallenge struct {
	SessionID         string   `json:"session_id"`
	TaskID            string   `json:"task_id"`
	Scope             string   `json:"scope"`
	Claim             string   `json:"claim"`
	Alternative       string   `json:"alternative"`
	Verdict           string   `json:"verdict"`
	AlternativeResult string   `json:"alternative_result"`
	Reason            string   `json:"reason"`
	Evidence          []string `json:"evidence"`
}

type analysisSessionSummary struct {
	ID         string    `json:"id"`
	Target     string    `json:"target,omitempty"`
	Goal       string    `json:"goal"`
	Scope      string    `json:"scope"`
	Status     string    `json:"status"`
	Model      string    `json:"model,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	ReportURL  string    `json:"report_url"`
	Tests      int       `json:"tests"`
	Findings   int       `json:"findings"`
	Challenges int       `json:"challenges"`
	Gaps       int       `json:"gaps"`
}

type analysisFindingInput struct {
	SessionID string
	Target    string
	Scope     string
	Finding   assessment.Finding
}

func (r *run) analysis() analysisView {
	r.mu.RLock()
	state := r.state
	view := buildAnalysis(r.id, r.customer, state, nil)
	root, anchors := r.root, browserAnchors(state)
	artifacts := registeredSourceArtifacts(state)
	r.mu.RUnlock()
	view.WebPages, view.WebTransitions = browserAnalysis(root, view.ID, anchors)
	hydrateSourceLocations(root, artifacts, view.Findings)
	linkPageFindings(view.WebPages, view.Findings)
	return view
}

func buildAnalysis(id, customer string, state assessment.State, inputs []analysisFindingInput) analysisView {
	view := analysisView{
		Kind: "assessment", ID: id, Customer: customer, Goal: state.Goal,
		Scope: state.Scope, Target: assessmentTarget(state), Model: state.Model, Status: state.Status, SessionCount: 1, FinishedAt: state.FinishedAt,
		GeneratedAt: time.Now().UTC(),
	}
	view.ReportURL = "/api/v1/assessments/" + id + "/report"
	if len(inputs) == 0 {
		for _, finding := range assessment.CurrentFindings(state.Plans) {
			inputs = append(inputs, analysisFindingInput{SessionID: id, Target: view.Target, Scope: state.Scope, Finding: finding})
		}
	}
	view.Findings = prioritizeFindings(inputs)
	latestReviews := map[string]analysisPriorReview{}
	for _, plan := range state.Plans {
		for _, review := range plan.PriorReviews {
			latestReviews[review.FindingID] = analysisPriorReview{SessionID: id, PriorReview: review}
		}
	}
	for _, review := range latestReviews {
		view.PriorReviews = append(view.PriorReviews, review)
	}
	sort.Slice(view.PriorReviews, func(i, j int) bool { return view.PriorReviews[i].FindingID < view.PriorReviews[j].FindingID })
	for _, result := range state.Results {
		if result.Task.Verification == nil || result.Verification == nil {
			continue
		}
		view.Challenges = append(view.Challenges, analysisChallenge{
			SessionID: id, TaskID: result.Task.ID, Scope: state.Scope,
			Claim: result.Task.Verification.Claim, Alternative: result.Task.Verification.Alternative,
			Verdict: result.Verification.Verdict, AlternativeResult: result.Verification.AlternativeResult,
			Reason: result.Verification.Reason, Evidence: append([]string(nil), result.Verification.Evidence...),
		})
	}
	for i := range view.Findings {
		finding := &view.Findings[i]
		if finding.ValidationTask != "" {
			for _, result := range state.Results {
				if result.Task.ID == finding.ValidationTask && result.Task.Verification != nil && result.Verification != nil {
					finding.Verification = &analysisVerification{Claim: result.Task.Verification.Claim, Alternative: result.Task.Verification.Alternative, Verdict: result.Verification.Verdict, AlternativeResult: result.Verification.AlternativeResult, Reason: result.Verification.Reason, Evidence: append([]string(nil), result.Verification.Evidence...)}
					break
				}
			}
		}
		if finding.Status == "reproduced" && assessment.SupportedChallengeForFinding(state.Results, assessment.Finding{ValidationTask: finding.ValidationTask, Evidence: finding.Evidence}) == nil {
			finding.RecordedStatus = finding.Status
			finding.Status = "candidate"
			finding.Priority = "review"
			finding.PriorityScore, _, _ = findingPriority(assessment.Finding{Status: "candidate", Severity: finding.Severity, Confidence: finding.Confidence})
			finding.PriorityReason = "Earlier reproduction claim has no supported challenge verdict"
		}
	}
	sortAnalysisFindings(view.Findings)
	view.Coverage = []analysisCoverage{coverageForAssessment(id, state, view.Findings, view.Challenges)}
	view.Risk = summarizeRisk(view.Findings)
	view.Gaps = uniqueStrings(assessment.ReportGaps(state))
	for _, lead := range assessment.CurrentOpenLeads(state.Plans) {
		view.NextChecks = append(view.NextChecks, lead.NextCheck)
	}
	view.Conclusion = assessmentConclusion(state)
	view.ConclusionDetail = assessmentConclusionDetail(state)
	lastResult, reviewPending := assessment.LatestUnreviewedResult(state)
	view.ReviewPending = reviewPending
	if view.ReviewPending {
		view.LatestResult = fmt.Sprintf("%s — %s: %s", lastResult.Task.ID, lastResult.Status, lastResult.Summary)
		view.Summary = "Assessment ended before the coordinator reviewed its latest worker result. Findings and gaps are from the preceding plan and need review."
	} else {
		view.Summary = analysisSummary(view.Status, view.Risk, len(view.Findings), len(view.Gaps), unresolvedChallenges(view.Challenges))
	}
	view.NextActions = nextActions(view.Findings, view.Challenges, view.Gaps, view.NextChecks)
	return view
}

func buildCustomerAnalysis(id string, sessions []analysisView) analysisView {
	view := analysisView{Kind: "customer", ID: id, Status: "no_sessions", ReportURL: "/api/v1/customers/" + id + "/report", GeneratedAt: time.Now().UTC()}
	for _, session := range sessions {
		tests := 0
		for _, scope := range session.Coverage {
			tests += len(scope.Tests)
		}
		view.Sessions = append(view.Sessions, analysisSessionSummary{ID: session.ID, Target: session.Target, Goal: session.Goal, Scope: session.Scope, Status: session.Status, Model: session.Model, UpdatedAt: session.FinishedAt, ReportURL: "/api/v1/assessments/" + session.ID + "/report", Tests: tests, Findings: len(session.Findings), Challenges: len(session.Challenges), Gaps: len(session.Gaps)})
		if session.Status == "running" || session.Status == "starting" {
			view.Status = "active"
		} else if view.Status == "no_sessions" {
			view.Status = session.Status
		}
		view.Findings = append(view.Findings, session.Findings...)
		view.PriorReviews = append(view.PriorReviews, session.PriorReviews...)
		view.Challenges = append(view.Challenges, session.Challenges...)
		view.WebPages = append(view.WebPages, session.WebPages...)
		view.WebTransitions = append(view.WebTransitions, session.WebTransitions...)
	}
	view.SessionCount = len(sessions)
	if len(sessions) == 0 {
		view.Summary = "No assessment sessions have been recorded yet."
		return view
	}
	reviewTimes := map[string]time.Time{}
	for _, session := range sessions {
		for _, review := range session.PriorReviews {
			if prior, ok := reviewTimes[review.FindingID]; ok && prior.After(session.FinishedAt) {
				continue
			}
			reviewTimes[review.FindingID] = session.FinishedAt
			for i := range view.Findings {
				if view.Findings[i].ID == review.FindingID {
					copy := review
					view.Findings[i].LatestReview = &copy
				}
			}
		}
	}
	sortAnalysisFindings(view.Findings)
	linkPageFindings(view.WebPages, view.Findings)
	view.Coverage = mergeCoverage(sessions)
	view.Correlations = correlateFindings(view.Findings)
	view.Risk = summarizeRisk(view.Findings)
	for _, session := range sessions {
		view.Gaps = append(view.Gaps, session.Gaps...)
		view.NextChecks = append(view.NextChecks, session.NextChecks...)
	}
	view.Gaps = uniqueStrings(view.Gaps)
	view.NextChecks = uniqueStrings(view.NextChecks)
	view.Summary = analysisSummary(view.Status, view.Risk, len(view.Findings), len(view.Gaps), unresolvedChallenges(view.Challenges))
	view.NextActions = nextActions(view.Findings, view.Challenges, view.Gaps, view.NextChecks)
	sort.Slice(view.Sessions, func(i, j int) bool { return view.Sessions[i].ID < view.Sessions[j].ID })
	return view
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
		findings = append(findings, analysisFinding{ID: analysisFindingID(input.SessionID, f), SessionID: input.SessionID, Target: input.Target, Scope: input.Scope, Title: f.Title, RelatedPrior: append([]string(nil), f.RelatedPrior...), Status: f.Status, Severity: f.Severity, Confidence: f.Confidence, Priority: priority, PriorityScore: score, PriorityReason: reason, ValidationTask: f.ValidationTask, Impact: f.Impact, Steps: append([]string(nil), f.Steps...), Evidence: append([]string(nil), f.Evidence...), Remediation: append([]string(nil), f.Remediation...), CVEIDs: append([]string(nil), f.CVEIDs...), AffectedSoftware: append([]string(nil), f.AffectedSoftware...), References: append([]string(nil), f.References...), SourceLocations: analysisSources(f.SourceLocations)})
	}
	sortAnalysisFindings(findings)
	return findings
}

func analysisFindingID(sessionID string, finding assessment.Finding) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + finding.Title + "\x00" + strings.Join(finding.Evidence, "\x00")))
	return sessionID + "#" + hex.EncodeToString(sum[:6])
}

func sortAnalysisFindings(findings []analysisFinding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].PriorityScore != findings[j].PriorityScore {
			return findings[i].PriorityScore > findings[j].PriorityScore
		}
		return findings[i].Title < findings[j].Title
	})
}

func findingPriority(f assessment.Finding) (int, string, string) {
	severity := map[string]int{"critical": 40, "high": 30, "medium": 20, "low": 10, "info": 1}[f.Severity]
	status := map[string]int{"reproduced": 6, "candidate": 0}[f.Status]
	confidence := map[string]int{"high": 3, "medium": 2, "low": 1}[f.Confidence]
	score := severity + status + confidence
	label := "review"
	switch {
	case f.Status != "reproduced":
		label = "review"
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
		if finding.LatestReview != nil && finding.LatestReview.Verdict == "remediated" {
			risk.Remediated++
			continue
		}
		if finding.Status == "candidate" {
			risk.Candidates++
			continue
		}
		if finding.Status == "reproduced" {
			risk.Reproduced++
		}
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
	}
	return risk
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

func unresolvedChallenges(challenges []analysisChallenge) int {
	count := 0
	for _, challenge := range challenges {
		if challenge.Verdict == "inconclusive" {
			count++
		}
	}
	return count
}

func analysisSummary(status string, risk analysisRisk, findings, gaps, unresolved int) string {
	activeFindings := findings - risk.Remediated
	if findings > 0 && activeFindings == 0 {
		return fmt.Sprintf("%d earlier %s have an evidence-backed remediation retest. Review the linked checks before treating the target as clear.", findings, countNoun(findings, "finding", "findings"))
	}
	if activeFindings == 0 {
		if unresolved > 0 {
			base := fmt.Sprintf("No confirmed findings. %d follow-up %s could not settle the claim.", unresolved, countNoun(unresolved, "check", "checks"))
			if gaps == 0 {
				return base
			}
			return base + " Review the test limits and open questions before drawing conclusions."
		}
		if gaps > 0 {
			return "No findings are recorded yet. Review what was tested and the remaining questions before drawing conclusions."
		}
		return "No findings are recorded. Check test coverage before drawing conclusions about the target."
	}
	if risk.Reproduced > 0 {
		confirmed := fmt.Sprintf("%d %s passed a recorded follow-up check.", risk.Reproduced, countNoun(risk.Reproduced, "finding", "findings"))
		if risk.Candidates == 0 {
			return confirmed
		}
		return confirmed + fmt.Sprintf(" %d %s still need validation or review.", risk.Candidates, countNoun(risk.Candidates, "candidate", "candidates"))
	}
	verb := "require"
	pronoun := "they"
	if activeFindings == 1 {
		verb = "requires"
		pronoun = "it"
	}
	return fmt.Sprintf("%d possible %s %s confirmation before %s can be treated as verified.", activeFindings, countNoun(activeFindings, "finding", "findings"), verb, pronoun)
}

func countNoun(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

func nextActions(findings []analysisFinding, challenges []analysisChallenge, gaps, nextChecks []string) []string {
	var actions []string
	for _, finding := range findings {
		if finding.LatestReview != nil && finding.LatestReview.Verdict == "remediated" {
			continue
		}
		action := "Check whether " + finding.Title + " affects the current target before assigning a fix."
		if finding.Status == "reproduced" {
			action = "Prioritize a fix for " + finding.Title + ", then retest it."
		}
		actions = append(actions, action)
	}
	for _, challenge := range challenges {
		if challenge.Verdict == "inconclusive" {
			actions = append(actions, "Review why the follow-up check could not settle: "+challenge.Claim)
		}
	}
	actions = append(actions, nextChecks...)
	if len(gaps) > 0 && len(nextChecks) == 0 {
		actions = append(actions, "Review the recorded limits of this test before deciding what to check next.")
	}
	return uniqueStrings(actions)
}
