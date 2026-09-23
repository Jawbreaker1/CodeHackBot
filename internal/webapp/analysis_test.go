package webapp

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

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

func TestCurrentFindingsAgreeAcrossAssessmentAndCustomerViews(t *testing.T) {
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
	if len(view.Findings) != 1 || view.Findings[0].Status != "reproduced" || len(analysis.Findings) != 1 || analysis.Risk.Candidates != 0 || analysis.Risk.Reproduced != 1 || len(customer.Findings) != 1 || customer.Findings[0].Finding.Status != "reproduced" {
		t.Fatalf("views disagree on current finding: session=%+v analysis=%+v customer=%+v", view.Findings, analysis.Findings, customer.Findings)
	}
	current.state.Plans = append(current.state.Plans, assessment.Decision{Complete: true})
	if len(current.view("").Findings) != 0 || len(current.analysis().Findings) != 0 || len(server.customerView("fixture-lab").Findings) != 0 {
		t.Fatal("withdrawn finding remained in the current risk views")
	}
	if len(current.state.Plans[0].Findings) != 1 {
		t.Fatal("historical candidate was removed from plan history")
	}
}
