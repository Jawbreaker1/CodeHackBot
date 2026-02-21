package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type WorkerSpec struct {
	WorkerID string
	Command  string
	Args     []string
	Env      []string
	Dir      string
}

type WorkerManager struct {
	manager           *Manager
	HeartbeatInterval time.Duration
	mu                sync.Mutex
	running           map[string]*runningWorker
	completed         map[string]completedWorker
}

type runningWorker struct {
	spec     WorkerSpec
	cmd      *exec.Cmd
	runID    string
	started  time.Time
	attempts int
	stopHB   chan struct{}
	logPath  string
	logFile  *os.File
}

type completedWorker struct {
	spec     WorkerSpec
	exitedAt time.Time
	failed   bool
	attempts int
	errorMsg string
	runID    string
	workerID string
	logPath  string
}

type WorkerExit struct {
	RunID    string
	WorkerID string
	Failed   bool
	Attempts int
	ErrorMsg string
	ExitedAt time.Time
	LogPath  string
}

func NewWorkerManager(manager *Manager) *WorkerManager {
	return &WorkerManager{
		manager:           manager,
		HeartbeatInterval: 5 * time.Second,
		running:           map[string]*runningWorker{},
		completed:         map[string]completedWorker{},
	}
}

func (wm *WorkerManager) Launch(runID string, spec WorkerSpec) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(spec.WorkerID) == "" {
		return fmt.Errorf("worker id is required")
	}
	if strings.TrimSpace(spec.Command) == "" {
		return fmt.Errorf("worker command is required")
	}

	key := workerKey(runID, spec.WorkerID)
	wm.mu.Lock()
	if _, ok := wm.running[key]; ok {
		wm.mu.Unlock()
		return fmt.Errorf("worker already running: %s", spec.WorkerID)
	}
	previous := wm.completed[key]
	attempts := previous.attempts + 1
	wm.mu.Unlock()

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Env = spec.Env
	workDir := spec.Dir
	if strings.TrimSpace(workDir) == "" {
		workDir = wm.workerWorkspaceDir(runID, spec.WorkerID)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create worker workspace: %w", err)
	}
	cmd.Dir = workDir
	logPath := filepath.Join(workDir, "worker.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open worker log: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureWorkerProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start worker %s: %w", spec.WorkerID, err)
	}

	seq, err := wm.manager.nextSeq(runID, spec.WorkerID)
	if err != nil {
		terminateWorkerProcess(cmd, 0)
		return err
	}
	now := wm.manager.Now()
	if err := AppendEventJSONL(wm.manager.eventPath(runID), EventEnvelope{
		EventID:  NewEventID(),
		RunID:    runID,
		WorkerID: spec.WorkerID,
		Seq:      seq,
		TS:       now,
		Type:     EventTypeWorkerStarted,
		Payload: mustJSONRaw(map[string]any{
			"pid":      cmd.Process.Pid,
			"command":  spec.Command,
			"args":     spec.Args,
			"attempts": attempts,
			"log_path": logPath,
		}),
	}); err != nil {
		terminateWorkerProcess(cmd, 0)
		_ = logFile.Close()
		return err
	}

	wm.mu.Lock()
	wm.running[key] = &runningWorker{
		spec:     spec,
		cmd:      cmd,
		runID:    runID,
		started:  now,
		attempts: attempts,
		stopHB:   make(chan struct{}),
		logPath:  logPath,
		logFile:  logFile,
	}
	wm.mu.Unlock()

	wm.startHeartbeatLoop(key)
	go wm.waitWorker(key)
	return nil
}

func (wm *WorkerManager) Stop(runID, workerID string, grace time.Duration) error {
	key := workerKey(runID, workerID)
	wm.mu.Lock()
	worker, ok := wm.running[key]
	wm.mu.Unlock()
	if !ok {
		return fmt.Errorf("worker not running: %s", workerID)
	}
	terminateWorkerProcess(worker.cmd, grace)
	return nil
}

func (wm *WorkerManager) RestartFailed(runID, workerID string) error {
	key := workerKey(runID, workerID)
	wm.mu.Lock()
	entry, ok := wm.completed[key]
	wm.mu.Unlock()
	if !ok {
		return fmt.Errorf("worker has no completed state: %s", workerID)
	}
	if !entry.failed {
		return fmt.Errorf("worker did not fail: %s", workerID)
	}
	return wm.Launch(runID, entry.spec)
}

