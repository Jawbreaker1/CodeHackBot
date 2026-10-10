package assessment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Finished worker results are saved as they arrive. After a server restart,
// keep those results and make unfinished tasks explicit before the next
// coordinator round. No task is replayed implicitly.
func recoverInterruptedBatch(root string, state *State) (bool, error) {
	if len(state.Plans) == 0 || state.Plans[len(state.Plans)-1].Complete {
		return false, nil
	}
	plan := state.Plans[len(state.Plans)-1]
	selected := make(map[string]bool, len(plan.ApprovedTaskIDs))
	for _, id := range plan.ApprovedTaskIDs {
		selected[id] = true
	}
	skipped := make(map[string]bool, len(plan.SkippedTaskIDs))
	for _, id := range plan.SkippedTaskIDs {
		skipped[id] = true
	}
	completed := make(map[string]bool, len(state.Results))
	for _, result := range state.Results {
		completed[result.Task.ID] = true
	}
	changed := false
	for _, task := range plan.Tasks {
		if completed[task.ID] || skipped[task.ID] || len(selected) > 0 && !selected[task.ID] {
			continue
		}
		result := Result{Task: task, Status: "blocked", Summary: "This worker was interrupted before a completed result was recorded. Review its saved task session and evidence before repeating work or drawing a conclusion."}
		if !validID(task.ID) {
			return changed, fmt.Errorf("invalid interrupted task ID %q", task.ID)
		}
		path := filepath.Join(root, "tasks", task.ID, "result.json")
		recovered := false
		if data, err := os.ReadFile(path); err == nil {
			var saved Result
			if json.Unmarshal(data, &saved) == nil && saved.Task.ID == task.ID && finishedWorkerStatus(saved.Status) {
				saved.Task = task
				result = saved
				recovered = true
			}
		}
		if !recovered {
			if err := saveJSON(path, result); err != nil {
				return changed, err
			}
		}
		state.Results = append(state.Results, result)
		completed[task.ID] = true
		changed = true
	}
	return changed, nil
}

func finishedWorkerStatus(status string) bool {
	switch status {
	case "done", "failed", "blocked", "aborted", "waiting_user":
		return true
	default:
		return false
	}
}
