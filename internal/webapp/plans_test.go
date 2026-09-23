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
	if plans[0].Tasks[0].Status != "done" || plans[0].Tasks[1].Status != "skipped" {
		t.Fatalf("restored plan projection = %+v", plans)
	}
}

func TestResearchPlanIsVisibleBeforeOperatorSelection(t *testing.T) {
	r := &run{
		id: "research-fixture", goal: "investigate local software", scope: "synthetic only", status: "running",
		workers: map[string]workerView{}, approvals: map[string]*pendingApproval{}, questions: map[string]*pendingQuestion{},
		plan: &pendingPlan{ID: "plan-1", plan: assessment.Decision{Phase: "research", Summary: "Identify relevant local sources", Tasks: []assessment.Task{{ID: "sources", Goal: "inventory advisories", DoneWhen: "sources recorded"}}}},
	}
	view := r.view("")
	if view.PendingPlan == nil || view.PendingPlan.Phase != "research" || len(view.PlanTimeline) != 1 || view.PlanTimeline[0].Phase != "research" {
		t.Fatalf("research plan review is not visible: %+v", view)
	}
}
