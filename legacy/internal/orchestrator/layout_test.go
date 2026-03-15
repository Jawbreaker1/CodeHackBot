package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRunLayout(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	paths, err := EnsureRunLayout(base, "run-1")
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	dirs := []string{
		paths.PlanDir,
		paths.TaskDir,
		paths.EventDir,
		paths.ArtifactDir,
		paths.FindingDir,
	}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected directory: %s", dir)
		}
	}
	wantRoot := filepath.Join(base, "run-1", "orchestrator")
	if paths.Root != wantRoot {
		t.Fatalf("root mismatch: got %s want %s", paths.Root, wantRoot)
	}
}
