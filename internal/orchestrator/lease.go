package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	LeaseStatusLeased           = "leased"
	LeaseStatusQueued           = "queued"
	LeaseStatusAwaitingApproval = "awaiting_approval"
	LeaseStatusRunning          = "running"
	LeaseStatusCompleted        = "completed"
	LeaseStatusFailed           = "failed"
	LeaseStatusBlocked          = "blocked"
	LeaseStatusCanceled         = "canceled"
)

func (m *Manager) WriteLease(runID string, lease TaskLease) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	if err := ValidateTaskLease(lease); err != nil {
		return err
	}
	paths := BuildRunPaths(m.SessionsDir, runID)
	if err := os.MkdirAll(paths.TaskDir, 0o755); err != nil {
		return fmt.Errorf("create task dir: %w", err)
	}
	if err := WriteJSONAtomic(leaseFilePath(paths.TaskDir, lease.TaskID), lease); err != nil {
		return fmt.Errorf("write lease: %w", err)
	}

	seq, err := m.nextSeq(runID, orchestratorWorkerID)
	if err != nil {
		return err
	}
	payload := mustJSONRaw(map[string]any{
		"lease_id":        lease.LeaseID,
		"status":          lease.Status,
		"attempt":         lease.Attempt,
		"assigned_worker": lease.WorkerID,
		"deadline":        lease.Deadline,
	})
	return AppendEventJSONL(m.eventPath(runID), EventEnvelope{
		EventID:  NewEventID(),
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   lease.TaskID,
		Seq:      seq,
		TS:       m.Now(),
		Type:     EventTypeTaskLeased,
		Payload:  payload,
	})
}

func (m *Manager) ReadLeases(runID string) ([]TaskLease, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("run id is required")
	}
	taskDir := BuildRunPaths(m.SessionsDir, runID).TaskDir
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, fmt.Errorf("read task dir: %w", err)
	}
	out := make([]TaskLease, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lease.json") {
			continue
		}
		path := filepath.Join(taskDir, entry.Name())
		lease, err := readLease(path)
		if err != nil {
			return nil, err
		}
		out = append(out, lease)
	}
	return out, nil
}

func (m *Manager) ReadLease(runID, taskID string) (TaskLease, error) {
	if strings.TrimSpace(taskID) == "" {
		return TaskLease{}, fmt.Errorf("task id is required")
	}
	taskDir := BuildRunPaths(m.SessionsDir, runID).TaskDir
	return readLease(leaseFilePath(taskDir, taskID))
}

func (m *Manager) UpdateLeaseStatus(runID, taskID, status, workerID string) error {
	lease, err := m.ReadLease(runID, taskID)
	if err != nil {
		return err
	}
	lease.Status = status
	lease.WorkerID = workerID
	taskDir := BuildRunPaths(m.SessionsDir, runID).TaskDir
	return WriteJSONAtomic(leaseFilePath(taskDir, taskID), lease)
}

func (m *Manager) ReclaimMissedStartup(runID string, startupTimeout time.Duration) ([]TaskLease, error) {
	if startupTimeout <= 0 {
		return nil, fmt.Errorf("startup timeout must be > 0")
	}
	leases, err := m.ReadLeases(runID)
	if err != nil {
		return nil, err
	}
	events, err := m.Events(runID, 0)
	if err != nil {
		return nil, err
	}

	reclaimed := make([]TaskLease, 0)
	now := m.Now()
	for _, lease := range leases {
		if lease.Status != LeaseStatusLeased {
			continue
		}
		if now.Before(lease.StartedAt.Add(startupTimeout)) {
			continue
		}
		if hasTaskStarted(events, lease.TaskID, lease.WorkerID, lease.StartedAt) {
			continue
		}

		lease.Status = LeaseStatusQueued
		lease.WorkerID = ""
		lease.StartedAt = now
		lease.Deadline = now.Add(startupTimeout)
		if err := WriteJSONAtomic(leaseFilePath(BuildRunPaths(m.SessionsDir, runID).TaskDir, lease.TaskID), lease); err != nil {
			return nil, fmt.Errorf("rewrite reclaimed lease: %w", err)
		}
		if err := m.emitReclaimedEvents(runID, lease); err != nil {
			return nil, err
		}
		reclaimed = append(reclaimed, lease)
	}
	return reclaimed, nil
}

