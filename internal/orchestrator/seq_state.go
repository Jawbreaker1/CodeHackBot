package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sequenceState struct {
	Workers map[string]int64 `json:"workers"`
}

func (m *Manager) nextSeqLocked(runID, workerID string) (int64, error) {
	if strings.TrimSpace(runID) == "" {
		return 0, fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(workerID) == "" {
		return 0, fmt.Errorf("worker id is required")
	}
	state, err := m.loadSeqStateLocked(runID)
	if err != nil {
		return 0, err
	}
	prev := state[workerID]
	next := prev + 1
	state[workerID] = next
	if err := m.writeSeqStateLocked(runID, state); err != nil {
		state[workerID] = prev
		return 0, err
	}
	return next, nil
}

func (m *Manager) loadSeqStateLocked(runID string) (map[string]int64, error) {
	if state, ok := m.seqState[runID]; ok && state != nil {
		return state, nil
	}
	workers := map[string]int64{}
	path := m.seqStatePath(runID)
	data, err := os.ReadFile(path)
	if err == nil {
		var parsed sequenceState
		if parseErr := json.Unmarshal(data, &parsed); parseErr != nil {
			return nil, fmt.Errorf("parse seq state: %w", parseErr)
		}
		for workerID, seq := range parsed.Workers {
			if strings.TrimSpace(workerID) == "" || seq <= 0 {
				continue
			}
			workers[workerID] = seq
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read seq state: %w", err)
	}

	if err := m.mergeSeqStateFromEventLogLocked(runID, workers); err != nil {
		return nil, err
	}
	if err := m.writeSeqStateLocked(runID, workers); err != nil {
		return nil, err
	}
	m.seqState[runID] = workers
	return workers, nil
}

func (m *Manager) mergeSeqStateFromEventLogLocked(runID string, state map[string]int64) error {
	path := m.eventPath(runID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat event log: %w", err)
	}
	events, _, _, err := readEventsFromOffsetResilient(path, 0)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.Seq > state[event.WorkerID] {
			state[event.WorkerID] = event.Seq
		}
	}
	return nil
}

func (m *Manager) writeSeqStateLocked(runID string, state map[string]int64) error {
	copyMap := make(map[string]int64, len(state))
	for workerID, seq := range state {
		if strings.TrimSpace(workerID) == "" || seq <= 0 {
			continue
		}
		copyMap[workerID] = seq
	}
	return WriteJSONAtomic(m.seqStatePath(runID), sequenceState{Workers: copyMap})
}

func (m *Manager) seqStatePath(runID string) string {
	return filepath.Join(BuildRunPaths(m.SessionsDir, runID).EventDir, "seq_state.json")
}
