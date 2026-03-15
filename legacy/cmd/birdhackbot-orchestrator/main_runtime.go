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
			return finalizeAbortedRun(manager, coord, runID, tick, stopGrace, stdout, stderr, announce, "run interrupted")
		case <-ticker.C:
			status, err := manager.Status(runID)
			if err != nil {
				fmt.Fprintf(stderr, "run status failed: %v\n", err)
				return 1
			}
			if status.State == "stopped" {
				return finalizeAbortedRun(manager, coord, runID, tick, stopGrace, stdout, stderr, announce, "run stopped")
			}
			if err := coord.Tick(); err != nil {
				fmt.Fprintf(stderr, "run tick failed: %v\n", err)
				return 1
			}
			if coord.Done() {
				if _, err := manager.IngestEvidence(runID); err != nil {
					fmt.Fprintf(stderr, "run final evidence ingest failed: %v\n", err)
					return 1
				}
				outcome, outcomeDetail, err := evaluateRunTerminalOutcome(manager, runID)
				if err != nil {
					fmt.Fprintf(stderr, "run completion evaluation failed: %v\n", err)
					return 1
				}
				if outcome == runOutcomeSuccess {
					_ = manager.SetRunOutcome(runID, outcome)
					if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunCompleted, map[string]any{
						"source":      "run",
						"run_outcome": string(outcome),
						"detail":      outcomeDetail,
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
				_ = manager.SetRunOutcome(runID, outcome)
				if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunStopped, map[string]any{
					"source":      "run_terminal_failure",
					"detail":      outcomeDetail,
					"run_outcome": string(outcome),
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

func finalizeAbortedRun(
	manager *orchestrator.Manager,
	coord *orchestrator.Coordinator,
	runID string,
	tick time.Duration,
	stopGrace time.Duration,
	stdout io.Writer,
	stderr io.Writer,
	announce bool,
	terminalMessage string,
) int {
	if err := coord.StopAll(stopGrace); err != nil {
		fmt.Fprintf(stderr, "run stop failed: %v\n", err)
		return 1
	}
	status, err := manager.Status(runID)
	if err != nil {
		fmt.Fprintf(stderr, "run status failed during terminalization: %v\n", err)
		return 1
	}
	if status.State != "completed" && status.State != "stopped" {
		if err := manager.Stop(runID); err != nil {
			fmt.Fprintf(stderr, "run stop event failed: %v\n", err)
			return 1
		}
		// Wait briefly for event projection to converge before report/status output.
		deadline := time.Now().Add(maxDuration(3*tick, 200*time.Millisecond))
		for time.Now().Before(deadline) {
			current, statusErr := manager.Status(runID)
			if statusErr == nil && (current.State == "stopped" || current.State == "completed") {
				break
			}
			time.Sleep(minDuration(25*time.Millisecond, tick))
		}
	}
	if _, err := manager.IngestEvidence(runID); err != nil {
		fmt.Fprintf(stderr, "run terminal evidence ingest failed: %v\n", err)
		return 1
	}
	if err := manager.SetRunOutcome(runID, orchestrator.RunOutcomeAborted); err != nil {
		fmt.Fprintf(stderr, "run outcome update failed: %v\n", err)
		return 1
	}
	emitRunReport(manager, runID, stdout, stderr, announce)
	if err := manager.SetRunPhase(runID, orchestrator.RunPhaseCompleted); err != nil {
		fmt.Fprintf(stderr, "run phase update failed: %v\n", err)
		return 1
	}
	if announce {
		fmt.Fprintf(stdout, "%s: %s\n", terminalMessage, runID)
	}
	return 0
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a >= b {
		return a
	}
	return b
}

func emitRunReport(manager *orchestrator.Manager, runID string, stdout, stderr io.Writer, announce bool) {
	path, err := manager.AssembleRunReport(runID, "")
	if err != nil {
		if announce {
			fmt.Fprintf(stderr, "report generation failed: %v\n", err)
		}
		return
	}
	if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunReportGenerated, map[string]any{
		"path":   path,
		"source": "run_terminal_outcome",
	}); err != nil && announce {
		fmt.Fprintf(stderr, "report event emit failed: %v\n", err)
	}
	if announce {
		fmt.Fprintf(stdout, "report written: %s\n", path)
	}
}

const runOutcomeSuccess = orchestrator.RunOutcomeSuccess

func evaluateRunTerminalOutcome(manager *orchestrator.Manager, runID string) (orchestrator.RunOutcome, string, error) {
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		return orchestrator.RunOutcomeFailed, "", err
	}
	leases, err := manager.ReadLeases(runID)
	if err != nil {
		return orchestrator.RunOutcomeFailed, "", err
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
		completionGate, err := manager.EvaluateCompletionVerificationGate(runID)
		if err != nil {
			return orchestrator.RunOutcomeFailed, "", err
		}
		if strings.EqualFold(strings.TrimSpace(completionGate.VerificationGate), "fail") {
			detail := strings.TrimSpace(completionGate.VerificationGateReason)
			if detail == "" {
				detail = fmt.Sprintf("unverified completed tasks: %d", completionGate.UnverifiedTasks)
			}
			return orchestrator.RunOutcomeFailed, "completion_verification_gate_failed:" + detail, nil
		}
		truthGate, err := manager.EvaluateReportTruthGate(runID)
		if err != nil {
			return orchestrator.RunOutcomeFailed, "", err
		}
		if strings.EqualFold(strings.TrimSpace(truthGate.VerificationGate), "fail") {
			detail := strings.TrimSpace(truthGate.VerificationGateReason)
			if detail == "" {
				detail = fmt.Sprintf("unverified high-impact claims: %d", truthGate.HighImpactUnverified)
			}
			return orchestrator.RunOutcomeFailed, "report_truth_gate_failed:" + detail, nil
		}
		return runOutcomeSuccess, "all_tasks_completed", nil
	}
	detailParts := make([]string, 0, 2)
	if len(failed) > 0 {
		detailParts = append(detailParts, "failed="+strings.Join(failed, ","))
	}
	if len(incomplete) > 0 {
		detailParts = append(detailParts, "incomplete="+strings.Join(incomplete, ","))
	}
	return orchestrator.RunOutcomeFailed, strings.Join(detailParts, " | "), nil
}
