package webapp

import "github.com/Jawbreaker1/CodeHackBot/internal/assessment"

// coordinatorPlanView is a read-only presentation of persisted coordinator
// decisions. It never infers or advances a plan from tool output.
type coordinatorPlanView struct {
	Round   int                   `json:"round"`
	Phase   string                `json:"phase"`
	Summary string                `json:"summary"`
	Status  string                `json:"status"`
	Tasks   []coordinatorTaskView `json:"tasks"`
}

type coordinatorTaskView struct {
	ID       string `json:"id"`
	Goal     string `json:"goal"`
	DoneWhen string `json:"done_when"`
	Status   string `json:"status"`
}

func coordinatorPlans(state assessment.State, workers map[string]workerView) []coordinatorPlanView {
	results := make(map[string]string, len(state.Results))
	for _, result := range state.Results {
		results[result.Task.ID] = result.Status
	}
	plans := make([]coordinatorPlanView, 0, len(state.Plans))
	for i, decision := range state.Plans {
		phase := decision.Phase
		if phase == "" {
			phase = "assessment"
		}
		plan := coordinatorPlanView{Round: i + 1, Phase: phase, Summary: decision.Summary, Status: "finished"}
		if decision.Complete {
			plan.Status = "complete"
		}
		selected := make(map[string]bool, len(decision.ApprovedTaskIDs))
		for _, id := range decision.ApprovedTaskIDs {
			selected[id] = true
		}
		for _, task := range decision.Tasks {
			status := results[task.ID]
			if status == "" {
				status = "queued"
				if !selected[task.ID] {
					status = "skipped"
				} else {
					if worker, ok := workers[task.ID]; ok && worker.Phase != "task_queued" {
						status = "running"
					}
					plan.Status = "running"
				}
			}
			plan.Tasks = append(plan.Tasks, coordinatorTaskView{ID: task.ID, Goal: task.Goal, DoneWhen: task.DoneWhen, Status: status})
		}
		plans = append(plans, plan)
	}
	return plans
}
