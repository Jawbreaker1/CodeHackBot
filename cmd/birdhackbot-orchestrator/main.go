package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

const version = "dev"
const plannerVersion = "planner_v1"

const (
	plannerModeStaticV1 = "goal_seed_v1"
	plannerModeLLMV1    = "goal_llm_v1"
	plannerModeTUIV1    = "interactive_tui_v1"

	plannerLLMBaseURLEnv     = "BIRDHACKBOT_LLM_BASE_URL"
	plannerLLMModelEnv       = "BIRDHACKBOT_LLM_MODEL"
	plannerLLMAPIKeyEnv      = "BIRDHACKBOT_LLM_API_KEY"
	plannerLLMTimeoutEnv     = "BIRDHACKBOT_LLM_TIMEOUT_SECONDS"
	plannerDefaultLLMTimeout = 120
)

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
	case "benchmark":
		return runBenchmark(rest[1:], stdout, stderr)
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
	case "approve-all":
		return runApproveAll(rest[1:], stdout, stderr)
	case "deny":
		return runDeny(rest[1:], stdout, stderr)
	case "worker-stop":
		return runWorkerStop(rest[1:], stdout, stderr)
	case "report":
		return runReport(rest[1:], stdout, stderr)
	case "stop":
		return runStop(rest[1:], stdout, stderr)
	case "tui":
		return runTUI(rest[1:], stdout, stderr)
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
	var sessionsDir, runID, goal, permissionModeRaw, workerCmd, planReviewRaw, planReviewRationale, plannerModeRaw string
	var tick, startupTimeout, staleTimeout, softStallGrace, approvalTimeout, stopGrace time.Duration
	var maxAttempts, maxParallelism, regenerateCount int
	var disruptiveOptIn bool
	var scopeLocal bool
	var useTUI bool
	var workerArgs, workerEnv, scopeTargets, scopeNetworks, scopeDenyTargets, constraints, successCriteria, stopCriteria stringFlags
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.StringVar(&goal, "goal", "", "goal text for seed planning (requires scope + constraints)")
	fs.StringVar(&permissionModeRaw, "permissions", string(orchestrator.PermissionDefault), "permission mode: readonly|default|all")
	fs.StringVar(&workerCmd, "worker-cmd", "", "worker command to launch for each task")
	fs.Var(&workerArgs, "worker-arg", "worker argument (repeatable)")
	fs.Var(&workerEnv, "worker-env", "worker environment variable KEY=VALUE (repeatable)")
	fs.Var(&scopeTargets, "scope-target", "in-scope target hostname/ip (repeatable)")
	fs.Var(&scopeNetworks, "scope-network", "in-scope network CIDR (repeatable)")
	fs.BoolVar(&scopeLocal, "scope-local", false, "local-only scope alias for --scope-target 127.0.0.1 and --scope-target localhost")
	fs.Var(&scopeDenyTargets, "scope-deny-target", "explicit deny target hostname/ip (repeatable)")
	fs.Var(&constraints, "constraint", "run constraint (repeatable)")
	fs.Var(&successCriteria, "success-criterion", "success criterion (repeatable)")
	fs.Var(&stopCriteria, "stop-criterion", "stop criterion (repeatable)")
	fs.StringVar(&planReviewRaw, "plan-review", "approve", "goal plan decision: approve|reject|edit|regenerate")
	fs.StringVar(&planReviewRationale, "plan-review-rationale", "", "planner decision rationale for audit trail")
	fs.StringVar(&plannerModeRaw, "planner", "auto", "planner mode for --goal runs: auto|llm|static (static must be explicitly selected)")
	fs.IntVar(&regenerateCount, "plan-regenerate-count", 1, "number of regeneration attempts when --plan-review=regenerate")
	fs.IntVar(&maxParallelism, "max-parallelism", 1, "max worker parallelism for goal-seeded runs")
	fs.DurationVar(&tick, "tick", 250*time.Millisecond, "coordinator tick interval")
	fs.DurationVar(&startupTimeout, "startup-timeout", 30*time.Second, "startup SLA timeout")
	fs.DurationVar(&staleTimeout, "stale-timeout", 20*time.Second, "stale lease timeout")
	fs.DurationVar(&softStallGrace, "soft-stall-grace", 30*time.Second, "soft stall progress grace")
	fs.DurationVar(&approvalTimeout, "approval-timeout", 45*time.Minute, "approval wait timeout")
	fs.DurationVar(&stopGrace, "stop-grace", 2*time.Second, "worker stop grace period")
	fs.IntVar(&maxAttempts, "max-attempts", 2, "max retry attempts per task")
	fs.BoolVar(&disruptiveOptIn, "disruptive-opt-in", false, "allow disruptive actions to enter approval flow")
	fs.BoolVar(&useTUI, "tui", false, "run coordinator with attached TUI in this terminal")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	runID = strings.TrimSpace(runID)
	goal = normalizeGoal(goal)
	planReview, err := normalizePlanReview(planReviewRaw)
	if err != nil {
		fmt.Fprintf(stderr, "run invalid --plan-review: %v\n", err)
		return 2
	}
	plannerMode, err := orchestrator.ParsePlannerMode(plannerModeRaw)
	if err != nil {
		fmt.Fprintf(stderr, "run invalid --planner: %v\n", err)
		return 2
	}
	if regenerateCount < 1 {
		fmt.Fprintln(stderr, "run invalid --plan-regenerate-count: must be >= 1")
		return 2
	}
	resolvedSessionsDir := strings.TrimSpace(sessionsDir)
	if resolvedSessionsDir == "" {
		resolvedSessionsDir = "sessions"
	}
	if !filepath.IsAbs(resolvedSessionsDir) {
		if abs, absErr := filepath.Abs(resolvedSessionsDir); absErr == nil {
			resolvedSessionsDir = abs
		}
	}
	resolvedWorkerCmd := strings.TrimSpace(workerCmd)
	if resolvedWorkerCmd != "" && !filepath.IsAbs(resolvedWorkerCmd) {
		if abs, absErr := filepath.Abs(resolvedWorkerCmd); absErr == nil {
			resolvedWorkerCmd = abs
		}
	}
	permissionMode := orchestrator.PermissionMode(strings.TrimSpace(permissionModeRaw))
	workerConfigPath := detectWorkerConfigPath()
	manager := orchestrator.NewManager(resolvedSessionsDir)
	scope := orchestrator.Scope{
		Networks:    compactStringFlags(scopeNetworks),
		Targets:     compactStringFlags(scopeTargets),
		DenyTargets: compactStringFlags(scopeDenyTargets),
	}
	if scopeLocal {
		scope.Targets = compactStrings(append(scope.Targets, "127.0.0.1", "localhost"))
	}
	goalConstraints := compactStringFlags(constraints)

	if goal == "" {
		if runID == "" {
			runID = generateRunID(time.Now().UTC())
		}
		runRoot := orchestrator.BuildRunPaths(resolvedSessionsDir, runID).Root
		if _, statErr := os.Stat(runRoot); os.IsNotExist(statErr) {
			plan := buildInteractivePlanningPlan(runID, time.Now().UTC(), scope, goalConstraints, maxParallelism)
			if _, err := manager.StartFromPlan(plan, ""); err != nil {
				fmt.Fprintf(stderr, "run failed creating planning run: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "run started in planning mode: %s\n", runID)
			if !useTUI {
				fmt.Fprintf(stdout, "open with: birdhackbot-orchestrator tui --sessions-dir %s --run %s\n", resolvedSessionsDir, runID)
				return 0
			}
		} else if statErr != nil {
			fmt.Fprintf(stderr, "run failed reading run path: %v\n", statErr)
			return 1
		}
	}

	if strings.TrimSpace(workerCmd) == "" && goal != "" && !(planReview == "reject" || planReview == "edit") {
		fmt.Fprintln(stderr, "run requires --worker-cmd")
		return 2
	}
	if goal != "" {
		if err := validateGoalSeedInputs(scope, goalConstraints); err != nil {
			fmt.Fprintf(stderr, "run invalid goal input: %v\n", err)
			return 2
		}
		if runID == "" {
			runID = generateRunID(time.Now().UTC())
		}
		if err := ensureRunIDAvailable(resolvedSessionsDir, runID); err != nil {
			fmt.Fprintf(stderr, "run invalid id: %v\n", err)
			return 2
		}

		success := compactStringFlags(successCriteria)
		stop := compactStringFlags(stopCriteria)
		hypothesisLimit := 5
		plan, plannerBuildNote, err := buildGoalPlanFromMode(
			context.Background(),
			plannerMode,
			workerConfigPath,
			resolvedSessionsDir,
			runID,
			goal,
			scope,
			goalConstraints,
			success,
			stop,
			maxParallelism,
			hypothesisLimit,
			time.Now().UTC(),
		)
		if err != nil {
			fmt.Fprintf(stderr, "run failed synthesizing plan: %v\n", err)
			return 1
		}
		if planReview == "regenerate" {
			for i := 0; i < regenerateCount; i++ {
				hypothesisLimit = minInt(8, 5+i+1)
				plan, plannerBuildNote, err = buildGoalPlanFromMode(
					context.Background(),
					plannerMode,
					workerConfigPath,
					resolvedSessionsDir,
					runID,
					goal,
					scope,
					goalConstraints,
					success,
					stop,
					maxParallelism,
					hypothesisLimit,
					time.Now().UTC(),
				)
				if err != nil {
					fmt.Fprintf(stderr, "run failed regenerating plan (attempt %d): %v\n", i+1, err)
					return 1
				}
			}
		}
		decision := planReview
		if planReview == "regenerate" {
			decision = "approve"
		}
		plan.Metadata.RunPhase = orchestrator.RunPhaseReview
		plan.Metadata.PlannerVersion = plannerVersion
		plan.Metadata.PlannerPromptHash = plannerPromptHash(
			goal,
			plan.Metadata.PlannerMode,
			scope,
			goalConstraints,
			success,
			stop,
			maxParallelism,
			plan.Metadata.PlannerPlaybooks,
		)
		plan.Metadata.PlannerDecision = decision
		plan.Metadata.PlannerRationale = mergePlannerRationale(planReviewRationale, plannerBuildNote)
		plan.Metadata.RegenerationCount = 0
		if planReview == "regenerate" {
			plan.Metadata.RegenerationCount = regenerateCount
		}

		printPlanSummary(stdout, plan)
		if strings.TrimSpace(plannerBuildNote) != "" {
			fmt.Fprintf(stdout, "plan note: %s\n", strings.TrimSpace(plannerBuildNote))
		}
		switch planReview {
		case "reject":
			plan.Metadata.RunPhase = orchestrator.RunPhaseReview
			plan.Metadata.PlannerDecision = "reject"
			planPath, auditPath, err := persistPlanReview(resolvedSessionsDir, plan, "plan.review.json")
			if err != nil {
				fmt.Fprintf(stderr, "run failed writing rejected plan review: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "plan rejected: %s\nreview log: %s\n", planPath, auditPath)
			return 0
		case "edit":
			plan.Metadata.RunPhase = orchestrator.RunPhaseReview
			plan.Metadata.PlannerDecision = "edit"
			planPath, auditPath, err := persistPlanReview(resolvedSessionsDir, plan, "plan.draft.json")
			if err != nil {
				fmt.Fprintf(stderr, "run failed writing editable plan draft: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "plan draft written: %s\nedit and launch with: birdhackbot-orchestrator start --sessions-dir %s --plan %s\nreview log: %s\n", planPath, resolvedSessionsDir, planPath, auditPath)
			return 0
		}
		plan.Metadata.RunPhase = orchestrator.RunPhaseApproved
		startedRunID, err := manager.StartFromPlan(plan, "")
		if err != nil {
			fmt.Fprintf(stderr, "run failed creating goal plan: %v\n", err)
			return 1
		}
		runID = startedRunID
		if _, _, err := persistPlanReview(resolvedSessionsDir, plan, "plan.json"); err != nil {
			fmt.Fprintf(stderr, "run warning: failed to write planner audit: %v\n", err)
		}
		fmt.Fprintf(stdout, "run started: %s\n", runID)
	}
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		fmt.Fprintf(stderr, "run failed loading plan: %v\n", err)
		return 1
	}
	runPhase := orchestrator.NormalizeRunPhase(plan.Metadata.RunPhase)
	if runPhase == "" {
		if len(plan.Tasks) == 0 {
			runPhase = orchestrator.RunPhasePlanning
		} else {
			runPhase = orchestrator.RunPhaseApproved
			if err := manager.SetRunPhase(runID, runPhase); err != nil {
				fmt.Fprintf(stderr, "run failed setting default phase: %v\n", err)
				return 1
			}
			plan.Metadata.RunPhase = runPhase
		}
	}
	if runPhase == orchestrator.RunPhasePlanning || runPhase == orchestrator.RunPhaseReview {
		if useTUI {
			tuiRefresh := tick
			if tuiRefresh < 500*time.Millisecond {
				tuiRefresh = 500 * time.Millisecond
			}
			tuiCode := runTUI([]string{
				"--sessions-dir", resolvedSessionsDir,
				"--run", runID,
				"--refresh", tuiRefresh.String(),
			}, stdout, stderr)
			if tuiCode != 0 {
				return tuiCode
			}
			plan, err = manager.LoadRunPlan(runID)
			if err != nil {
				fmt.Fprintf(stderr, "run failed loading plan after tui: %v\n", err)
				return 1
			}
			runPhase = orchestrator.NormalizeRunPhase(plan.Metadata.RunPhase)
			if runPhase != orchestrator.RunPhaseApproved {
				return 0
			}
		} else {
			fmt.Fprintf(stdout, "run %s is in %s phase; no workers launched\n", runID, runPhase)
			return 0
		}
	}
	if len(plan.Tasks) == 0 {
		fmt.Fprintf(stderr, "run %s has no executable tasks\n", runID)
		return 1
	}
	if strings.TrimSpace(resolvedWorkerCmd) == "" {
		fmt.Fprintln(stderr, "run requires --worker-cmd for execution")
		return 2
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
			"BIRDHACKBOT_ORCH_SESSIONS_DIR="+resolvedSessionsDir,
			"BIRDHACKBOT_ORCH_RUN_ID="+runID,
			"BIRDHACKBOT_ORCH_TASK_ID="+task.TaskID,
			fmt.Sprintf("BIRDHACKBOT_ORCH_ATTEMPT=%d", attempt),
			"BIRDHACKBOT_ORCH_WORKER_ID="+workerID,
			"BIRDHACKBOT_ORCH_PERMISSION_MODE="+string(permissionMode),
			fmt.Sprintf("BIRDHACKBOT_ORCH_DISRUPTIVE_OPT_IN=%t", disruptiveOptIn),
		)
		if workerConfigPath != "" {
			env = append(env, "BIRDHACKBOT_CONFIG_PATH="+workerConfigPath)
		}
		return orchestrator.WorkerSpec{
			WorkerID: workerID,
			Command:  resolvedWorkerCmd,
			Args:     append([]string{}, workerArgs...),
			Env:      env,
		}
	}, broker)
	if err != nil {
		fmt.Fprintf(stderr, "run failed creating coordinator: %v\n", err)
		return 1
	}
	coord.SetRunPhase(runPhase)
	if err := coord.Reconcile(); err != nil {
		fmt.Fprintf(stderr, "run failed reconcile: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if useTUI {
		runDone := make(chan int, 1)
		go func() {
			runDone <- executeCoordinatorLoop(ctx, manager, coord, runID, tick, stopGrace, stdout, stderr, false)
		}()

		tuiRefresh := tick
		if tuiRefresh < 500*time.Millisecond {
			tuiRefresh = 500 * time.Millisecond
		}
		tuiCode := runTUI([]string{
			"--sessions-dir", resolvedSessionsDir,
			"--run", runID,
			"--refresh", tuiRefresh.String(),
		}, stdout, stderr)

		select {
		case runCode := <-runDone:
			if runCode != 0 {
				return runCode
			}
			return tuiCode
		default:
		}

		if status, err := manager.Status(runID); err == nil && status.State != "completed" && status.State != "stopped" {
			_ = manager.Stop(runID)
		}
		stop()

		select {
		case runCode := <-runDone:
			if runCode != 0 {
				return runCode
			}
		case <-time.After(5 * time.Second):
			fmt.Fprintf(stderr, "run shutdown timeout: %s\n", runID)
			return 1
		}
		return tuiCode
	}

	return executeCoordinatorLoop(ctx, manager, coord, runID, tick, stopGrace, stdout, stderr, true)
}
