package webapp

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func TestCoordinatorPlansFollowPersistedDecisionsAndLiveWorkerState(t *testing.T) {
	state := assessment.State{Plans: []assessment.Decision{{
		Phase:           "research",
		Summary:         "Trace plan display",
		Tasks:           []assessment.Task{{ID: "trace", Goal: "trace the UI", DoneWhen: "source evidence"}, {ID: "skip", Goal: "unused"}},
		ApprovedTaskIDs: []string{"trace"},
	}}}
	plans := coordinatorPlans(state, map[string]workerView{"trace": {ID: "trace", Phase: "execution_started"}})
	if len(plans) != 1 || plans[0].Phase != "research" || plans[0].Status != "running" || plans[0].Tasks[0].Status != "running" || plans[0].Tasks[1].Status != "skipped" {
		t.Fatalf("live plan projection = %+v", plans)
	}
	state.Results = []assessment.Result{{Task: state.Plans[0].Tasks[0], Status: "done"}}
	plans = coordinatorPlans(state, nil)
	if plans[0].Tasks[0].Status != "done" || plans[0].Tasks[1].Status != "skipped" || plans[0].Signal != "awaiting_review" {
		t.Fatalf("restored plan projection = %+v", plans)
	}
}

func TestCoordinatorPlanSeparatesWorkerCompletionFromFinding(t *testing.T) {
	state := assessment.State{Plans: []assessment.Decision{
		{Summary: "Attempt recovery", Tasks: []assessment.Task{{ID: "first", Goal: "test one method"}}, ApprovedTaskIDs: []string{"first"}},
		{Summary: "Try another method", Review: "The first method did not establish access.", Tasks: []assessment.Task{{ID: "second", Goal: "test another method"}}, ApprovedTaskIDs: []string{"second"}},
		{Summary: "Verify a lead", Review: "A possible issue was found, but access still needs checking.", Tasks: []assessment.Task{{ID: "verify", Goal: "check the lead"}}, ApprovedTaskIDs: []string{"verify"}, Findings: []assessment.Finding{{Status: "candidate", ValidationTask: "second"}}},
		{Summary: "Assessment finished", Complete: true, Review: "A separate check confirmed the issue.", Findings: []assessment.Finding{{Status: "reproduced", ValidationTask: "verify"}}},
	}, Results: []assessment.Result{
		{Task: assessment.Task{ID: "first"}, Status: "done", Summary: "One method finished without recovery"},
		{Task: assessment.Task{ID: "second"}, Status: "done"},
		{Task: assessment.Task{ID: "verify"}, Status: "done"},
	}}
	plans := coordinatorPlans(state, nil)
	if plans[0].Signal != "continued" || plans[0].Review != "The first method did not establish access." || plans[0].Tasks[0].ResultSummary == "" {
		t.Fatalf("finished worker was presented as success: %+v", plans[0])
	}
	if plans[1].Signal != "candidate" || plans[2].Signal != "verified" || plans[3].Signal != "verified_final" {
		t.Fatalf("finding progression = %+v", plans)
	}
}

func TestResearchPlanIsVisibleBeforeOperatorSelection(t *testing.T) {
	r := &run{
		id: "research-fixture", goal: "investigate local software", scope: "synthetic only", status: "running",
		workers: map[string]workerView{}, approvals: map[string]*pendingApproval{}, questions: map[string]*pendingQuestion{},
		plan: &pendingPlan{ID: "plan-1", plan: assessment.Decision{Phase: "research", Summary: "Identify relevant local sources", Tasks: []assessment.Task{{ID: "sources", Goal: "inventory advisories", DoneWhen: "sources recorded"}}}},
	}
	view := r.view("")
	if view.PendingPlan == nil || view.PendingPlan.Phase != "research" || len(view.PlanTimeline) != 1 || view.PlanTimeline[0].Phase != "research" || view.PlanTimeline[0].Signal != "review" {
		t.Fatalf("research plan review is not visible: %+v", view)
	}
}
