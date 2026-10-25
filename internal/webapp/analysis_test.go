package webapp

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestAnalysisUsesRecordedDeniedActionInsteadOfModelGap(t *testing.T) {
	state := assessment.State{
		Status: "completed_with_gaps",
		Plans:  []assessment.Decision{{Complete: true, Summary: "The operator denied a marker execution.", PlainSummary: "The marker was denied.", Gaps: []string{"The operator denied a marker execution."}}},
		Results: []assessment.Result{{
			Task: assessment.Task{ID: "validate", Goal: "Validate a marker"}, Status: "blocked",
			DeniedExecution: &ctxpacket.DeniedExecution{Summary: "Read source excerpts", AuditRef: "tasks/validate/logs/denied.approval.json"},
		}},
	}
	view := buildAnalysis("fixture", "lab", state, nil)
	if len(view.Gaps) != 1 || !strings.Contains(view.Gaps[0], "Read source excerpts") || strings.Contains(view.Gaps[0], "denied a marker") || strings.Contains(view.Conclusion, "marker was denied") || strings.Contains(view.ConclusionDetail, "denied a marker") || len(view.Coverage) != 1 || len(view.Coverage[0].Gaps) != 1 {
		t.Fatalf("analysis did not use the recorded denial: %+v", view)
	}
}

func TestAnalysisPreservesLatestUnresolvedGap(t *testing.T) {
	state := assessment.State{Status: "incomplete", Plans: []assessment.Decision{
		{Gaps: []string{"No evidence yet"}},
		{Gaps: []string{"One role still needs validation"}},
	}}
	view := buildAnalysis("fixture", "lab", state, nil)
	if len(view.Gaps) != 1 || view.Gaps[0] != "One role still needs validation" || view.Conclusion != "" {
		t.Fatalf("latest unresolved gap not preserved: %+v", view)
	}
}

func TestAnalysisDoesNotRepeatEveryGapAsANextAction(t *testing.T) {
	state := assessment.State{Status: "incomplete", Plans: []assessment.Decision{{Gaps: []string{"Service check not run", "Login behavior not tested", "Source unavailable"}}}}
	view := buildAnalysis("fixture", "lab", state, nil)
	if len(view.Gaps) != 3 || len(view.NextActions) != 1 {
		t.Fatalf("analysis repeated the gap list as actions: %+v", view)
	}
}

func TestAnalysisShowsRecordedNextCheckInsteadOfGenericGapAction(t *testing.T) {
	check := "Read the installed build and compare it with the vendor release history."
	state := assessment.State{Status: "completed_with_gaps", Plans: []assessment.Decision{{
		Complete:  true,
		OpenLeads: []assessment.Lead{{ID: "version", Hypothesis: "The installed build may predate a fix.", NextCheck: check, Evidence: []string{"tasks/inspect/version.log"}}},
	}}}
	view := buildAnalysis("fixture", "lab", state, nil)
	if len(view.NextChecks) != 1 || view.NextChecks[0] != check || len(view.NextActions) != 1 || view.NextActions[0].Kind != "investigate" || view.NextActions[0].Title != check || len(view.Gaps) != 1 || !strings.Contains(view.Gaps[0], "Unverified lead") {
		t.Fatalf("analysis did not surface the recorded continuation: %+v", view)
	}
}

func TestAnalysisActionsKeepFindingLanguageAndPriority(t *testing.T) {
	findings := []analysisFinding{
		{ID: "high", Title: "Saknar åtkomstkontroll", Status: "reproduced", Remediation: []string{"Begränsa åtkomsten och testa igen."}},
		{ID: "candidate", Title: "Möjlig sessionsläcka", Status: "candidate"},
	}
	actions := nextActions(findings, nil, nil, []string{"Granska ett annat flöde."})
	if len(actions) != 3 || actions[0].Kind != "fix" || actions[0].Title != findings[0].Title || actions[0].Detail != findings[0].Remediation[0] || actions[0].FindingID != findings[0].ID || actions[1].Kind != "verify" || actions[1].Title != findings[1].Title || actions[2].Kind != "investigate" || actions[2].Title != "Granska ett annat flöde." {
		t.Fatalf("actions lost finding order or rewrote the operator language: %+v", actions)
	}
}

func TestAnalysisSummaryUsesReadableSingularCounts(t *testing.T) {
	summary := analysisSummary("completed", analysisRisk{Reproduced: 1, Candidates: 1}, 2, 0, 0)
	if !strings.Contains(summary, "1 candidate observation still needs review") {
		t.Fatalf("singular finding summary is unclear: %s", summary)
	}
	if summary := analysisSummary("completed", analysisRisk{Candidates: 2}, 2, 0, 0); !strings.Contains(summary, "2 candidate observations need confirmation before becoming findings") {
		t.Fatalf("unconfirmed observations were presented as findings: %s", summary)
	}
}

func TestAnalysisSeparatesUnreviewedWorkerResultFromCandidateRisk(t *testing.T) {
	state := assessment.State{Status: "incomplete", Plans: []assessment.Decision{{Summary: "The worker will retry", Gaps: []string{"Retry in progress"}, Tasks: []assessment.Task{{ID: "retry"}}, Findings: []assessment.Finding{{Title: "Possible issue", Status: "candidate", Severity: "medium", Confidence: "low"}}}}, Results: []assessment.Result{{Task: assessment.Task{ID: "retry"}, Status: "done", Summary: "The retry ended without application access."}}}
	view := buildAnalysis("fixture", "lab", state, nil)
	if !view.ReviewPending || view.Conclusion != "" || view.LatestResult == "" || view.Risk.Medium != 0 || view.Risk.Candidates != 1 || view.Findings[0].Priority != "review" {
		t.Fatalf("analysis overstated an incomplete result: %+v", view)
	}
}

func TestAnalysisFlagsLegacyReproducedFindingWithoutChangingSavedState(t *testing.T) {
	server := NewServer(Config{RepoRoot: t.TempDir()})
	current, err := server.newRun("fixture-lab", "review fixture", "synthetic only")
	if err != nil {
		t.Fatal(err)
	}
	candidate := assessment.Finding{Title: "sample issue", Status: "candidate", Impact: "hypothesis", Evidence: []string{"first.log"}}
	reproduced := assessment.Finding{Title: "sample issue", Status: "reproduced", Impact: "validated", Evidence: []string{"validation.log"}}
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "completed", Plans: []assessment.Decision{
		{Findings: []assessment.Finding{candidate}},
		{Complete: true, Findings: []assessment.Finding{reproduced}},
	}}
	view := current.view("")
	analysis := current.analysis()
	customer := server.customerView("fixture-lab")
	if len(view.Findings) != 1 || view.Findings[0].Status != "candidate" || len(analysis.Findings) != 1 || analysis.Findings[0].Status != "candidate" || analysis.Findings[0].RecordedStatus != "reproduced" || analysis.Risk.Candidates != 1 || analysis.Risk.Reproduced != 0 || len(customer.Findings) != 1 || customer.Findings[0].Finding.Status != "candidate" {
		t.Fatalf("legacy reproduction was not marked for review without changing saved state: session=%+v analysis=%+v customer=%+v", view.Findings, analysis.Findings, customer.Findings)
	}
	current.state.Plans = append(current.state.Plans, assessment.Decision{Complete: true})
	if len(current.view("").Findings) != 0 || len(current.analysis().Findings) != 0 || len(server.customerView("fixture-lab").Findings) != 0 {
		t.Fatal("withdrawn finding remained in the current risk views")
	}
	if len(current.state.Plans[0].Findings) != 1 {
		t.Fatal("historical candidate was removed from plan history")
	}
}
