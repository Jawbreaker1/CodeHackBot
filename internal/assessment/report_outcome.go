package assessment

import (
	"fmt"
	"strings"
)

// LatestUnreviewedResult identifies a result from the most recent plan when
// the run ended before a following coordinator decision could assess it.
func LatestUnreviewedResult(state State) (Result, bool) {
	if (state.Status != "incomplete" && state.Status != "aborted") || len(state.Plans) == 0 || len(state.Results) == 0 {
		return Result{}, false
	}
	lastPlan := state.Plans[len(state.Plans)-1]
	if lastPlan.Complete {
		return Result{}, false
	}
	lastResult := state.Results[len(state.Results)-1]
	for _, task := range lastPlan.Tasks {
		if task.ID == lastResult.Task.ID {
			return lastResult, true
		}
	}
	return Result{}, false
}

// reportOutcome separates a coordinator's final conclusion from a plan that
// was followed by worker results the coordinator never reviewed.
func reportOutcome(state State) (summary string, unreviewed bool) {
	if len(state.Plans) > 0 && state.Plans[len(state.Plans)-1].Complete {
		if value := strings.TrimSpace(state.Plans[len(state.Plans)-1].Summary); value != "" {
			return reportSummary(value), false
		}
	}
	if state.Status == "running" || state.Status == "starting" {
		return "Assessment in progress; no final coordinator conclusion has been recorded.", false
	}
	last, pending := LatestUnreviewedResult(state)
	if !pending {
		return "The coordinator did not record a final conclusion. Review the recorded plans and limitations before drawing conclusions.", false
	}
	summary = fmt.Sprintf("The assessment ended before the coordinator reviewed its latest worker results. The latest worker, %s, finished with status %s.", last.Task.ID, last.Status)
	if detail := strings.TrimSpace(last.Summary); detail != "" {
		summary += " Its recorded result: " + detail
	}
	summary += " Findings below come from the preceding plan and may not reflect that result. Its gaps were not reconciled. Worker completion alone does not establish an assessment finding."
	return summary, true
}
