package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func executeCoordinatorLoop(
	ctx context.Context,
	manager *orchestrator.Manager,
	coord *orchestrator.Coordinator,
	runID string,
	tick time.Duration,
	stopGrace time.Duration,
	stdout io.Writer,
	stderr io.Writer,
	announce bool,
) int {
	if err := manager.SetRunPhase(runID, orchestrator.RunPhaseExecuting); err != nil {
		fmt.Fprintf(stderr, "run failed setting phase: %v\n", err)
		return 1
	}
	coord.SetRunPhase(orchestrator.RunPhaseExecuting)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = coord.StopAll(stopGrace)
			if status, err := manager.Status(runID); err == nil && status.State != "completed" && status.State != "stopped" {
				_ = manager.Stop(runID)
			}
			_ = manager.SetRunPhase(runID, orchestrator.RunPhaseCompleted)
			if announce {
				fmt.Fprintf(stdout, "run interrupted: %s\n", runID)
			}
			return 0
		case <-ticker.C:
			status, err := manager.Status(runID)
			if err != nil {
				fmt.Fprintf(stderr, "run status failed: %v\n", err)
				return 1
			}
			if status.State == "stopped" {
				_ = coord.StopAll(stopGrace)
				emitRunReport(manager, runID, stdout, stderr, announce)
				_ = manager.SetRunPhase(runID, orchestrator.RunPhaseCompleted)
				if announce {
					fmt.Fprintf(stdout, "run stopped: %s\n", runID)
				}
				return 0
			}
			if err := coord.Tick(); err != nil {
				fmt.Fprintf(stderr, "run tick failed: %v\n", err)
				return 1
			}
			if coord.Done() {
				outcome, outcomeDetail, err := evaluateRunTerminalOutcome(manager, runID)
				if err != nil {
					fmt.Fprintf(stderr, "run completion evaluation failed: %v\n", err)
					return 1
				}
				if outcome == runOutcomeSuccess {
					if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunCompleted, map[string]any{
						"source": "run",
					}); err != nil {
						fmt.Fprintf(stderr, "run completion event failed: %v\n", err)
						return 1
					}
					emitRunReport(manager, runID, stdout, stderr, announce)
					_ = manager.SetRunPhase(runID, orchestrator.RunPhaseCompleted)
					if announce {
						fmt.Fprintf(stdout, "run completed: %s\n", runID)
					}
					return 0
				}
				if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunStopped, map[string]any{
					"source": "run_terminal_failure",
					"detail": outcomeDetail,
				}); err != nil {
					fmt.Fprintf(stderr, "run failure event failed: %v\n", err)
					return 1
				}
				emitRunReport(manager, runID, stdout, stderr, announce)
				_ = manager.SetRunPhase(runID, orchestrator.RunPhaseCompleted)
				if announce {
					fmt.Fprintf(stdout, "run stopped with failures: %s (%s)\n", runID, outcomeDetail)
				}
				return 1
			}
		}
	}
}

func emitRunReport(manager *orchestrator.Manager, runID string, stdout, stderr io.Writer, announce bool) {
	path, err := manager.AssembleRunReport(runID, "")
	if err != nil {
		if announce {
			fmt.Fprintf(stderr, "report generation failed: %v\n", err)
		}
		return
	}
	if announce {
		fmt.Fprintf(stdout, "report written: %s\n", path)
	}
}

type runOutcome string

const (
	runOutcomeSuccess runOutcome = "success"
	runOutcomeFailure runOutcome = "failure"
)

func evaluateRunTerminalOutcome(manager *orchestrator.Manager, runID string) (runOutcome, string, error) {
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		return runOutcomeFailure, "", err
	}
	leases, err := manager.ReadLeases(runID)
	if err != nil {
		return runOutcomeFailure, "", err
	}
	leaseStateByTask := map[string]string{}
	for _, lease := range leases {
		leaseStateByTask[lease.TaskID] = strings.TrimSpace(lease.Status)
	}
	failed := make([]string, 0)
	incomplete := make([]string, 0)
	for _, task := range plan.Tasks {
		status := strings.TrimSpace(leaseStateByTask[task.TaskID])
		if status == "" {
			status = orchestrator.LeaseStatusQueued
		}
		switch status {
		case orchestrator.LeaseStatusCompleted:
			continue
		case orchestrator.LeaseStatusFailed, orchestrator.LeaseStatusBlocked, orchestrator.LeaseStatusCanceled:
			failed = append(failed, fmt.Sprintf("%s:%s", task.TaskID, status))
		default:
			incomplete = append(incomplete, fmt.Sprintf("%s:%s", task.TaskID, status))
		}
	}
	if len(failed) == 0 && len(incomplete) == 0 {
		return runOutcomeSuccess, "all_tasks_completed", nil
	}
	detailParts := make([]string, 0, 2)
	if len(failed) > 0 {
		detailParts = append(detailParts, "failed="+strings.Join(failed, ","))
	}
	if len(incomplete) > 0 {
		detailParts = append(detailParts, "incomplete="+strings.Join(incomplete, ","))
	}
	return runOutcomeFailure, strings.Join(detailParts, " | "), nil
}
