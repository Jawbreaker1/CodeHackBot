package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

const version = "dev"

type stringFlags []string

func (s *stringFlags) String() string {
	return strings.Join(*s, ",")
}

func (s *stringFlags) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var showVersion bool
	root := flag.NewFlagSet("birdhackbot-orchestrator", flag.ContinueOnError)
	root.SetOutput(stderr)
	root.BoolVar(&showVersion, "version", false, "print version and exit")
	if err := root.Parse(args); err != nil {
		return 2
	}

	if showVersion {
		fmt.Fprintf(stdout, "birdhackbot-orchestrator %s\n", version)
		return 0
	}

	rest := root.Args()
	if len(rest) == 0 {
		printUsage(stderr)
		return 2
	}

	switch rest[0] {
	case "start":
		return runStart(rest[1:], stdout, stderr)
	case "run":
		return runRun(rest[1:], stdout, stderr)
	case "status":
		return runStatus(rest[1:], stdout, stderr)
	case "events":
		return runEvents(rest[1:], stdout, stderr)
	case "workers":
		return runWorkers(rest[1:], stdout, stderr)
	case "approvals":
		return runApprovals(rest[1:], stdout, stderr)
	case "approve":
		return runApprove(rest[1:], stdout, stderr)
	case "deny":
		return runDeny(rest[1:], stdout, stderr)
	case "worker-stop":
		return runWorkerStop(rest[1:], stdout, stderr)
	case "report":
		return runReport(rest[1:], stdout, stderr)
	case "stop":
		return runStop(rest[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", rest[0])
		printUsage(stderr)
		return 2
	}
}

func runStart(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, planPath, runID string
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&planPath, "plan", "", "path to run plan json")
	fs.StringVar(&runID, "run", "", "optional run id override")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(planPath) == "" {
		fmt.Fprintln(stderr, "start requires --plan")
		return 2
	}
	manager := orchestrator.NewManager(sessionsDir)
	id, err := manager.Start(planPath, runID)
	if err != nil {
		fmt.Fprintf(stderr, "start failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "run started: %s\n", id)
	return 0
}

func runRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID, permissionModeRaw, workerCmd string
	var tick, startupTimeout, staleTimeout, softStallGrace, approvalTimeout, stopGrace time.Duration
	var maxAttempts int
	var disruptiveOptIn bool
	var workerArgs, workerEnv stringFlags
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.StringVar(&permissionModeRaw, "permissions", string(orchestrator.PermissionDefault), "permission mode: readonly|default|all")
	fs.StringVar(&workerCmd, "worker-cmd", "", "worker command to launch for each task")
	fs.Var(&workerArgs, "worker-arg", "worker argument (repeatable)")
	fs.Var(&workerEnv, "worker-env", "worker environment variable KEY=VALUE (repeatable)")
	fs.DurationVar(&tick, "tick", 250*time.Millisecond, "coordinator tick interval")
	fs.DurationVar(&startupTimeout, "startup-timeout", 30*time.Second, "startup SLA timeout")
	fs.DurationVar(&staleTimeout, "stale-timeout", 20*time.Second, "stale lease timeout")
	fs.DurationVar(&softStallGrace, "soft-stall-grace", 30*time.Second, "soft stall progress grace")
	fs.DurationVar(&approvalTimeout, "approval-timeout", 45*time.Minute, "approval wait timeout")
	fs.DurationVar(&stopGrace, "stop-grace", 2*time.Second, "worker stop grace period")
	fs.IntVar(&maxAttempts, "max-attempts", 2, "max retry attempts per task")
	fs.BoolVar(&disruptiveOptIn, "disruptive-opt-in", false, "allow disruptive actions to enter approval flow")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "run requires --run")
		return 2
	}
	if strings.TrimSpace(workerCmd) == "" {
		fmt.Fprintln(stderr, "run requires --worker-cmd")
		return 2
	}
	permissionMode := orchestrator.PermissionMode(strings.TrimSpace(permissionModeRaw))
	manager := orchestrator.NewManager(sessionsDir)
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		fmt.Fprintf(stderr, "run failed loading plan: %v\n", err)
		return 1
	}
	scheduler, err := orchestrator.NewScheduler(plan, plan.MaxParallelism)
	if err != nil {
		fmt.Fprintf(stderr, "run failed creating scheduler: %v\n", err)
		return 1
	}
	workers := orchestrator.NewWorkerManager(manager)
	broker, err := orchestrator.NewApprovalBroker(permissionMode, disruptiveOptIn, approvalTimeout)
	if err != nil {
		fmt.Fprintf(stderr, "run failed creating approval broker: %v\n", err)
		return 1
	}
	coord, err := orchestrator.NewCoordinator(runID, plan.Scope, manager, workers, scheduler, maxAttempts, startupTimeout, staleTimeout, softStallGrace, func(task orchestrator.TaskSpec, attempt int, workerID string) orchestrator.WorkerSpec {
		env := append([]string{}, os.Environ()...)
		env = append(env, workerEnv...)
		env = append(env,
			"BIRDHACKBOT_ORCH_SESSIONS_DIR="+sessionsDir,
			"BIRDHACKBOT_ORCH_RUN_ID="+runID,
			"BIRDHACKBOT_ORCH_TASK_ID="+task.TaskID,
			fmt.Sprintf("BIRDHACKBOT_ORCH_ATTEMPT=%d", attempt),
			"BIRDHACKBOT_ORCH_WORKER_ID="+workerID,
			"BIRDHACKBOT_ORCH_PERMISSION_MODE="+string(permissionMode),
			fmt.Sprintf("BIRDHACKBOT_ORCH_DISRUPTIVE_OPT_IN=%t", disruptiveOptIn),
		)
		return orchestrator.WorkerSpec{
			WorkerID: workerID,
			Command:  workerCmd,
			Args:     append([]string{}, workerArgs...),
			Env:      env,
		}
	}, broker)
	if err != nil {
		fmt.Fprintf(stderr, "run failed creating coordinator: %v\n", err)
		return 1
	}
	if err := coord.Reconcile(); err != nil {
		fmt.Fprintf(stderr, "run failed reconcile: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = coord.StopAll(stopGrace)
			_ = manager.Stop(runID)
			fmt.Fprintf(stdout, "run interrupted: %s\n", runID)
			return 0
		case <-ticker.C:
			status, err := manager.Status(runID)
			if err != nil {
				fmt.Fprintf(stderr, "run status failed: %v\n", err)
				return 1
			}
			if status.State == "stopped" {
				_ = coord.StopAll(stopGrace)
				fmt.Fprintf(stdout, "run stopped: %s\n", runID)
				return 0
			}
			if err := coord.Tick(); err != nil {
				fmt.Fprintf(stderr, "run tick failed: %v\n", err)
				return 1
			}
			if coord.Done() {
				if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunCompleted, map[string]any{
					"source": "run",
				}); err != nil {
					fmt.Fprintf(stderr, "run completion event failed: %v\n", err)
					return 1
				}
				fmt.Fprintf(stdout, "run completed: %s\n", runID)
				return 0
			}
		}
	}
}

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
	fmt.Fprintf(stdout, "active_workers: %d\n", status.ActiveWorkers)
	fmt.Fprintf(stdout, "queued_tasks: %d\n", status.QueuedTasks)
	fmt.Fprintf(stdout, "running_tasks: %d\n", status.RunningTasks)
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
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
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
	enc := json.NewEncoder(stdout)
	for _, req := range pending {
		if err := enc.Encode(req); err != nil {
			fmt.Fprintf(stderr, "approvals encode failed: %v\n", err)
			return 1
		}
	}
	return 0
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
	fmt.Fprintln(stderr, "commands: start, run, status, workers, events, approvals, approve, deny, worker-stop, report, stop")
}
