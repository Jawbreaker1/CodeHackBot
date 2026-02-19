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

func (m *Manager) WriteTask(runID string, task TaskSpec) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	if err := ValidateTaskSpec(task); err != nil {
		return err
	}
	path := filepath.Join(BuildRunPaths(m.SessionsDir, runID).TaskDir, task.TaskID+".json")
	if err := WriteJSONAtomic(path, task); err != nil {
		return fmt.Errorf("write task spec: %w", err)
	}
	return nil
}

func (m *Manager) AddTask(runID string, task TaskSpec) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	if err := ValidateTaskSpec(task); err != nil {
		return err
	}
	plan, err := m.LoadRunPlan(runID)
	if err != nil {
		return err
	}
	for _, existing := range plan.Tasks {
		if existing.TaskID == task.TaskID {
			return fmt.Errorf("task %s already exists", task.TaskID)
		}
	}
	plan.Tasks = append(plan.Tasks, task)
	if err := ValidateRunPlan(plan); err != nil {
		return err
	}
	planPath := filepath.Join(BuildRunPaths(m.SessionsDir, runID).PlanDir, "plan.json")
	if err := WriteJSONAtomic(planPath, plan); err != nil {
		return fmt.Errorf("write run plan: %w", err)
	}
	if err := m.WriteTask(runID, task); err != nil {
		return err
	}
	return nil
}
