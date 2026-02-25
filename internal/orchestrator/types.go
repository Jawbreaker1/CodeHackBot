package orchestrator

import (
	"encoding/json"
	"time"
)

const (
	EventTypeRunStarted          = "run_started"
	EventTypeTaskLeased          = "task_leased"
	EventTypeTaskStarted         = "task_started"
	EventTypeTaskProgress        = "task_progress"
	EventTypeTaskArtifact        = "task_artifact"
	EventTypeTaskFinding         = "task_finding"
	EventTypeTaskFailed          = "task_failed"
	EventTypeTaskCompleted       = "task_completed"
	EventTypeWorkerStarted       = "worker_started"
	EventTypeWorkerHeartbeat     = "worker_heartbeat"
	EventTypeWorkerStopped       = "worker_stopped"
	EventTypeRunStopped          = "run_stopped"
	EventTypeRunCompleted        = "run_completed"
	EventTypeRunReportGenerated  = "run_report_generated"
	EventTypeRunWarning          = "run_warning"
	EventTypeRunStateUpdated     = "run_state_updated"
	EventTypeRunReplanRequested  = "run_replan_requested"
	EventTypeApprovalRequested   = "approval_requested"
	EventTypeApprovalGranted     = "approval_granted"
	EventTypeApprovalDenied      = "approval_denied"
	EventTypeApprovalExpired     = "approval_expired"
	EventTypeWorkerStopRequested = "worker_stop_requested"
	EventTypeOperatorInstruction = "operator_instruction"

	FindingStateHypothesis = "hypothesis"
	FindingStateCandidate  = "candidate_finding"
	FindingStateVerified   = "verified_finding"
	FindingStateRejected   = "rejected_finding"
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
	CreatedAt         time.Time    `json:"created_at,omitempty"`
	RunPhase          string       `json:"run_phase,omitempty"`
	Goal              string       `json:"goal,omitempty"`
	NormalizedGoal    string       `json:"normalized_goal,omitempty"`
	PlannerMode       string       `json:"planner_mode,omitempty"`
	PlannerVersion    string       `json:"planner_version,omitempty"`
	PlannerModel      string       `json:"planner_model,omitempty"`
	PlannerPlaybooks  []string     `json:"planner_playbooks,omitempty"`
	PlannerPromptHash string       `json:"planner_prompt_hash,omitempty"`
	PlannerDecision   string       `json:"planner_decision,omitempty"`
	PlannerRationale  string       `json:"planner_rationale,omitempty"`
	RegenerationCount int          `json:"regeneration_count,omitempty"`
	Hypotheses        []Hypothesis `json:"hypotheses,omitempty"`
}

type Hypothesis struct {
	ID               string   `json:"id"`
	Statement        string   `json:"statement"`
	Confidence       string   `json:"confidence"`
	Impact           string   `json:"impact"`
	Score            int      `json:"score"`
	SuccessSignals   []string `json:"success_signals,omitempty"`
	FailSignals      []string `json:"fail_signals,omitempty"`
	EvidenceRequired []string `json:"evidence_required,omitempty"`
}

type Scope struct {
	Networks    []string `json:"networks,omitempty"`
	Targets     []string `json:"targets,omitempty"`
	DenyTargets []string `json:"deny_targets,omitempty"`
}

type TaskSpec struct {
	TaskID            string     `json:"task_id"`
	Title             string     `json:"title"`
	IdempotencyKey    string     `json:"idempotency_key,omitempty"`
	Goal              string     `json:"goal"`
	Targets           []string   `json:"targets,omitempty"`
	DependsOn         []string   `json:"depends_on,omitempty"`
	Priority          int        `json:"priority,omitempty"`
	Strategy          string     `json:"strategy,omitempty"`
	Action            TaskAction `json:"action,omitempty"`
	DoneWhen          []string   `json:"done_when"`
	FailWhen          []string   `json:"fail_when"`
	ExpectedArtifacts []string   `json:"expected_artifacts"`
	RiskLevel         string     `json:"risk_level"`
	Budget            TaskBudget `json:"budget"`
}

type TaskAction struct {
	Type           string   `json:"type,omitempty"`
	Command        string   `json:"command,omitempty"`
	Args           []string `json:"args,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	WorkingDir     string   `json:"working_dir,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
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
	State       string            `json:"state,omitempty"`
	Severity    string            `json:"severity"`
	Confidence  string            `json:"confidence"`
	Evidence    []string          `json:"evidence,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Sources     []FindingSource   `json:"sources,omitempty"`
	Conflicts   []FindingConflict `json:"conflicts,omitempty"`
}

type FindingSource struct {
	EventID    string    `json:"event_id"`
	WorkerID   string    `json:"worker_id"`
	TaskID     string    `json:"task_id"`
	Source     string    `json:"source,omitempty"`
	Severity   string    `json:"severity,omitempty"`
	Confidence string    `json:"confidence,omitempty"`
	TS         time.Time `json:"ts"`
}

type FindingConflict struct {
	Field          string `json:"field"`
	ExistingValue  string `json:"existing_value"`
	IncomingValue  string `json:"incoming_value"`
	IncomingEvent  string `json:"incoming_event"`
	IncomingSource string `json:"incoming_source,omitempty"`
	Resolution     string `json:"resolution"`
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
