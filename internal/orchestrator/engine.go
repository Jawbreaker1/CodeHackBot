package orchestrator

import "context"

type Engine interface {
	Start(context.Context, RunPlan) (string, error)
	Status(context.Context, string) (RunStatus, error)
	Stop(context.Context, string) error
}

type RunStatus struct {
	RunID         string `json:"run_id"`
	State         string `json:"state"`
	ActiveWorkers int    `json:"active_workers"`
	QueuedTasks   int    `json:"queued_tasks"`
	RunningTasks  int    `json:"running_tasks"`
}
