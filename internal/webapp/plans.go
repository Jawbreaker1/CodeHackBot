package webapp

import "github.com/Jawbreaker1/CodeHackBot/internal/assessment"

// coordinatorPlanView is a read-only presentation of persisted coordinator
// decisions. It never infers or advances a plan from tool output.
type coordinatorPlanView struct {
	Round        int                   `json:"round"`
	Phase        string                `json:"phase"`
	Summary      string                `json:"summary"`
	PlainSummary string                `json:"plain_summary,omitempty"`
	Review       string                `json:"review,omitempty"`
	Signal       string                `json:"signal"`
	Status       string                `json:"status"`
	Tasks        []coordinatorTaskView `json:"tasks"`
}

type coordinatorTaskView struct {
	ID            string   `json:"id"`
	Goal          string   `json:"goal"`
	DoneWhen      string   `json:"done_when"`
	StrategyHints []string `json:"strategy_hints,omitempty"`
	Status        string   `json:"status"`
	ResultSummary string   `json:"result_summary,omitempty"`
}

func coordinatorPlans(state assessment.State, workers map[string]workerView) []coordinatorPlanView {
	results := make(map[string]assessment.Result, len(state.Results))
	for _, result := range state.Results {
		results[result.Task.ID] = result
	}
	plans := make([]coordinatorPlanView, 0, len(state.Plans))
	for i, decision := range state.Plans {
		phase := decision.Phase
		if phase == "" {
			phase = "assessment"
		}
		plan := coordinatorPlanView{Round: i + 1, Phase: phase, Summary: decision.Summary, PlainSummary: decision.PlainSummary, Status: "finished"}
		if i+1 < len(state.Plans) {
			plan.Review = state.Plans[i+1].Review
		}
		if decision.Complete {
			plan.Status = "complete"
		}
		selected := make(map[string]bool, len(decision.ApprovedTaskIDs))
		for _, id := range decision.ApprovedTaskIDs {
			selected[id] = true
		}
		for _, task := range decision.Tasks {
			result := results[task.ID]
			status := result.Status
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
			plan.Tasks = append(plan.Tasks, coordinatorTaskView{ID: task.ID, Goal: task.Goal, DoneWhen: task.DoneWhen, StrategyHints: task.StrategyHints, Status: status, ResultSummary: result.Summary})
		}
		plan.Signal = planSignal(plan, state, i)
		plans = append(plans, plan)
	}
	return plans
}

// A plan's execution status is not its outcome. Only the next recorded
// coordinator decision can report a finding from this round's workers.
func planSignal(plan coordinatorPlanView, state assessment.State, index int) string {
	if plan.Status == "complete" {
		for _, finding := range state.Plans[index].Findings {
			if finding.Status == "reproduced" {
				return "verified_final"
			}
		}
		return "concluded"
	}
	if plan.Status == "running" {
		return "in_progress"
	}
	selected := map[string]bool{}
	for _, task := range plan.Tasks {
		if task.Status == "failed" || task.Status == "blocked" || task.Status == "aborted" {
			return "needs_attention"
		}
		if task.Status != "skipped" {
			selected[task.ID] = true
		}
	}
	if len(selected) == 0 {
		return "not_run"
	}
	if index+1 >= len(state.Plans) {
		return "awaiting_review"
	}
	signal := "continued"
	for _, finding := range state.Plans[index+1].Findings {
		if !selected[finding.ValidationTask] {
			continue
		}
		if finding.Status == "reproduced" {
			return "verified"
		}
		if finding.Status == "candidate" {
			signal = "candidate"
		}
	}
	return signal
}
