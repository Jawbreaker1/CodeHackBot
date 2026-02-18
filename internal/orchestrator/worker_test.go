package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_WORKER_HELPER") != "1" {
		return
	}
	mode := os.Getenv("WORKER_HELPER_MODE")
	switch mode {
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "fail":
		os.Exit(1)
	default:
		os.Exit(0)
	}
}

func TestWorkerManagerLaunchStopCleanup(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-1"
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

	wm := NewWorkerManager(m)
	spec := helperWorkerSpec(t, "worker-1", "sleep")
	if err := wm.Launch(runID, spec); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if wm.RunningCount() != 1 {
		t.Fatalf("expected one running worker")
	}

	if err := wm.Stop(runID, "worker-1", 10*time.Millisecond); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return wm.RunningCount() == 0 && wm.CompletedCount() == 1
	})

	events, err := m.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var started, stopped bool
	for _, event := range events {
		if event.WorkerID != "worker-1" {
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
		t.Fatalf("expected worker started and stopped events")
	}

	removed := wm.CleanupCompleted(0)
	if removed != 1 {
		t.Fatalf("expected one cleanup removal, got %d", removed)
	}
}

func TestWorkerManagerRestartFailed(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-2"
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

	wm := NewWorkerManager(m)
	spec := helperWorkerSpec(t, "worker-1", "fail")
	if err := wm.Launch(runID, spec); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return wm.RunningCount() == 0 && wm.CompletedCount() == 1
	})

	if err := wm.RestartFailed(runID, "worker-1"); err != nil {
		t.Fatalf("RestartFailed: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return wm.RunningCount() == 0 && wm.CompletedCount() == 1
	})

	events, err := m.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	startCount := 0
	for _, event := range events {
		if event.WorkerID == "worker-1" && event.Type == EventTypeWorkerStarted {
			startCount++
		}
	}
	if startCount < 2 {
		t.Fatalf("expected at least two worker_started events after restart, got %d", startCount)
	}
}

func TestWorkerManagerEmitsHeartbeat(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-heartbeat"
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

	wm := NewWorkerManager(m)
	wm.HeartbeatInterval = 20 * time.Millisecond
	spec := helperWorkerSpec(t, "worker-hb-1", "sleep")
	if err := wm.Launch(runID, spec); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	waitUntil(t, time.Second, func() bool {
		events, err := m.Events(runID, 0)
		if err != nil {
			return false
		}
		for _, event := range events {
			if event.WorkerID == "worker-hb-1" && event.Type == EventTypeWorkerHeartbeat {
				return true
			}
		}
		return false
	})
	_ = wm.Stop(runID, "worker-hb-1", 10*time.Millisecond)
	waitUntil(t, 3*time.Second, func() bool {
		return wm.RunningCount() == 0
	})
}

func TestWorkerManagerCreatesPerWorkerWorkspace(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-workspace"
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
	wm := NewWorkerManager(m)
	spec := helperWorkerSpec(t, "worker-ws-1", "ok")
	spec.Dir = ""
	if err := wm.Launch(runID, spec); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return wm.RunningCount() == 0
	})
	workspace := filepath.Join(base, runID, "orchestrator", "workers", "worker-ws-1")
	info, err := os.Stat(workspace)
	if err != nil {
		t.Fatalf("stat workspace: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workspace is not dir: %s", workspace)
	}
}

func TestWorkerManagerPathToolExecutionOutsideSessionDir(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	base := t.TempDir()
	runID := "run-worker-path"
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
	wm := NewWorkerManager(m)
	spec := WorkerSpec{
		WorkerID: "worker-path-1",
		Command:  "sh",
		Args:     []string{"-c", "command -v sh >/dev/null"},
		Env:      os.Environ(),
	}
	if err := wm.Launch(runID, spec); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return wm.RunningCount() == 0 && wm.CompletedCount() == 1
	})
}

func helperWorkerSpec(t *testing.T, workerID, mode string) WorkerSpec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	// Ensure helper process does not inherit stale helper flags.
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GO_WANT_WORKER_HELPER=") {
			continue
		}
		if strings.HasPrefix(entry, "WORKER_HELPER_MODE=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "GO_WANT_WORKER_HELPER=1", "WORKER_HELPER_MODE="+mode)
	return WorkerSpec{
		WorkerID: workerID,
		Command:  exe,
		Args:     []string{"-test.run=TestWorkerHelperProcess"},
		Env:      env,
		Dir:      filepath.Dir(exe),
	}
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
