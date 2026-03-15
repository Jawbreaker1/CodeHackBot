package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestExecuteCoordinatorLoopInterruptTerminalizesWithReport(t *testing.T) {
	t.Parallel()

	_, runID, manager, coord := newRuntimeTerminalHarness(t, "run-runtime-interrupt")

	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	var errOut bytes.Buffer
	codeCh := make(chan int, 1)
	go func() {
		codeCh <- executeCoordinatorLoop(ctx, manager, coord, runID, 20*time.Millisecond, 200*time.Millisecond, &out, &errOut, true)
	}()

	waitForEventType(t, manager, runID, orchestrator.EventTypeWorkerStarted, 5*time.Second)
	cancel()

	select {
	case code := <-codeCh:
		if code != 0 {
			t.Fatalf("executeCoordinatorLoop failed: code=%d stderr=%s", code, errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for interrupted loop to exit")
	}

	assertAbortedTerminalization(t, manager, runID)
	if !strings.Contains(out.String(), "run interrupted: "+runID) {
		t.Fatalf("unexpected interrupt output: %q", out.String())
	}
}

func TestExecuteCoordinatorLoopStopEventTerminalizesWithReport(t *testing.T) {
	t.Parallel()

	_, runID, manager, coord := newRuntimeTerminalHarness(t, "run-runtime-stop")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out bytes.Buffer
	var errOut bytes.Buffer
	codeCh := make(chan int, 1)
	go func() {
		codeCh <- executeCoordinatorLoop(ctx, manager, coord, runID, 20*time.Millisecond, 200*time.Millisecond, &out, &errOut, true)
	}()

	waitForEventType(t, manager, runID, orchestrator.EventTypeWorkerStarted, 5*time.Second)
	if err := manager.Stop(runID); err != nil {
		t.Fatalf("manager.Stop: %v", err)
	}

	select {
	case code := <-codeCh:
		if code != 0 {
			t.Fatalf("executeCoordinatorLoop failed: code=%d stderr=%s", code, errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for stopped loop to exit")
	}

	assertAbortedTerminalization(t, manager, runID)
	if !strings.Contains(out.String(), "run stopped: "+runID) {
		t.Fatalf("unexpected stop output: %q", out.String())
	}
}

func newRuntimeTerminalHarness(t *testing.T, runID string) (string, string, *orchestrator.Manager, *orchestrator.Coordinator) {
	t.Helper()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan.json")
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Title:             "long running worker",
				Goal:              "hold worker open for stop/interrupt terminalization test",
				Targets:           []string{"127.0.0.1"},
				DoneWhen:          []string{"manual_stop"},
				FailWhen:          []string{"manual_stop"},
				ExpectedArtifacts: []string{"worker.log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     10,
					MaxToolCalls: 10,
					MaxRuntime:   5 * time.Minute,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	manager := orchestrator.NewManager(base)
	if _, err := manager.Start(planPath, runID); err != nil {
		t.Fatalf("manager.Start: %v", err)
	}
	loadedPlan, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	scheduler, err := orchestrator.NewScheduler(loadedPlan, loadedPlan.MaxParallelism)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := orchestrator.NewWorkerManager(manager)
	broker, err := orchestrator.NewApprovalBroker(orchestrator.PermissionAll, false, 2*time.Minute)
	if err != nil {
		t.Fatalf("NewApprovalBroker: %v", err)
	}
	coord, err := orchestrator.NewCoordinator(
		runID,
		loadedPlan.Scope,
		manager,
		workers,
		scheduler,
		1,
		30*time.Second,
		30*time.Second,
		15*time.Second,
		func(task orchestrator.TaskSpec, attempt int, workerID string) orchestrator.WorkerSpec {
			return orchestrator.WorkerSpec{
				WorkerID: workerID,
				Command:  os.Args[0],
				Args:     []string{"-test.run=TestHelperProcessOrchestratorWorker", "worker-hang"},
				Env:      append(append([]string{}, os.Environ()...), "GO_WANT_HELPER_PROCESS=1"),
			}
		},
		broker,
	)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	if err := coord.Reconcile(); err != nil {
		t.Fatalf("coord.Reconcile: %v", err)
	}
	return base, runID, manager, coord
}

func waitForEventType(t *testing.T, manager *orchestrator.Manager, runID, eventType string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		events, err := manager.Events(runID, 0)
		if err == nil && hasEventType(events, eventType) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for event type %s", eventType)
}

func assertAbortedTerminalization(t *testing.T, manager *orchestrator.Manager, runID string) {
	t.Helper()

	status, err := manager.Status(runID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "stopped" {
		t.Fatalf("expected stopped run state, got %s", status.State)
	}
	if status.ActiveWorkers != 0 || status.RunningTasks != 0 {
		t.Fatalf("expected terminal status with zero active/running counters, got active=%d running=%d", status.ActiveWorkers, status.RunningTasks)
	}

	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if got := orchestrator.NormalizeRunOutcome(plan.Metadata.RunOutcome); got != orchestrator.RunOutcomeAborted {
		t.Fatalf("expected run_outcome=aborted, got %q", plan.Metadata.RunOutcome)
	}
	if got := orchestrator.NormalizeRunPhase(plan.Metadata.RunPhase); got != orchestrator.RunPhaseCompleted {
		t.Fatalf("expected run_phase=completed, got %q", plan.Metadata.RunPhase)
	}

	reportPath, reportReady, err := manager.ResolveRunReportPath(runID)
	if err != nil {
		t.Fatalf("ResolveRunReportPath: %v", err)
	}
	if !reportReady {
		t.Fatalf("expected report_ready=true, report_path=%s", reportPath)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file at %s: %v", reportPath, err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, orchestrator.EventTypeRunStopped) {
		t.Fatalf("expected run_stopped event")
	}
	if !hasEventType(events, orchestrator.EventTypeRunReportGenerated) {
		t.Fatalf("expected run_report_generated event")
	}
}
