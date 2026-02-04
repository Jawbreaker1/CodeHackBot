package memory

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	SummaryFilename = "summary.md"
	FactsFilename   = "known_facts.md"
	FocusFilename   = "focus.md"
	StateFilename   = "context.json"
)

type Artifacts struct {
	SummaryPath string
	FactsPath   string
	FocusPath   string
	StatePath   string
}

func EnsureArtifacts(sessionDir string) (Artifacts, error) {
	if sessionDir == "" {
		return Artifacts{}, fmt.Errorf("session dir is empty")
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return Artifacts{}, fmt.Errorf("create session dir: %w", err)
	}

	paths := Artifacts{
		SummaryPath: filepath.Join(sessionDir, SummaryFilename),
		FactsPath:   filepath.Join(sessionDir, FactsFilename),
		FocusPath:   filepath.Join(sessionDir, FocusFilename),
		StatePath:   filepath.Join(sessionDir, StateFilename),
	}

	if _, err := os.Stat(paths.SummaryPath); os.IsNotExist(err) {
		if err := os.WriteFile(paths.SummaryPath, []byte(defaultSummaryContent()), 0o644); err != nil {
			return Artifacts{}, fmt.Errorf("write summary: %w", err)
		}
	}
	if _, err := os.Stat(paths.FactsPath); os.IsNotExist(err) {
		if err := os.WriteFile(paths.FactsPath, []byte(defaultFactsContent()), 0o644); err != nil {
			return Artifacts{}, fmt.Errorf("write known facts: %w", err)
		}
	}
	if _, err := os.Stat(paths.FocusPath); os.IsNotExist(err) {
		if err := os.WriteFile(paths.FocusPath, []byte(defaultFocusContent()), 0o644); err != nil {
			return Artifacts{}, fmt.Errorf("write focus: %w", err)
		}
	}
	if _, err := os.Stat(paths.StatePath); os.IsNotExist(err) {
		if err := SaveState(paths.StatePath, State{}); err != nil {
			return Artifacts{}, err
		}
	}

	return paths, nil
}

func defaultSummaryContent() string {
	return "# Session Summary\n\n- Summary pending.\n"
}

func defaultFactsContent() string {
	return "# Known Facts\n\n- None recorded.\n"
}

func defaultFocusContent() string {
	return "# Current Focus\n\n- Not set.\n"
}