func (wm *WorkerManager) IsRunning(runID, workerID string) bool {
	key := workerKey(runID, workerID)
	wm.mu.Lock()
	_, ok := wm.running[key]
	wm.mu.Unlock()
	return ok
}

func (wm *WorkerManager) CleanupCompleted(maxAge time.Duration) int {
	cutoff := wm.manager.Now().Add(-maxAge)
	removed := 0
	wm.mu.Lock()
	for key, worker := range wm.completed {
		if worker.exitedAt.Before(cutoff) || maxAge <= 0 {
			delete(wm.completed, key)
			removed++
		}
	}
	wm.mu.Unlock()
	return removed
}

func (wm *WorkerManager) RunningCount() int {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	return len(wm.running)
}

func (wm *WorkerManager) CompletedCount() int {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	return len(wm.completed)
}

func (wm *WorkerManager) DrainCompleted(runID string) []WorkerExit {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	out := make([]WorkerExit, 0, len(wm.completed))
	for key, entry := range wm.completed {
		if runID != "" && entry.runID != runID {
			continue
		}
		out = append(out, WorkerExit{
			RunID:    entry.runID,
			WorkerID: entry.workerID,
			Failed:   entry.failed,
			Attempts: entry.attempts,
			ErrorMsg: entry.errorMsg,
			ExitedAt: entry.exitedAt,
			LogPath:  entry.logPath,
		})
		delete(wm.completed, key)
	}
	return out
}

func (wm *WorkerManager) waitWorker(key string) {
	wm.mu.Lock()
	worker, ok := wm.running[key]
	wm.mu.Unlock()
	if !ok {
		return
	}
	err := worker.cmd.Wait()
	if worker.logFile != nil {
		_ = worker.logFile.Close()
	}
	now := wm.manager.Now()
	failed := err != nil
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	close(worker.stopHB)
	wm.mu.Lock()
	delete(wm.running, key)
	attempts := worker.attempts
	wm.completed[key] = completedWorker{
		spec:     worker.spec,
		exitedAt: now,
		failed:   failed,
		attempts: attempts,
		errorMsg: errMsg,
		runID:    worker.runID,
		workerID: worker.spec.WorkerID,
		logPath:  worker.logPath,
	}
	wm.mu.Unlock()

	payload := map[string]any{
		"pid":      worker.cmd.Process.Pid,
		"status":   "completed",
		"attempts": attempts,
	}
	if failed {
		payload["status"] = "failed"
		payload["error"] = errMsg
	}
	if strings.TrimSpace(worker.logPath) != "" {
		payload["log_path"] = worker.logPath
	}
	seq, seqErr := wm.claimNextSeq(worker.runID, worker.spec.WorkerID)
	if seqErr != nil {
		return
	}
	_ = AppendEventJSONL(wm.manager.eventPath(worker.runID), EventEnvelope{
		EventID:  NewEventID(),
		RunID:    worker.runID,
		WorkerID: worker.spec.WorkerID,
		Seq:      seq,
		TS:       now,
		Type:     EventTypeWorkerStopped,
		Payload:  mustJSONRaw(payload),
	})
}

func (wm *WorkerManager) startHeartbeatLoop(key string) {
	wm.mu.Lock()
	worker, ok := wm.running[key]
	interval := wm.HeartbeatInterval
	wm.mu.Unlock()
	if !ok || interval <= 0 {
		return
	}
	go func(runID, workerID string, stop <-chan struct{}, started time.Time) {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				seq, err := wm.claimNextSeq(runID, workerID)
				if err != nil {
					return
				}
				_ = AppendEventJSONL(wm.manager.eventPath(runID), EventEnvelope{
					EventID:  NewEventID(),
					RunID:    runID,
					WorkerID: workerID,
					Seq:      seq,
					TS:       wm.manager.Now(),
					Type:     EventTypeWorkerHeartbeat,
					Payload: mustJSONRaw(map[string]any{
						"uptime_seconds": int(wm.manager.Now().Sub(started).Seconds()),
					}),
				})
			}
		}
	}(worker.runID, worker.spec.WorkerID, worker.stopHB, worker.started)
}

func (wm *WorkerManager) claimNextSeq(runID, workerID string) (int64, error) {
	return wm.manager.nextSeq(runID, workerID)
}

func workerKey(runID, workerID string) string {
	return runID + "::" + workerID
}

func (wm *WorkerManager) workerWorkspaceDir(runID, workerID string) string {
	return filepath.Join(wm.manager.SessionsDir, runID, "orchestrator", "workers", workerID)
}
