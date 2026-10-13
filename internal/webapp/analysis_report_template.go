package webapp

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

//go:embed templates/customer-analysis.md.tmpl
var customerReportTemplateFiles embed.FS

var customerAnalysisReportTemplate = template.Must(template.New("customer-analysis.md.tmpl").Funcs(template.FuncMap{
	"inline": func(value string) string { return strings.Join(strings.Fields(value), " ") },
	"inc":    func(value int) int { return value + 1 },
}).ParseFS(customerReportTemplateFiles, "templates/customer-analysis.md.tmpl"))

type customerAnalysisReportData struct {
	View        analysisView
	FormatLabel string
	FormatNote  string
}

// renderCustomerAnalysisReport uses the same saved analysis that the project
// view displays. It does not merge similar findings or change session evidence.
func renderCustomerAnalysisReport(view analysisView, format assessment.ReportFormat) ([]byte, error) {
	if view.Kind != "customer" || !format.Valid() {
		return nil, fmt.Errorf("unsupported project report request")
	}
	data := customerAnalysisReportData{View: view}
	if format == assessment.OWASPReport {
		data.FormatLabel = "OWASP WSTG-aligned"
		data.FormatNote = "OWASP WSTG guides the report structure; this does not establish that every WSTG test was performed."
	} else {
		data.FormatLabel = "PTES-aligned"
		data.FormatNote = "PTES guides the report structure; this does not establish that every PTES phase was completed."
	}
	var output bytes.Buffer
	if err := customerAnalysisReportTemplate.Execute(&output, data); err != nil {
		return nil, fmt.Errorf("render project report: %w", err)
	}
	return output.Bytes(), nil
}
