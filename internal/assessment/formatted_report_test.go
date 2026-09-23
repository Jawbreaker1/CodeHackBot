package assessment

import (
	"strings"
	"testing"
)

func TestFormattedReportsPreserveRecordedFindingsWithoutInventingMappings(t *testing.T) {
	state := State{ID: "fixture", Goal: "Assess one authorized host", Scope: "192.0.2.1 only", Status: "completed",
		Plans:   []Decision{{Complete: true, Summary: "Executive summary\n\nOne configuration issue was observed.", Gaps: []string{"Firmware version was not observed."}, Findings: []Finding{{Title: "Plaintext administration", Status: "reproduced", Severity: "medium", Confidence: "high", Impact: "Local traffic may be intercepted.", Steps: []string{"Request the login page."}, Evidence: []string{"/workspace/evidence/login.http"}, Remediation: []string{"Enable HTTPS administration."}}}}},
		Results: []Result{{Task: Task{ID: "inspect", Goal: "Inspect the scoped service"}, Status: "done", Summary: "Verbose worker detail should remain in the canonical report."}}}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		output, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		text := string(output)
		for _, want := range []string{"Assess one authorized host", "192.0.2.1 only", "Plaintext administration", "Request the login page.", "/workspace/evidence/login.http", "Enable HTTPS administration.", "Firmware version was not observed."} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s report omitted %q: %s", format, want, text)
			}
		}
		if strings.Contains(text, "WSTG-CONF-") || strings.Contains(text, "CVSS:4.0/") {
			t.Fatalf("%s report invented a standards mapping or score", format)
		}
		if strings.Contains(text, "Verbose worker detail") || strings.Contains(text, "Executive summary\n\nExecutive summary") {
			t.Fatalf("%s report copied verbose worker output or a duplicate heading", format)
		}
		if format == PTESReport && !strings.Contains(text, "**Priority actions:**\n\n- Enable HTTPS administration.") {
			t.Fatal("PTES priority actions did not render as a Markdown list")
		}
	}
	if _, err := RenderFormattedReport(state, "unknown"); err == nil {
		t.Fatal("unknown report format was accepted")
	}
}

func TestIncompleteReportDoesNotPresentAnUnreviewedPlanAsConclusion(t *testing.T) {
	state := State{ID: "fixture", Status: "incomplete", Error: "coordinator context limit", Plans: []Decision{{Summary: "A worker will retry the check", Gaps: []string{"Retry in progress"}, Tasks: []Task{{ID: "retry"}}}}, Results: []Result{{Task: Task{ID: "retry"}, Status: "done", Summary: "The retry completed but could not inspect the application."}}}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		output, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		text := string(output)
		if strings.Contains(text, "A worker will retry the check") || strings.Contains(text, "Retry in progress") || !strings.Contains(text, "The retry completed but could not inspect the application") || !strings.Contains(text, "did not reconcile") || !strings.Contains(text, "coordinator context limit") {
			t.Fatalf("%s presented stale planning as a conclusion: %s", format, text)
		}
	}
}
