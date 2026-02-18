package orchestrator

import (
	"encoding/json"
	"time"
)

const (
	EventTypeRunStarted        = "run_started"
	EventTypeTaskLeased        = "task_leased"
	EventTypeTaskStarted       = "task_started"
	EventTypeTaskProgress      = "task_progress"
	EventTypeTaskArtifact      = "task_artifact"
	EventTypeTaskFinding       = "task_finding"
	EventTypeTaskFailed        = "task_failed"
	EventTypeTaskCompleted     = "task_completed"
	EventTypeWorkerStarted     = "worker_started"
	EventTypeWorkerStopped     = "worker_stopped"
	EventTypeRunStopped        = "run_stopped"
	EventTypeRunCompleted      = "run_completed"
	EventTypeApprovalRequested = "approval_requested"
	EventTypeApprovalGranted   = "approval_granted"
	EventTypeApprovalDenied    = "approval_denied"
	EventTypeApprovalExpired   = "approval_expired"
)

type RunPlan struct {
	RunID           string       `json:"run_id"`
	Scope           Scope        `json:"scope"`
	Constraints     []string     `json:"constraints"`
	SuccessCriteria []string     `json:"success_criteria"`
	StopCriteria    []string     `json:"stop_criteria"`
	MaxParallelism  int          `json:"max_parallelism"`
	Tasks           []TaskSpec   `json:"tasks"`
	Metadata        PlanMetadata `json:"metadata,omitempty"`
}

type PlanMetadata struct {
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type Scope struct {
	Networks    []string `json:"networks,omitempty"`
	Targets     []string `json:"targets,omitempty"`
	DenyTargets []string `json:"deny_targets,omitempty"`
}

type TaskSpec struct {
	TaskID            string     `json:"task_id"`
	Title             string     `json:"title"`
	Goal              string     `json:"goal"`
	DependsOn         []string   `json:"depends_on,omitempty"`
	Priority          int        `json:"priority,omitempty"`
	Strategy          string     `json:"strategy,omitempty"`
	DoneWhen          []string   `json:"done_when"`
	FailWhen          []string   `json:"fail_when"`
	ExpectedArtifacts []string   `json:"expected_artifacts"`
	RiskLevel         string     `json:"risk_level"`
	Budget            TaskBudget `json:"budget"`
}

type TaskBudget struct {
	MaxSteps     int           `json:"max_steps"`
	MaxToolCalls int           `json:"max_tool_calls"`
	MaxRuntime   time.Duration `json:"max_runtime"`
}

type TaskLease struct {
	TaskID    string    `json:"task_id"`
	LeaseID   string    `json:"lease_id"`
	WorkerID  string    `json:"worker_id"`
	Status    string    `json:"status"`
	Attempt   int       `json:"attempt"`
	StartedAt time.Time `json:"started_at"`
	Deadline  time.Time `json:"deadline"`
}

type Artifact struct {
	RunID    string            `json:"run_id"`
	TaskID   string            `json:"task_id"`
	Type     string            `json:"type"`
	Title    string            `json:"title"`
	Path     string            `json:"path"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Finding struct {
	RunID       string            `json:"run_id"`
	TaskID      string            `json:"task_id"`
	Target      string            `json:"target"`
	FindingType string            `json:"finding_type"`
	Title       string            `json:"title"`
	Severity    string            `json:"severity"`
	Confidence  string            `json:"confidence"`
	Evidence    []string          `json:"evidence,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type EventEnvelope struct {
	EventID  string          `json:"event_id"`
	RunID    string          `json:"run_id"`
	WorkerID string          `json:"worker_id"`
	TaskID   string          `json:"task_id,omitempty"`
	Seq      int64           `json:"seq"`
	TS       time.Time       `json:"ts"`
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}
