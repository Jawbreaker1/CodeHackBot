package orchestrator

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"time"
)

type RunStateSnapshot struct {
	RunID         string         `json:"run_id"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Phase         string         `json:"phase,omitempty"`
	ActiveWorkers int            `json:"active_workers"`
	TaskCounts    map[string]int `json:"task_counts"`
	ArtifactCount int            `json:"artifact_count"`
	FindingCount  int            `json:"finding_count"`
	BlockedTasks  []string       `json:"blocked_tasks,omitempty"`
}

func (m *Manager) WriteRunState(runID string, state RunStateSnapshot) error {
	path := filepath.Join(BuildRunPaths(m.SessionsDir, runID).PlanDir, "state.json")
	return WriteJSONAtomic(path, state)
}

func runStateHash(state RunStateSnapshot) string {
	clone := state
	clone.UpdatedAt = time.Time{}
	sort.Strings(clone.BlockedTasks)
	if clone.TaskCounts == nil {
		clone.TaskCounts = map[string]int{}
	}
	data, err := json.Marshal(clone)
	if err != nil {
		return ""
	}
	return hashKey(string(data))
}
