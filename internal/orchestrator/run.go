package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const orchestratorWorkerID = "orchestrator"

type Manager struct {
	SessionsDir string
	Now         func() time.Time
}

type WorkerStatus struct {
	WorkerID    string    `json:"worker_id"`
	State       string    `json:"state"`
	LastSeq     int64     `json:"last_seq"`
	LastEvent   time.Time `json:"last_event"`
	CurrentTask string    `json:"current_task,omitempty"`
}

func NewManager(sessionsDir string) *Manager {
	return &Manager{
		SessionsDir: sessionsDir,
		Now:         func() time.Time { return time.Now().UTC() },
	}
}

func (m *Manager) Start(planPath, overrideRunID string) (string, error) {
	plan, err := ReadRunPlan(planPath)
	if err != nil {
		return "", err
	}
	return m.StartFromPlan(plan, overrideRunID)
}

func (m *Manager) StartFromPlan(plan RunPlan, overrideRunID string) (string, error) {
	if overrideRunID != "" {
		plan.RunID = overrideRunID
	}
	if err := ValidatePlanForStart(plan); err != nil {
		return "", err
	}

	paths, err := EnsureRunLayout(m.SessionsDir, plan.RunID)
	if err != nil {
		return "", err
	}
	if err := WriteJSONAtomic(filepath.Join(paths.PlanDir, "plan.json"), plan); err != nil {
		return "", fmt.Errorf("write run plan: %w", err)
	}
	for _, task := range plan.Tasks {
		if err := WriteJSONAtomic(filepath.Join(paths.TaskDir, task.TaskID+".json"), task); err != nil {
			return "", fmt.Errorf("write task %s: %w", task.TaskID, err)
		}
	}
	if err := m.InitializeMemoryBank(plan.RunID, plan); err != nil {
		return "", fmt.Errorf("initialize memory bank: %w", err)
	}

	seq, err := m.nextSeq(plan.RunID, orchestratorWorkerID)
	if err != nil {
		return "", err
	}
	event := EventEnvelope{
		EventID:  NewEventID(),
		RunID:    plan.RunID,
		WorkerID: orchestratorWorkerID,
		Seq:      seq,
		TS:       m.Now(),
		Type:     EventTypeRunStarted,
		Payload:  mustJSONRaw(map[string]any{"source": "start"}),
	}
	if err := AppendEventJSONL(m.eventPath(plan.RunID), event); err != nil {
		return "", err
	}
	return plan.RunID, nil
}

func (m *Manager) Stop(runID string) error {
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	events, err := m.Events(runID, 0)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return fmt.Errorf("run %s has no events", runID)
	}
	stopWorkerID := fmt.Sprintf("operator-stop-%d", m.Now().UnixNano())
	seq, err := m.nextSeq(runID, stopWorkerID)
	if err != nil {
		return err
	}
	return AppendEventJSONL(m.eventPath(runID), EventEnvelope{
		EventID:  NewEventID(),
		RunID:    runID,
		WorkerID: stopWorkerID,
		Seq:      seq,
		TS:       m.Now(),
		Type:     EventTypeRunStopped,
		Payload:  mustJSONRaw(map[string]any{"source": "stop"}),
	})
}

func (m *Manager) Status(runID string) (RunStatus, error) {
	events, err := m.Events(runID, 0)
	if err != nil {
		return RunStatus{}, err
	}
	return BuildRunStatus(runID, events), nil
}

func (m *Manager) Workers(runID string) ([]WorkerStatus, error) {
	events, err := m.Events(runID, 0)
	if err != nil {
		return nil, err
	}
	return BuildWorkerStatus(events), nil
}

func (m *Manager) Events(runID string, limit int) ([]EventEnvelope, error) {
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	path := m.eventPath(runID)
	events, err := ReadEvents(path)
	if err != nil {
		return nil, err
	}
	events = DedupeEvents(events)
	if err := ValidateMonotonicSequences(events); err != nil {
		return nil, err
	}
	if limit > 0 && len(events) > limit {
		return events[len(events)-limit:], nil
	}
	return events, nil
}

func (m *Manager) eventPath(runID string) string {
	return filepath.Join(m.SessionsDir, runID, "orchestrator", "event", "event.jsonl")
}

func (m *Manager) nextSeq(runID, workerID string) (int64, error) {
	path := m.eventPath(runID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, fmt.Errorf("stat event log: %w", err)
	}
	events, err := ReadEvents(path)
	if err != nil {
		return 0, err
	}
	var maxSeq int64
	for _, event := range events {
		if event.WorkerID == workerID && event.Seq > maxSeq {
			maxSeq = event.Seq
		}
	}
	return maxSeq + 1, nil
}