func (m *Manager) ReclaimStaleLeases(runID string, staleTimeout time.Duration, isWorkerRunning func(string) bool) ([]TaskLease, error) {
	if staleTimeout <= 0 {
		return nil, fmt.Errorf("stale timeout must be > 0")
	}
	leases, err := m.ReadLeases(runID)
	if err != nil {
		return nil, err
	}
	events, err := m.Events(runID, 0)
	if err != nil {
		return nil, err
	}

	reclaimed := make([]TaskLease, 0)
	now := m.Now()
	for _, lease := range leases {
		if lease.Status != LeaseStatusRunning && lease.Status != LeaseStatusLeased {
			continue
		}
		if isWorkerRunning != nil && isWorkerRunning(lease.WorkerID) {
			continue
		}
		lastSignal, ok := latestWorkerSignal(events, lease.WorkerID, lease.StartedAt)
		if !ok {
			lastSignal = lease.StartedAt
		}
		if now.Sub(lastSignal) < staleTimeout {
			continue
		}
		lease.Status = LeaseStatusQueued
		lease.WorkerID = ""
		lease.StartedAt = now
		lease.Deadline = now.Add(staleTimeout)
		if err := WriteJSONAtomic(leaseFilePath(BuildRunPaths(m.SessionsDir, runID).TaskDir, lease.TaskID), lease); err != nil {
			return nil, fmt.Errorf("rewrite stale reclaimed lease: %w", err)
		}
		if err := m.emitStaleReclaimedEvents(runID, lease); err != nil {
			return nil, err
		}
		reclaimed = append(reclaimed, lease)
	}
	return reclaimed, nil
}

func (m *Manager) emitReclaimedEvents(runID string, lease TaskLease) error {
	seq, err := m.nextSeq(runID, orchestratorWorkerID)
	if err != nil {
		return err
	}
	fail := EventEnvelope{
		EventID:  NewEventID(),
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   lease.TaskID,
		Seq:      seq,
		TS:       m.Now(),
		Type:     EventTypeTaskFailed,
		Payload: mustJSONRaw(map[string]any{
			"lease_id": lease.LeaseID,
			"reason":   "startup_sla_missed",
			"reclaim":  true,
		}),
	}
	if err := AppendEventJSONL(m.eventPath(runID), fail); err != nil {
		return err
	}
	seq++
	requeued := EventEnvelope{
		EventID:  NewEventID(),
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   lease.TaskID,
		Seq:      seq,
		TS:       m.Now(),
		Type:     EventTypeTaskLeased,
		Payload: mustJSONRaw(map[string]any{
			"lease_id":        lease.LeaseID,
			"status":          lease.Status,
			"assigned_worker": "",
			"requeue_reason":  "startup_sla_missed",
		}),
	}
	return AppendEventJSONL(m.eventPath(runID), requeued)
}

func (m *Manager) emitStaleReclaimedEvents(runID string, lease TaskLease) error {
	seq, err := m.nextSeq(runID, orchestratorWorkerID)
	if err != nil {
		return err
	}
	fail := EventEnvelope{
		EventID:  NewEventID(),
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   lease.TaskID,
		Seq:      seq,
		TS:       m.Now(),
		Type:     EventTypeTaskFailed,
		Payload: mustJSONRaw(map[string]any{
			"lease_id": lease.LeaseID,
			"reason":   "stale_lease",
			"reclaim":  true,
		}),
	}
	if err := AppendEventJSONL(m.eventPath(runID), fail); err != nil {
		return err
	}
	seq++
	requeued := EventEnvelope{
		EventID:  NewEventID(),
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   lease.TaskID,
		Seq:      seq,
		TS:       m.Now(),
		Type:     EventTypeTaskLeased,
		Payload: mustJSONRaw(map[string]any{
			"lease_id":        lease.LeaseID,
			"status":          lease.Status,
			"assigned_worker": "",
			"requeue_reason":  "stale_lease",
		}),
	}
	return AppendEventJSONL(m.eventPath(runID), requeued)
}

func leaseFilePath(taskDir, taskID string) string {
	return filepath.Join(taskDir, taskID+".lease.json")
}

func hasTaskStarted(events []EventEnvelope, taskID, workerID string, notBefore time.Time) bool {
	for _, event := range events {
		if event.TaskID != taskID {
			continue
		}
		if event.Type != EventTypeTaskStarted {
			continue
		}
		if workerID != "" && event.WorkerID != workerID {
			continue
		}
		if event.TS.Before(notBefore) {
			continue
		}
		return true
	}
	return false
}

func latestWorkerSignal(events []EventEnvelope, workerID string, notBefore time.Time) (time.Time, bool) {
	var last time.Time
	found := false
	for _, event := range events {
		if event.WorkerID != workerID {
			continue
		}
		switch event.Type {
		case EventTypeWorkerStarted, EventTypeWorkerHeartbeat, EventTypeTaskStarted, EventTypeTaskProgress:
		default:
			continue
		}
		if event.TS.Before(notBefore) {
			continue
		}
		if !found || event.TS.After(last) {
			last = event.TS
			found = true
		}
	}
	return last, found
}

func latestTaskSignal(events []EventEnvelope, taskID string, notBefore time.Time) (time.Time, bool) {
	var last time.Time
	found := false
	for _, event := range events {
		if event.TaskID != taskID {
			continue
		}
		switch event.Type {
		case EventTypeTaskStarted, EventTypeTaskProgress:
		default:
			continue
		}
		if event.TS.Before(notBefore) {
			continue
		}
		if !found || event.TS.After(last) {
			last = event.TS
			found = true
		}
	}
	return last, found
}
