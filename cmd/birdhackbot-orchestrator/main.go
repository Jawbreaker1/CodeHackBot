package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

const version = "dev"
const plannerVersion = "planner_v1"

const (
	plannerModeStaticV1 = "goal_seed_v1"
	plannerModeLLMV1    = "goal_llm_v1"

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
	fs.Var(&scopeDenyTargets, "scope-deny-target", "explicit deny target hostname/ip (repeatable)")
	fs.Var(&constraints, "constraint", "run constraint (repeatable)")
	fs.Var(&successCriteria, "success-criterion", "success criterion (repeatable)")
	fs.Var(&stopCriteria, "stop-criterion", "stop criterion (repeatable)")
	fs.StringVar(&planReviewRaw, "plan-review", "approve", "goal plan decision: approve|reject|edit|regenerate")
	fs.StringVar(&planReviewRationale, "plan-review-rationale", "", "planner decision rationale for audit trail")
	fs.StringVar(&plannerModeRaw, "planner", "static", "planner mode for --goal runs: static|llm|auto")
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
	if runID == "" && goal == "" {
		fmt.Fprintln(stderr, "run requires --run or --goal")
		return 2
	}
	if strings.TrimSpace(workerCmd) == "" && !(goal != "" && (planReview == "reject" || planReview == "edit")) {
		fmt.Fprintln(stderr, "run requires --worker-cmd")
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
	if goal != "" {
		scope := orchestrator.Scope{
			Networks:    compactStringFlags(scopeNetworks),
			Targets:     compactStringFlags(scopeTargets),
			DenyTargets: compactStringFlags(scopeDenyTargets),
		}
		goalConstraints := compactStringFlags(constraints)
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
		promptHash := plannerPromptHash(goal, plannerMode, scope, goalConstraints, success, stop, maxParallelism)
		hypothesisLimit := 5
		plan, plannerBuildNote, err := buildGoalPlanFromMode(
			context.Background(),
			plannerMode,
			workerConfigPath,
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
		plan.Metadata.PlannerVersion = plannerVersion
		plan.Metadata.PlannerPromptHash = promptHash
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
			plan.Metadata.PlannerDecision = "reject"
			planPath, auditPath, err := persistPlanReview(resolvedSessionsDir, plan, "plan.review.json")
			if err != nil {
				fmt.Fprintf(stderr, "run failed writing rejected plan review: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "plan rejected: %s\nreview log: %s\n", planPath, auditPath)
			return 0
		case "edit":
			plan.Metadata.PlannerDecision = "edit"
			planPath, auditPath, err := persistPlanReview(resolvedSessionsDir, plan, "plan.draft.json")
			if err != nil {
				fmt.Fprintf(stderr, "run failed writing editable plan draft: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "plan draft written: %s\nedit and launch with: birdhackbot-orchestrator start --sessions-dir %s --plan %s\nreview log: %s\n", planPath, resolvedSessionsDir, planPath, auditPath)
			return 0
		}
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

func normalizeGoal(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}

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
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = coord.StopAll(stopGrace)
			if status, err := manager.Status(runID); err == nil && status.State != "completed" && status.State != "stopped" {
				_ = manager.Stop(runID)
			}
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
				if announce {
					fmt.Fprintf(stdout, "run stopped with failures: %s (%s)\n", runID, outcomeDetail)
				}
				return 1
			}
		}
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

func compactStringFlags(values stringFlags) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func validateGoalSeedInputs(scope orchestrator.Scope, constraints []string) error {
	if len(scope.Targets) == 0 && len(scope.Networks) == 0 {
		return fmt.Errorf("at least one --scope-target or --scope-network is required with --goal")
	}
	if len(constraints) == 0 {
		return fmt.Errorf("at least one --constraint is required with --goal")
	}
	return nil
}

func ensureRunIDAvailable(sessionsDir, runID string) error {
	runRoot := orchestrator.BuildRunPaths(sessionsDir, runID).Root
	_, err := os.Stat(runRoot)
	if err == nil {
		return fmt.Errorf("run id %q already exists", runID)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("check run path: %w", err)
	}
	return nil
}

func generateRunID(now time.Time) string {
	utc := now.UTC()
	return fmt.Sprintf("run-%s-%04x", utc.Format("20060102-150405"), utc.UnixNano()&0xffff)
}

func buildGoalPlanFromMode(
	ctx context.Context,
	plannerMode string,
	workerConfigPath string,
	runID, goal string,
	scope orchestrator.Scope,
	constraints, successCriteria, stopCriteria []string,
	maxParallelism, hypothesisLimit int,
	now time.Time,
) (orchestrator.RunPlan, string, error) {
	if plannerMode == "llm" || plannerMode == "auto" {
		client, model, llmCfgErr := resolvePlannerLLMClient(workerConfigPath)
		if llmCfgErr == nil && client != nil && strings.TrimSpace(model) != "" {
			plan, llmRationale, err := buildGoalLLMPlan(ctx, client, model, runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
			if err == nil {
				return plan, llmRationale, nil
			}
			if plannerMode == "llm" {
				return orchestrator.RunPlan{}, "", err
			}
			fallback, fallbackErr := buildGoalSeedPlan(runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
			if fallbackErr != nil {
				return orchestrator.RunPlan{}, "", fmt.Errorf("llm planner failed (%v); static fallback failed: %w", err, fallbackErr)
			}
			note := fmt.Sprintf("auto fallback to static planner: %v", err)
			return fallback, note, nil
		}
		if plannerMode == "llm" {
			return orchestrator.RunPlan{}, "", llmCfgErr
		}
		fallback, fallbackErr := buildGoalSeedPlan(runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
		if fallbackErr != nil {
			return orchestrator.RunPlan{}, "", fallbackErr
		}
		note := ""
		if llmCfgErr != nil {
			note = fmt.Sprintf("auto fallback to static planner: %v", llmCfgErr)
		}
		return fallback, note, nil
	}
	plan, err := buildGoalSeedPlan(runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
	return plan, "", err
}

func buildGoalLLMPlan(
	ctx context.Context,
	client llm.Client,
	model string,
	runID, goal string,
	scope orchestrator.Scope,
	constraints, successCriteria, stopCriteria []string,
	maxParallelism, hypothesisLimit int,
	now time.Time,
) (orchestrator.RunPlan, string, error) {
	normalizedGoal := normalizeGoal(goal)
	hypotheses := orchestrator.GenerateHypotheses(normalizedGoal, scope, hypothesisLimit)
	tasks, llmRationale, err := orchestrator.SynthesizeTaskGraphWithLLM(ctx, client, model, normalizedGoal, scope, constraints, hypotheses, maxParallelism)
	if err != nil {
		return orchestrator.RunPlan{}, "", err
	}
	if maxParallelism <= 0 {
		maxParallelism = 1
	}
	if len(successCriteria) == 0 {
		successCriteria = []string{"goal_seed_completed"}
	}
	if len(stopCriteria) == 0 {
		stopCriteria = []string{"manual_stop", "out_of_scope", "budget_exhausted"}
	}
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           scope,
		Constraints:     constraints,
		SuccessCriteria: successCriteria,
		StopCriteria:    stopCriteria,
		MaxParallelism:  maxParallelism,
		Tasks:           tasks,
		Metadata: orchestrator.PlanMetadata{
			CreatedAt:      now.UTC(),
			Goal:           strings.TrimSpace(goal),
			NormalizedGoal: normalizedGoal,
			PlannerMode:    plannerModeLLMV1,
			Hypotheses:     hypotheses,
		},
	}
	if err := orchestrator.ValidateSynthesizedPlan(plan); err != nil {
		return orchestrator.RunPlan{}, "", err
	}
	return plan, strings.TrimSpace(llmRationale), nil
}

func resolvePlannerLLMClient(workerConfigPath string) (llm.Client, string, error) {
	cfg, err := loadPlannerLLMConfig(workerConfigPath)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
		return nil, "", fmt.Errorf("llm planner requires %s or llm.base_url in config", plannerLLMBaseURLEnv)
	}
	model := strings.TrimSpace(cfg.LLM.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.Model)
	}
	if model == "" {
		return nil, "", fmt.Errorf("llm planner requires %s or llm.model in config", plannerLLMModelEnv)
	}
	return llm.NewLMStudioClient(cfg), model, nil
}

func loadPlannerLLMConfig(workerConfigPath string) (config.Config, error) {
	cfg := config.Config{}
	cfg.LLM.TimeoutSeconds = plannerDefaultLLMTimeout
	if strings.TrimSpace(workerConfigPath) != "" {
		loaded, _, err := config.Load(workerConfigPath, "", "")
		if err != nil {
			return config.Config{}, fmt.Errorf("load config from %s: %w", workerConfigPath, err)
		}
		cfg = loaded
		if cfg.LLM.TimeoutSeconds <= 0 {
			cfg.LLM.TimeoutSeconds = plannerDefaultLLMTimeout
		}
	}

	if v := strings.TrimSpace(os.Getenv(plannerLLMBaseURLEnv)); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMModelEnv)); v != "" {
		cfg.LLM.Model = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMAPIKeyEnv)); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMTimeoutEnv)); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return config.Config{}, fmt.Errorf("invalid %s value %q", plannerLLMTimeoutEnv, v)
		}
		cfg.LLM.TimeoutSeconds = parsed
	}
	return cfg, nil
}

