package sessionstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

const Version = 1

// State is the persisted worker session state for v1 resume.
type State struct {
	Version   int                    `json:"version"`
	Status    string                 `json:"status"`
	BaseURL   string                 `json:"base_url,omitempty"`
	Model     string                 `json:"model,omitempty"`
	MaxSteps  int                    `json:"max_steps,omitempty"`
	AllowAll  bool                   `json:"allow_all,omitempty"`
	Packet    ctxpacket.WorkerPacket `json:"packet"`
	Summary   string                 `json:"summary,omitempty"`
	LastError string                 `json:"last_error,omitempty"`
}

func Save(path string, state State) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	state.Version = Version
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func Load(path string) (State, error) {
	if path == "" {
		return State{}, fmt.Errorf("path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	if state.Version != Version {
		return State{}, fmt.Errorf("unsupported state version %d", state.Version)
	}
	return state, nil
}
