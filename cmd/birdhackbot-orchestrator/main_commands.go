package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID string
	var reclaimStartup bool
	var startupTimeout time.Duration
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.BoolVar(&reclaimStartup, "reclaim-startup", false, "reclaim leases that missed startup SLA")
	fs.DurationVar(&startupTimeout, "startup-timeout", 30*time.Second, "startup SLA timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "status requires --run")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	if reclaimStartup {
		reclaimed, err := manager.ReclaimMissedStartup(runID, startupTimeout)
		if err != nil {
			fmt.Fprintf(stderr, "status reclaim failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "reclaimed_startup_leases: %d\n", len(reclaimed))
	}
	status, err := manager.Status(runID)
	if err != nil {
		fmt.Fprintf(stderr, "status failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "run: %s\n", status.RunID)
	fmt.Fprintf(stdout, "state: %s\n", status.State)
	if plan, err := manager.LoadRunPlan(runID); err == nil {
		phase := orchestrator.NormalizeRunPhase(plan.Metadata.RunPhase)
		if phase == "" {
			phase = "-"
		}
		fmt.Fprintf(stdout, "phase: %s\n", phase)
	}
	fmt.Fprintf(stdout, "active_workers: %d\n", status.ActiveWorkers)
	fmt.Fprintf(stdout, "queued_tasks: %d\n", status.QueuedTasks)
	fmt.Fprintf(stdout, "running_tasks: %d\n", status.RunningTasks)
	if reportPath, reportReady, reportErr := manager.ResolveRunReportPath(runID); reportErr == nil {
		fmt.Fprintf(stdout, "report_path: %s\n", reportPath)
		fmt.Fprintf(stdout, "report_ready: %t\n", reportReady)
	}
	return 0
}

func runEvents(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID string
	var limit int
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.IntVar(&limit, "limit", 0, "only print the last N events")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "events requires --run")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	events, err := manager.Events(runID, limit)
	if err != nil {
		fmt.Fprintf(stderr, "events failed: %v\n", err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			fmt.Fprintf(stderr, "events encode failed: %v\n", err)
			return 1
		}
	}
	return 0
}

func runWorkers(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("workers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID string
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "workers requires --run")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	workers, err := manager.Workers(runID)
	if err != nil {
		fmt.Fprintf(stderr, "workers failed: %v\n", err)
		return 1
	}
	for _, worker := range workers {
		fmt.Fprintf(stdout, "%s\t%s\tseq=%d\ttask=%s\n", worker.WorkerID, worker.State, worker.LastSeq, worker.CurrentTask)
	}
	return 0
}

func runApprovals(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("approvals", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID string
	var jsonOutput bool
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.BoolVar(&jsonOutput, "json", false, "print JSON lines instead of human-readable output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "approvals requires --run")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	pending, err := manager.PendingApprovals(runID)
	if err != nil {
		fmt.Fprintf(stderr, "approvals failed: %v\n", err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "no pending approvals")
		return 0
	}
	if !jsonOutput {
		fmt.Fprintf(stdout, "pending approvals: %d\n\n", len(pending))
		for _, req := range pending {
			fmt.Fprintf(stdout, "approval: %s\n", req.ApprovalID)
			fmt.Fprintf(stdout, "task: %s", req.TaskID)
			if strings.TrimSpace(req.TaskTitle) != "" {
				fmt.Fprintf(stdout, " (%s)", strings.TrimSpace(req.TaskTitle))
			}
			fmt.Fprintln(stdout)
			if strings.TrimSpace(req.TaskGoal) != "" {
				fmt.Fprintf(stdout, "goal: %s\n", strings.TrimSpace(req.TaskGoal))
			}
			if strings.TrimSpace(req.TaskStrategy) != "" {
				fmt.Fprintf(stdout, "strategy: %s\n", strings.TrimSpace(req.TaskStrategy))
			}
			if len(req.TaskTargets) > 0 {
				fmt.Fprintf(stdout, "targets: %s\n", strings.Join(req.TaskTargets, ", "))
			}
			fmt.Fprintf(stdout, "risk_tier: %s\n", strings.TrimSpace(req.RiskTier))
			fmt.Fprintf(stdout, "why: %s\n", approvalWhyText(req))
			fmt.Fprintf(stdout, "requested: %s\n", req.Requested.UTC().Format(time.RFC3339))
			fmt.Fprintf(stdout, "expires: %s\n", req.ExpiresAt.UTC().Format(time.RFC3339))
			fmt.Fprintf(stdout, "approve: birdhackbot-orchestrator approve --sessions-dir %s --run %s --approval %s --scope task --reason \"authorized\"\n", sessionsDir, runID, req.ApprovalID)
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "approve all: birdhackbot-orchestrator approve-all --sessions-dir %s --run %s --scope task --reason \"authorized\"\n", sessionsDir, runID)
		return 0
	}
	enc := json.NewEncoder(stdout)
	for _, req := range pending {
		if err := enc.Encode(req); err != nil {
			fmt.Fprintf(stderr, "approvals encode failed: %v\n", err)
			return 1
		}
	}
	return 0
}

func runApproveAll(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("approve-all", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID, scope, actor, reason string
	var expiresIn time.Duration
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.StringVar(&scope, "scope", string(orchestrator.ApprovalScopeTask), "approval scope: once|task|session")
	fs.StringVar(&actor, "actor", "operator", "approval actor")
	fs.StringVar(&reason, "reason", "", "approval reason")
	fs.DurationVar(&expiresIn, "expires-in", 0, "optional grant expiry duration")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "approve-all requires --run")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	pending, err := manager.PendingApprovals(runID)
	if err != nil {
		fmt.Fprintf(stderr, "approve-all failed listing approvals: %v\n", err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "no pending approvals")
		return 0
	}
	approved := 0
	for _, req := range pending {
		if err := manager.SubmitApprovalDecision(runID, req.ApprovalID, true, scope, actor, reason, expiresIn); err != nil {
			fmt.Fprintf(stderr, "approve-all failed for %s: %v\n", req.ApprovalID, err)
			return 1
		}
		approved++
		fmt.Fprintf(stdout, "approved: %s (%s)\n", req.ApprovalID, req.TaskID)
	}
	fmt.Fprintf(stdout, "approved_total: %d\n", approved)
	return 0
}

func approvalWhyText(req orchestrator.PendingApprovalView) string {
	reason := strings.TrimSpace(req.Reason)
	switch strings.TrimSpace(strings.ToLower(req.RiskTier)) {
	case string(orchestrator.RiskActiveProbe):
		if reason == "" {
			return "active probing can interact with targets and therefore requires operator approval in default permission mode"
		}
		return fmt.Sprintf("%s (active probing requires operator approval in default permission mode)", reason)
	case string(orchestrator.RiskExploitControlled):
		if reason == "" {
			return "controlled exploitation attempts require explicit operator approval"
		}
		return fmt.Sprintf("%s (controlled exploitation requires explicit approval)", reason)
	case string(orchestrator.RiskPrivEsc):
		if reason == "" {
			return "privilege-escalation attempts require explicit operator approval"
		}
		return fmt.Sprintf("%s (privilege escalation requires explicit approval)", reason)
	case string(orchestrator.RiskDisruptive):
		if reason == "" {
			return "disruptive actions require explicit opt-in and approval"
		}
		return fmt.Sprintf("%s (disruptive actions require explicit opt-in and approval)", reason)
	default:
		if reason == "" {
			return "policy requires approval for this task risk tier"
		}
		return reason
	}
}

func runApprove(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID, approvalID, scope, actor, reason string
	var expiresIn time.Duration
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.StringVar(&approvalID, "approval", "", "approval id")
	fs.StringVar(&scope, "scope", string(orchestrator.ApprovalScopeTask), "approval scope: once|task|session")
	fs.StringVar(&actor, "actor", "operator", "approval actor")
	fs.StringVar(&reason, "reason", "", "approval reason")
	fs.DurationVar(&expiresIn, "expires-in", 0, "optional grant expiry duration")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(approvalID) == "" {
		fmt.Fprintln(stderr, "approve requires --run and --approval")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	if err := manager.SubmitApprovalDecision(runID, approvalID, true, scope, actor, reason, expiresIn); err != nil {
		fmt.Fprintf(stderr, "approve failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "approved: %s\n", approvalID)
	return 0
}

func runDeny(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("deny", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID, approvalID, actor, reason string
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.StringVar(&approvalID, "approval", "", "approval id")
	fs.StringVar(&actor, "actor", "operator", "approval actor")
	fs.StringVar(&reason, "reason", "", "deny reason")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(approvalID) == "" {
		fmt.Fprintln(stderr, "deny requires --run and --approval")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	if err := manager.SubmitApprovalDecision(runID, approvalID, false, "", actor, reason, 0); err != nil {
		fmt.Fprintf(stderr, "deny failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "denied: %s\n", approvalID)
	return 0
}

func runWorkerStop(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worker-stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID, workerID, actor, reason string
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.StringVar(&workerID, "worker", "", "worker id")
	fs.StringVar(&actor, "actor", "operator", "request actor")
	fs.StringVar(&reason, "reason", "", "stop reason")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(workerID) == "" {
		fmt.Fprintln(stderr, "worker-stop requires --run and --worker")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	if err := manager.SubmitWorkerStopRequest(runID, workerID, actor, reason); err != nil {
		fmt.Fprintf(stderr, "worker-stop failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "worker stop requested: %s\n", workerID)
	return 0
}

func runReport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID, outPath string
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.StringVar(&outPath, "out", "", "optional output path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "report requires --run")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	path, err := manager.AssembleRunReport(runID, outPath)
	if err != nil {
		fmt.Fprintf(stderr, "report failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "report written: %s\n", path)
	return 0
}

func runStop(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID string
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "stop requires --run")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	if err := manager.Stop(runID); err != nil {
		fmt.Fprintf(stderr, "stop failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "run stopped: %s\n", runID)
	return 0
}

func printUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: birdhackbot-orchestrator <command> [flags]")
	fmt.Fprintln(stderr, "commands: start, run, benchmark, status, workers, events, approvals, approve, approve-all, deny, worker-stop, report, stop, tui")
}