func buildGoalSeedPlan(runID, goal string, scope orchestrator.Scope, constraints, successCriteria, stopCriteria []string, maxParallelism, hypothesisLimit int, now time.Time) (orchestrator.RunPlan, error) {
	normalizedGoal := normalizeGoal(goal)
	hypotheses := orchestrator.GenerateHypotheses(normalizedGoal, scope, hypothesisLimit)
	tasks, err := orchestrator.SynthesizeTaskGraph(normalizedGoal, scope, hypotheses)
	if err != nil {
		return orchestrator.RunPlan{}, err
	}
	if maxParallelism <= 0 {
		maxParallelism = 1
	}
	if len(successCriteria) == 0 {
		successCriteria = []string{"goal_seed_completed"}
	}
	if len(stopCriteria) == 0 {
		stopCriteria = []string{"manual_stop", "out_of_scope", "budget_exhausted"}
	}
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           scope,
		Constraints:     constraints,
		SuccessCriteria: successCriteria,
		StopCriteria:    stopCriteria,
		MaxParallelism:  maxParallelism,
		Tasks:           tasks,
		Metadata: orchestrator.PlanMetadata{
			CreatedAt:      now.UTC(),
			Goal:           strings.TrimSpace(goal),
			NormalizedGoal: normalizedGoal,
			PlannerMode:    plannerModeStaticV1,
			Hypotheses:     hypotheses,
		},
	}
	if err := orchestrator.ValidateSynthesizedPlan(plan); err != nil {
		return orchestrator.RunPlan{}, err
	}
	return plan, nil
}

