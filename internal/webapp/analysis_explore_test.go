package webapp

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestCustomerAnalysisMapsRecordedCoverageAndCorrelatesWithoutMergingFindings(t *testing.T) {
	first := assessment.State{Scope: "https://app.example.test", Status: "completed", Plans: []assessment.Decision{{Complete: true, Findings: []assessment.Finding{{Title: "Authorization gap", Status: "candidate", CVEIDs: []string{"CVE-2026-1234"}, AffectedSoftware: []string{"Acme App 2.0"}}}}}, Results: []assessment.Result{{Task: assessment.Task{ID: "inspect", Goal: "Inspect access boundaries"}, Status: "done"}}}
	second := assessment.State{Scope: "https://app.example.test", Status: "completed_with_gaps", Plans: []assessment.Decision{{Complete: true, Gaps: []string{"Second role not tested"}, Findings: []assessment.Finding{{Title: "Related exposure", Status: "candidate", CVEIDs: []string{"CVE-2026-1234"}, AffectedSoftware: []string{"Acme App 2.0"}}}}}, Results: []assessment.Result{{Task: assessment.Task{ID: "compare", Goal: "Compare roles"}, Status: "blocked"}}}
	other := assessment.State{Scope: "https://other.example.test", Status: "completed", Plans: []assessment.Decision{{Complete: true}}}
	view := buildCustomerAnalysis("project", []analysisView{buildAnalysis("one", "project", first, nil), buildAnalysis("two", "project", second, nil), buildAnalysis("three", "project", other, nil)})
	if view.SessionCount != 3 || len(view.Coverage) != 2 || len(view.Findings) != 2 || len(view.Correlations) != 3 || view.Sessions[0].Tests != 1 || view.Sessions[0].Findings != 1 {
		t.Fatalf("customer exploration lost sessions, coverage, or correlations: %+v", view)
	}
	var shared analysisCoverage
	for _, coverage := range view.Coverage {
		if coverage.Scope == "https://app.example.test" {
			shared = coverage
		}
	}
	if len(shared.SessionIDs) != 2 || len(shared.Tests) != 2 || len(shared.WeakPoints) != 2 || len(shared.Gaps) != 1 || shared.Tests[1].Status != "blocked" {
		t.Fatalf("recorded coverage was flattened or overstated: %+v", shared)
	}
	for _, signal := range view.Correlations {
		if len(signal.Matches) != 2 || signal.Matches[0].SessionID == signal.Matches[1].SessionID {
			t.Fatalf("correlation merged source sessions: %+v", signal)
		}
	}
}

func TestSupportedChallengeStaysVisibleInAnalysis(t *testing.T) {
	state := assessment.State{Scope: "fixture", Status: "completed", Plans: []assessment.Decision{{Complete: true, Findings: []assessment.Finding{{Title: "Observed weakness", Status: "reproduced", ValidationTask: "check", Severity: "high", Evidence: []string{"check.log"}}}}}, Results: []assessment.Result{{Task: assessment.Task{ID: "check", Verification: &assessment.VerificationRequest{Claim: "boundary fails", Alternative: "cache served a stale page"}}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"check.log"}}}, Verification: &assessment.VerificationResult{Verdict: "supported", AlternativeResult: "fresh response matched", Reason: "cache did not explain it", Evidence: []string{"check.log"}}}}}
	view := buildAnalysis("one", "project", state, nil)
	if len(view.Findings) != 1 || view.Findings[0].Status != "reproduced" || view.Findings[0].Verification == nil || view.Findings[0].Verification.AlternativeResult != "fresh response matched" || view.Risk.Reproduced != 1 || len(view.Challenges) != 1 {
		t.Fatalf("supported challenge disappeared from analysis: %+v", view)
	}
}

func TestInconclusiveChallengeVisibleWithoutFinding(t *testing.T) {
	first := assessment.State{Scope: "https://app.example.test", Status: "completed", Plans: []assessment.Decision{{Complete: true}}, Results: []assessment.Result{{Task: assessment.Task{ID: "verify", Verification: &assessment.VerificationRequest{Claim: "cross-tenant response violates policy", Alternative: "legacy route permits sharing"}}, Status: "done", Verification: &assessment.VerificationResult{Verdict: "inconclusive", AlternativeResult: "policy not established", Reason: "both explanations remain possible", Evidence: []string{"verify.log"}}}}}
	second := assessment.State{Scope: "https://app.example.test", Status: "completed", Plans: []assessment.Decision{{Complete: true}}, Results: []assessment.Result{{Task: assessment.Task{ID: "retest", Goal: "Retest report access"}, Status: "done"}}}
	view := buildCustomerAnalysis("project", []analysisView{buildAnalysis("one", "project", first, nil), buildAnalysis("two", "project", second, nil)})
	if len(view.Findings) != 0 || len(view.Challenges) != 1 || len(view.Coverage) != 1 || len(view.Coverage[0].Challenges) != 1 || view.Challenges[0].Verdict != "inconclusive" || view.Challenges[0].Evidence[0] != "verify.log" || len(view.NextActions) == 0 || !strings.Contains(view.Summary, "could not settle") {
		t.Fatalf("unresolved challenge lost from customer exploration: %+v", view)
	}
}

func TestRecordedSoftwareCorrelatesAcrossDifferentScopesWithoutMergingFindings(t *testing.T) {
	groups := correlateFindings([]analysisFinding{
		{SessionID: "one", Scope: "https://one.example.test", Title: "First observation", Status: "candidate", AffectedSoftware: []string{"Acme App 2.0"}},
		{SessionID: "two", Scope: "https://two.example.test", Title: "Second observation", Status: "reproduced", AffectedSoftware: []string{"acme app 2.0"}},
	})
	if len(groups) != 1 || len(groups[0].Matches) != 2 || groups[0].Matches[0].SessionID == groups[0].Matches[1].SessionID {
		t.Fatalf("structured software overlap should be a review lead across scopes: %+v", groups)
	}
}