func ReadRunPlan(path string) (RunPlan, error) {
	if path == "" {
		return RunPlan{}, fmt.Errorf("plan path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RunPlan{}, fmt.Errorf("read run plan: %w", err)
	}
	var plan RunPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return RunPlan{}, fmt.Errorf("parse run plan: %w", err)
	}
	return plan, nil
}

func (m *Manager) LoadRunPlan(runID string) (RunPlan, error) {
	if runID == "" {
		return RunPlan{}, fmt.Errorf("run id is required")
	}
	planPath := filepath.Join(BuildRunPaths(m.SessionsDir, runID).PlanDir, "plan.json")
	return ReadRunPlan(planPath)
}

func ValidatePlanForStart(plan RunPlan) error {
	if err := ValidateRunPlan(plan); err != nil {
		return err
	}
	if len(plan.Constraints) == 0 {
		return fmt.Errorf("%w: constraints is required", ErrInvalidPlan)
	}
	if len(plan.Scope.Networks) == 0 && len(plan.Scope.Targets) == 0 {
		return fmt.Errorf("%w: scope networks or targets is required", ErrInvalidPlan)
	}
	return nil
}

func BuildRunStatus(runID string, events []EventEnvelope) RunStatus {
	status := RunStatus{
		RunID: runID,
		State: "unknown",
	}
	if len(events) == 0 {
		return status
	}

	workerState := map[string]bool{}
	taskState := map[string]string{}
	hasRunStarted := false

	for _, event := range events {
		switch event.Type {
		case EventTypeRunStarted:
			hasRunStarted = true
		case EventTypeRunStopped:
			status.State = "stopped"
		case EventTypeRunCompleted:
			status.State = "completed"
		case EventTypeWorkerStarted:
			workerState[event.WorkerID] = true
		case EventTypeWorkerStopped:
			workerState[event.WorkerID] = false
		case EventTypeTaskLeased:
			taskState[event.TaskID] = "queued"
		case EventTypeTaskStarted, EventTypeTaskProgress, EventTypeTaskArtifact, EventTypeTaskFinding, EventTypeApprovalRequested, EventTypeApprovalGranted, EventTypeApprovalDenied, EventTypeApprovalExpired:
			taskState[event.TaskID] = "running"
		case EventTypeTaskCompleted, EventTypeTaskFailed:
			taskState[event.TaskID] = "done"
		}
	}

	if status.State == "unknown" && hasRunStarted {
		status.State = "running"
	}
	for _, active := range workerState {
		if active {
			status.ActiveWorkers++
		}
	}
	for _, task := range taskState {
		switch task {
		case "queued":
			status.QueuedTasks++
		case "running":
			status.RunningTasks++
		}
	}
	return status
}

func BuildWorkerStatus(events []EventEnvelope) []WorkerStatus {
	byWorker := map[string]WorkerStatus{}

	for _, event := range events {
		if event.WorkerID == "" {
			continue
		}
		ws := byWorker[event.WorkerID]
		if ws.WorkerID == "" {
			ws.WorkerID = event.WorkerID
			ws.State = "seen"
		}
		ws.LastSeq = maxI64(ws.LastSeq, event.Seq)
		if event.TS.After(ws.LastEvent) {
			ws.LastEvent = event.TS
		}
		switch event.Type {
		case EventTypeWorkerStarted:
			ws.State = "active"
		case EventTypeWorkerHeartbeat:
			if ws.State != "stopped" {
				ws.State = "active"
			}
		case EventTypeWorkerStopped:
			ws.State = "stopped"
		case EventTypeTaskStarted, EventTypeTaskProgress, EventTypeTaskArtifact, EventTypeTaskFinding:
			if ws.State != "stopped" {
				ws.State = "active"
			}
			ws.CurrentTask = event.TaskID
		case EventTypeTaskCompleted, EventTypeTaskFailed:
			if ws.CurrentTask == event.TaskID {
				ws.CurrentTask = ""
			}
		}
		byWorker[event.WorkerID] = ws
	}

	out := make([]WorkerStatus, 0, len(byWorker))
	for _, ws := range byWorker {
		out = append(out, ws)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].WorkerID < out[j].WorkerID
	})
	return out
}

func mustJSONRaw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func maxI64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
