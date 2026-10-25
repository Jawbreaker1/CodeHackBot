package assessment

import (
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestFormattedReportsPreserveRecordedFindingsWithoutInventingMappings(t *testing.T) {
	state := State{ID: "fixture", Goal: "Assess one authorized host", Scope: "192.0.2.1 only", Status: "completed",
		Plans:   []Decision{{Complete: true, Summary: "Executive summary\n\nOne configuration issue was observed.", Gaps: []string{"Firmware version was not observed."}, Findings: []Finding{{Title: "Plaintext administration", Status: "reproduced", ValidationTask: "inspect", Severity: "medium", Confidence: "high", Impact: "Local traffic may be intercepted.", Steps: []string{"Request the login page."}, Evidence: []string{"/workspace/evidence/login.http", "/workspace/evidence/inspect.log"}, Remediation: []string{"Enable HTTPS administration."}}}}},
		Results: []Result{{Task: Task{ID: "inspect", Goal: "Inspect the scoped service", Verification: &VerificationRequest{Claim: "administration uses HTTP", Alternative: "a TLS redirect protects the login"}}, Status: "done", Summary: "Verbose worker detail should remain in the canonical report.", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"/workspace/evidence/inspect.log"}}}, Verification: &VerificationResult{Verdict: "supported", AlternativeResult: "a fresh request did not redirect to TLS", Reason: "the login stayed on HTTP", Evidence: []string{"/workspace/evidence/inspect.log"}}}}}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		output, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		text := string(output)
		for _, want := range []string{"Assess one authorized host", "192.0.2.1 only", "Plaintext administration", "Request the login page.", "/workspace/evidence/login.http", "Enable HTTPS administration.", "Firmware version was not observed.", "administration uses HTTP", "a fresh request did not redirect to TLS"} {
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

func TestFormattedReportDowngradesUnchallengedHistoricalReproduction(t *testing.T) {
	state := State{Plans: []Decision{{Complete: true, Findings: []Finding{{Title: "Old claim", Status: "reproduced", Evidence: []string{"old.log"}}}}}}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		output, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		text := string(output)
		if !strings.Contains(text, "Old claim") || !strings.Contains(text, "candidate observations requiring recheck") || !strings.Contains(text, "candidate — unconfirmed") || !strings.Contains(text, "No confirmed findings or exposures were recorded") || strings.Contains(text, "**Status:** reproduced") {
			t.Fatalf("%s presented an old claim as reproduced: %s", format, text)
		}
	}
}

func TestRefutedValidationStaysOutOfFormalFindings(t *testing.T) {
	state := State{ID: "fixture", Status: "completed", Plans: []Decision{{Complete: true, Findings: []Finding{{Title: "Claim contradicted by check", Status: "candidate", Severity: "high", ValidationTask: "verify"}}}}, Results: []Result{{Task: Task{ID: "verify", Verification: &VerificationRequest{Claim: "private response is public", Alternative: "access is denied"}}, Status: "done", Verification: &VerificationResult{Verdict: "refuted", AlternativeResult: "access was denied", Reason: "protected response was not returned", Evidence: []string{"verify.log"}}}}}
	if got := DisplayFindings(state); len(got) != 0 {
		t.Fatalf("refuted claim stayed in displayed findings: %+v", got)
	}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		output, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(output), "Claim contradicted by check") {
			t.Fatalf("%s report listed a refuted claim as a finding: %s", format, output)
		}
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

func TestFindingsOverviewIsReadableMarkdownTable(t *testing.T) {
	state := State{Plans: []Decision{{Complete: true, Findings: []Finding{
		{Title: "Access | role boundary", Severity: "high", Status: "candidate"},
		{Title: "Confirmed | issue", Severity: "high", Status: "reproduced", ValidationTask: "verify", Evidence: []string{"verify.log"}},
	}}}, Results: []Result{{Task: Task{ID: "verify", Verification: &VerificationRequest{Claim: "boundary fails", Alternative: "access is denied"}}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"verify.log"}}}, Verification: &VerificationResult{Verdict: "supported", AlternativeResult: "fresh response matched", Reason: "access was not denied", Evidence: []string{"verify.log"}}}}}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		output, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		text := string(output)
		if !strings.Contains(text, "| ID | Finding | Severity | State |") || !strings.Contains(text, "| F-1 | Confirmed \\| issue | high | reproduced |") || !strings.Contains(text, "#### F-1 · Confirmed | issue") || !strings.Contains(text, "C-1 · Access | role boundary") || strings.Contains(text, "| F-2 | Access") {
			t.Fatalf("%s did not separate verified findings from candidates: %s", format, text)
		}
	}
}
