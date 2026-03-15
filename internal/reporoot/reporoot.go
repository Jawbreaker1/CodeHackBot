package reporoot

import (
	"fmt"
	"os"
	"path/filepath"
)

// Find walks upward from start until it finds a repo root marker.
func Find(start string) (string, error) {
	if start == "" {
		return "", fmt.Errorf("start path is required")
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("abs path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	for {
		if isRepoRoot(abs) {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	return "", fmt.Errorf("repo root not found from %s", start)
}

func isRepoRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return false
	}
	return true
}
