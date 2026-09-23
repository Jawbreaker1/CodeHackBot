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
	"inc":    func(value int) int { return value + 1 },
	"join":   strings.Join,
}).ParseFS(reportTemplateFiles, "templates/*.md.tmpl"))

type formattedReport struct {
	ID                string
	Status            string
	Goal              string
	Scope             string
	Started           string
	Finished          string
	Summary           string
	UnreviewedResults bool
	Findings          []Finding
	Gaps              []string
	Results           []Result
}

// RenderFormattedReport applies a fixed report structure to the saved
// coordinator findings. The result is still a draft requiring operator review.
func RenderFormattedReport(state State, format ReportFormat) ([]byte, error) {
	if !format.Valid() {
		return nil, fmt.Errorf("unsupported report format %q", format)
	}
	data := formattedReport{
		ID: state.ID, Status: state.Status, Goal: state.Goal, Scope: state.Scope,
		Findings: CurrentFindings(state.Plans), Results: state.Results,
	}
	if !state.StartedAt.IsZero() {
		data.Started = state.StartedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	if !state.FinishedAt.IsZero() {
		data.Finished = state.FinishedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	data.Summary, data.UnreviewedResults = reportOutcome(state)
	if len(state.Plans) > 0 {
		last := state.Plans[len(state.Plans)-1]
		if !data.UnreviewedResults {
			data.Gaps = last.Gaps
		}
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
