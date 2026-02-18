package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) ReadTask(runID, taskID string) (TaskSpec, error) {
	if strings.TrimSpace(runID) == "" {
		return TaskSpec{}, fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return TaskSpec{}, fmt.Errorf("task id is required")
	}
	path := filepath.Join(BuildRunPaths(m.SessionsDir, runID).TaskDir, taskID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskSpec{}, fmt.Errorf("read task spec: %w", err)
	}
	var task TaskSpec
	if err := json.Unmarshal(data, &task); err != nil {
		return TaskSpec{}, fmt.Errorf("parse task spec: %w", err)
	}
	if err := ValidateTaskSpec(task); err != nil {
		return TaskSpec{}, fmt.Errorf("validate task spec: %w", err)
	}
	return task, nil
}
