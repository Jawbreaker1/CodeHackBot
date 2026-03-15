package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Meta struct {
	ID        string `json:"id"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Status    string `json:"status"`
}

func MetaPath(sessionDir string) string {
	return filepath.Join(sessionDir, "session.json")
}

func InitMeta(sessionDir, sessionID string) error {
	path := MetaPath(sessionDir)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	meta := Meta{
		ID:        sessionID,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Status:    "active",
	}
	return writeMeta(path, meta)
}

func CloseMeta(sessionDir, sessionID string) error {
	path := MetaPath(sessionDir)
	meta, err := readMeta(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		meta = Meta{
			ID:        sessionID,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
	if meta.ID == "" {
		meta.ID = sessionID
	}
	meta.Status = "closed"
	meta.EndedAt = time.Now().UTC().Format(time.RFC3339)
	return writeMeta(path, meta)
}

func readMeta(path string) (Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, fmt.Errorf("parse meta: %w", err)
	}
	return meta, nil
}

func writeMeta(path string, meta Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write meta: %w", err)
	}
	return nil
}
