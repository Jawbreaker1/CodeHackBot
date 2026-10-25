package assessment

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

// ReportFormat names a versioned presentation of the recorded assessment.
// Neither format changes the underlying findings or claims standards certification.
type ReportFormat string

const (
	OWASPReport ReportFormat = "owasp-wstg"
	PTESReport  ReportFormat = "ptes"
)

func (f ReportFormat) Valid() bool { return f == OWASPReport || f == PTESReport }

//go:embed templates/*.md.tmpl
var reportTemplateFiles embed.FS

var reportTemplates = template.Must(template.New("").Funcs(template.FuncMap{
	"inline": func(value string) string { return strings.Join(strings.Fields(value), " ") },
	"cell": func(value string) string {
		return strings.ReplaceAll(strings.Join(strings.Fields(value), " "), "|", `\|`)
	},
	"inc":  func(value int) int { return value + 1 },
	"join": strings.Join,
}).ParseFS(reportTemplateFiles, "templates/*.md.tmpl"))

type formattedReport struct {
	ID                   string
	Status               string
	Goal                 string
	Scope                string
	ContinuationRequests []string
	Started              string
	Finished             string
	Summary              string
	SummaryEdited        bool
	EditorialNotes       string
	UnreviewedResults    bool
	LegacyClaims         bool
	Findings             []Finding
	Candidates           []Finding
	PriorReviews         []PriorReview
	NextChecks           []Lead
	Gaps                 []string
	Results              []Result
	Rounds               []reportRound
	Verifications        map[string]*formattedVerification
	AdvisoryIDs          map[string]string
	Readiness            ReportReadiness
}

// ReportEditorial changes only narrative text. Factual sections are always
// rebuilt from saved assessment state when a draft is revised.
type ReportEditorial struct {
	Summary string `json:"summary,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

type formattedVerification struct {
	Claim             string
	AlternativeResult string
	Verdict           string
	Reason            string
}

// RenderFormattedReport applies a fixed report structure to the saved
// coordinator findings. The result is still a draft requiring operator review.
func RenderFormattedReport(state State, format ReportFormat) ([]byte, error) {
	return RenderFormattedReportWithEditorial(state, format, ReportEditorial{})
}

func RenderFormattedReportWithEditorial(state State, format ReportFormat, editorial ReportEditorial) ([]byte, error) {
	if !format.Valid() {
		return nil, fmt.Errorf("unsupported report format %q", format)
	}
	findings, candidates, legacyClaims := reportFindingGroups(state)
	data := formattedReport{
		ID: state.ID, Status: state.Status, Goal: state.Goal, Scope: state.Scope, ContinuationRequests: state.ContinuationRequests,
		Findings: findings, Candidates: candidates, PriorReviews: CurrentPriorReviews(state.Plans), LegacyClaims: legacyClaims, Results: state.Results, Rounds: reportRounds(state), Verifications: map[string]*formattedVerification{}, AdvisoryIDs: map[string]string{},
		NextChecks: CurrentOpenLeads(state.Plans),
		Readiness:  AssessReportReadiness(state),
	}
	for _, result := range state.Results {
		if result.Task.Verification != nil && result.Verification != nil {
			data.Verifications[result.Task.ID] = &formattedVerification{Claim: result.Task.Verification.Claim, AlternativeResult: result.Verification.AlternativeResult, Verdict: result.Verification.Verdict, Reason: result.Verification.Reason}
		}
	}
	for _, finding := range findings {
		if advisory, ok := AdvisoryForFinding(state, finding); ok {
			data.AdvisoryIDs[finding.AdvisoryAssessmentID] = advisory.AdvisoryID
		}
	}
	if !state.StartedAt.IsZero() {
		data.Started = state.StartedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	if !state.FinishedAt.IsZero() {
		data.Finished = state.FinishedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	data.Summary, data.UnreviewedResults = reportOutcome(state)
	if editorial.Summary != "" && !data.UnreviewedResults {
		data.Summary = editorial.Summary
		data.SummaryEdited = true
	}
	data.EditorialNotes = editorial.Notes
	if !data.UnreviewedResults {
		data.Gaps = ReportGaps(state)
	}
	if state.Error != "" {
		data.Gaps = append(append([]string(nil), data.Gaps...), "Run limitation: "+state.Error)
	}
	var output bytes.Buffer
	if err := reportTemplates.ExecuteTemplate(&output, string(format)+".md.tmpl", data); err != nil {
		return nil, fmt.Errorf("render %s report: %w", format, err)
	}
	return output.Bytes(), nil
}

// Coordinator prose sometimes starts with its own section label. The template
// supplies that heading, so omit only an exact duplicate first line.
func reportSummary(value string) string {
	value = strings.TrimSpace(value)
	first, rest, hasRest := strings.Cut(value, "\n")
	if hasRest && strings.EqualFold(strings.TrimSpace(strings.TrimLeft(first, "# ")), "Executive summary") {
		return strings.TrimSpace(rest)
	}
	return value
}
