package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type State struct {
	StepsSinceSummary int      `json:"steps_since_summary"`
	LastSummaryAt     string   `json:"last_summary_at"`
	LastSummaryHash   string   `json:"last_summary_hash"`
	RecentLogs        []string `json:"recent_logs"`
}

func LoadState(path string) (State, error) {
	if path == "" {
		return State{}, fmt.Errorf("state path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	return state, nil
}

func SaveState(path string, state State) error {
	if path == "" {
		return fmt.Errorf("state path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}
