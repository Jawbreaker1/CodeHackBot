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
	if HasDeniedExecution(state) {
		completed, blocked := 0, 0
		for _, result := range state.Results {
			if result.Status == "done" {
				completed++
			}
			if result.DeniedExecution != nil {
				blocked++
			}
		}
		findings, _ := reviewFindings(state)
		candidates, reproduced := 0, 0
		for _, finding := range findings {
			if finding.Status == "reproduced" {
				reproduced++
			} else {
				candidates++
			}
		}
		return fmt.Sprintf("Assessment status: %s. %d delegated task(s) completed; %d stopped after an operator-denied action. The denied actions did not execute. The recorded results list %d reproduced finding(s) and %d candidate(s); review their cited evidence and the unfinished task objectives below.", strings.ReplaceAll(state.Status, "_", " "), completed, blocked, reproduced, candidates), false
	}
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

// HasDeniedExecution reports whether a worker stopped on a recorded operator
// denial, so review views can use the same factual fallback as formal reports.
func HasDeniedExecution(state State) bool {
	for _, result := range state.Results {
		if result.DeniedExecution != nil {
			return true
		}
	}
	return false
}

// ReviewSummary is the report's current executive summary, including the
// recorded-outcome fallback when final model prose is unsafe to reuse.
func ReviewSummary(state State) string {
	summary, _ := reportOutcome(state)
	return summary
}

// ReportGaps uses recorded task outcomes when an action was denied. The model's
// final free-text gaps remain in the saved plan but cannot substitute a broader
// unperformed objective for the exact proposal the operator declined.
func ReportGaps(state State) []string {
	if !HasDeniedExecution(state) {
		if len(state.Plans) == 0 {
			return nil
		}
		return append([]string(nil), state.Plans[len(state.Plans)-1].Gaps...)
	}
	var gaps []string
	for _, result := range state.Results {
		if denied := result.DeniedExecution; denied != nil {
			gaps = append(gaps, fmt.Sprintf("Task %s stopped after the operator denied the proposed action %q. That action did not execute; the remaining task objective was not completed. Approval record: %s", result.Task.ID, denied.Summary, denied.AuditRef))
		} else if result.Status != "done" {
			gaps = append(gaps, fmt.Sprintf("Task %s ended with status %s; its objective was not confirmed complete.", result.Task.ID, result.Status))
		}
	}
	return gaps
}
