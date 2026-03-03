package orchestrator

import (
	"encoding/json"
	"os"
	"strings"
)

func workerAttemptAlreadyCompleted(manager *Manager, cfg WorkerRunConfig) (bool, error) {
	if _, err := os.Stat(manager.eventPath(cfg.RunID)); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	events, err := manager.Events(cfg.RunID, 0)
	if err != nil {
		return false, err
	}
	workerID := WorkerSignalID(cfg.WorkerID)
	for _, event := range events {
		if event.TaskID != cfg.TaskID || event.WorkerID != workerID || event.Type != EventTypeTaskCompleted {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if attempt, ok := payloadInt(payload["attempt"]); ok && attempt == cfg.Attempt {
			return true, nil
		}
	}
	return false, nil
}

func primaryTaskTarget(task TaskSpec) string {
	if len(task.Targets) > 0 && strings.TrimSpace(task.Targets[0]) != "" {
		return strings.TrimSpace(task.Targets[0])
	}
	return "local"
}

func workerFailureAlreadyRecorded(manager *Manager, cfg WorkerRunConfig) (bool, error) {
	events, err := manager.Events(cfg.RunID, 0)
	if err != nil {
		return false, err
	}
	workerID := WorkerSignalID(cfg.WorkerID)
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.TaskID != cfg.TaskID || event.WorkerID != workerID || event.Type != EventTypeTaskFailed {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		attempt, ok := payloadInt(payload["attempt"])
		if !ok || attempt == cfg.Attempt {
			return true, nil
		}
	}
	return false, nil
}
