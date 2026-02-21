package orchestrator

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWorkerManagerLaunchesRealBirdHackBotBinary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available in PATH")
	}

	base := t.TempDir()
	runID := "run-worker-real-1"
	m := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(m.eventPath(runID), EventEnvelope{
		EventID:  "e1",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	repoRoot := mustRepoRoot(t)
	binPath := filepath.Join(base, "birdhackbot")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binPath, "./cmd/birdhackbot")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real birdhackbot binary: %v\n%s", err, string(out))
	}

	wm := NewWorkerManager(m)
	spec := WorkerSpec{
		WorkerID: "real-worker-1",
		Command:  binPath,
		Args:     []string{"--version"},
		Dir:      repoRoot,
	}
	if err := wm.Launch(runID, spec); err != nil {
		t.Fatalf("Launch real binary: %v", err)
	}
	waitUntil(t, 5*time.Second, func() bool {
		return wm.RunningCount() == 0 && wm.CompletedCount() == 1
	})

	events, err := m.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var started, stopped bool
	for _, event := range events {
		if event.WorkerID != "real-worker-1" {
			continue
		}
		if event.Type == EventTypeWorkerStarted {
			started = true
		}
		if event.Type == EventTypeWorkerStopped {
			stopped = true
		}
	}
	if !started || !stopped {
		t.Fatalf("expected real worker started/stopped events")
	}
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
