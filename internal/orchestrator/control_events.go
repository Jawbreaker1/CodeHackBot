package orchestrator

import (
	"fmt"
	"strings"
)

func (m *Manager) SubmitWorkerStopRequest(runID, workerID, actor, reason string) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(workerID) == "" {
		return fmt.Errorf("worker id is required")
	}
	payload := map[string]any{
		"target_worker_id": workerID,
		"actor":            actor,
		"reason":           reason,
		"source":           "cli",
	}
	operatorWorkerID := fmt.Sprintf("operator-worker-stop-%d", m.Now().UnixNano())
	return m.EmitEvent(runID, operatorWorkerID, "", EventTypeWorkerStopRequested, payload)
}