func normalizePlanReview(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "approve":
		return "approve", nil
	case "reject", "edit", "regenerate":
		return strings.ToLower(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("must be one of: approve|reject|edit|regenerate")
	}
}

func mergePlannerRationale(reviewRationale, plannerNote string) string {
	parts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(reviewRationale); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(plannerNote); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, " | ")
}

func plannerPromptHash(goal, plannerMode string, scope orchestrator.Scope, constraints, successCriteria, stopCriteria []string, maxParallelism int) string {
	payload := map[string]any{
		"version":          plannerVersion,
		"planner_mode":     strings.TrimSpace(plannerMode),
		"goal":             normalizeGoal(goal),
		"scope":            scope,
		"constraints":      constraints,
		"success_criteria": successCriteria,
		"stop_criteria":    stopCriteria,
		"max_parallelism":  maxParallelism,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func printPlanSummary(stdout io.Writer, plan orchestrator.RunPlan) {
	fmt.Fprintf(stdout, "plan summary: run=%s planner=%s mode=%s prompt_hash=%s hypotheses=%d tasks=%d max_parallelism=%d\n", plan.RunID, plan.Metadata.PlannerVersion, plan.Metadata.PlannerMode, plan.Metadata.PlannerPromptHash, len(plan.Metadata.Hypotheses), len(plan.Tasks), plan.MaxParallelism)
	for i, hypothesis := range plan.Metadata.Hypotheses {
		if i >= 3 {
			fmt.Fprintf(stdout, "  ... %d more hypotheses\n", len(plan.Metadata.Hypotheses)-i)
			break
		}
		fmt.Fprintf(stdout, "  [%s] impact=%s confidence=%s score=%d: %s\n", hypothesis.ID, hypothesis.Impact, hypothesis.Confidence, hypothesis.Score, hypothesis.Statement)
	}
	for i, task := range plan.Tasks {
		if i >= 5 {
			fmt.Fprintf(stdout, "  ... %d more tasks\n", len(plan.Tasks)-i)
			break
		}
		fmt.Fprintf(stdout, "  task[%d] %s risk=%s depends_on=%d strategy=%s\n", i+1, task.TaskID, task.RiskLevel, len(task.DependsOn), task.Strategy)
	}
}

func persistPlanReview(sessionsDir string, plan orchestrator.RunPlan, planFilename string) (string, string, error) {
	paths, err := orchestrator.EnsureRunLayout(sessionsDir, plan.RunID)
	if err != nil {
		return "", "", err
	}
	planPath := filepath.Join(paths.PlanDir, planFilename)
	if err := orchestrator.WriteJSONAtomic(planPath, plan); err != nil {
		return "", "", err
	}
	reviewPath := filepath.Join(paths.PlanDir, "plan.review.audit.json")
	review := map[string]any{
		"run_id":              plan.RunID,
		"planner_version":     plan.Metadata.PlannerVersion,
		"planner_prompt_hash": plan.Metadata.PlannerPromptHash,
		"decision":            plan.Metadata.PlannerDecision,
		"rationale":           plan.Metadata.PlannerRationale,
		"regeneration_count":  plan.Metadata.RegenerationCount,
		"created_at":          plan.Metadata.CreatedAt,
		"goal":                plan.Metadata.Goal,
		"normalized_goal":     plan.Metadata.NormalizedGoal,
		"hypothesis_count":    len(plan.Metadata.Hypotheses),
		"task_count":          len(plan.Tasks),
	}
	if err := orchestrator.WriteJSONAtomic(reviewPath, review); err != nil {
		return "", "", err
	}
	return planPath, reviewPath, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func detectWorkerConfigPath() string {
	if existing := strings.TrimSpace(os.Getenv("BIRDHACKBOT_CONFIG_PATH")); existing != "" {
		if _, err := os.Stat(existing); err == nil {
			return existing
		}
	}
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return ""
	}
	candidate := filepath.Join(wd, "config", "default.json")
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
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
	fmt.Fprintln(stderr, "commands: start, run, status, workers, events, approvals, approve, deny, worker-stop, report, stop, tui")
}
