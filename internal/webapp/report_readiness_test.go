package webapp

import (
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func TestAssessmentViewShowsLiveReportRecordChecks(t *testing.T) {
	started := time.Now().Add(-time.Minute)
	r := &run{id: "assessment-one", root: t.TempDir(), goal: "Inspect service", scope: "192.0.2.1", status: "running",
		state: assessment.State{Goal: "Inspect service", Scope: "192.0.2.1", Status: "running", StartedAt: started,
			Plans: []assessment.Decision{{Tasks: []assessment.Task{{ID: "inspect"}}}}},
	}
	view := r.view("")
	if view.ReportReadiness.Status != "collecting" || len(view.ReportReadiness.Checks) == 0 {
		t.Fatalf("live report checks were not exposed: %+v", view.ReportReadiness)
	}
	r.state.Results = []assessment.Result{{Task: assessment.Task{ID: "inspect"}, Status: "done"}}
	r.state.Plans = append(r.state.Plans, assessment.Decision{Complete: true, Summary: "Inspected the service"})
	r.state.Status, r.status = "completed", "completed"
	r.state.FinishedAt = time.Now()
	view = r.view("")
	if view.ReportReadiness.Status != "ready_for_review" {
		t.Fatalf("completed run did not update report checks: %+v", view.ReportReadiness)
	}
}
