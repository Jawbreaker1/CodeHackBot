package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
)

type RunPaths struct {
	Root        string
	PlanDir     string
	TaskDir     string
	EventDir    string
	ArtifactDir string
	FindingDir  string
	MemoryDir   string
}

func BuildRunPaths(baseDir, runID string) RunPaths {
	root := filepath.Join(baseDir, runID, "orchestrator")
	return RunPaths{
		Root:        root,
		PlanDir:     filepath.Join(root, "plan"),
		TaskDir:     filepath.Join(root, "task"),
		EventDir:    filepath.Join(root, "event"),
		ArtifactDir: filepath.Join(root, "artifact"),
		FindingDir:  filepath.Join(root, "finding"),
		MemoryDir:   filepath.Join(root, "memory"),
	}
}

func EnsureRunLayout(baseDir, runID string) (RunPaths, error) {
	if runID == "" {
		return RunPaths{}, fmt.Errorf("run_id is empty")
	}
	paths := BuildRunPaths(baseDir, runID)
	dirs := []string{
		paths.PlanDir,
		paths.TaskDir,
		paths.EventDir,
		paths.ArtifactDir,
		paths.FindingDir,
		paths.MemoryDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return RunPaths{}, fmt.Errorf("create orchestrator dir %s: %w", dir, err)
		}
	}
	return paths, nil
}
