package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

const (
	defaultBenchmarkScenarioPackPath = "docs/runbooks/autonomy-benchmark-scenarios.json"
	defaultBenchmarkBaselineOutPath  = "docs/runbooks/autonomy-benchmark-baseline.json"
)

var benchmarkSlugPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

type benchmarkScenarioPack struct {
	Version   string              `json:"version"`
	Scenarios []benchmarkScenario `json:"scenarios"`
}

type benchmarkScenario struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Goal            string             `json:"goal"`
	Scope           orchestrator.Scope `json:"scope"`
	Constraints     []string           `json:"constraints"`
	SuccessCriteria []string           `json:"success_criteria,omitempty"`
	StopCriteria    []string           `json:"stop_criteria,omitempty"`
	Planner         string             `json:"planner,omitempty"`
	PermissionMode  string             `json:"permission_mode,omitempty"`
	MaxParallelism  int                `json:"max_parallelism,omitempty"`
	MaxAttempts     int                `json:"max_attempts,omitempty"`
	DisruptiveOptIn bool               `json:"disruptive_opt_in,omitempty"`
}

func (s *benchmarkScenario) UnmarshalJSON(data []byte) error {
	type rawScenario struct {
		ID              string             `json:"id"`
		Name            string             `json:"name"`
		Goal            string             `json:"goal"`
		Scope           orchestrator.Scope `json:"scope"`
		Constraints     []string           `json:"constraints"`
		SuccessCriteria []string           `json:"success_criteria,omitempty"`
		StopCriteria    []string           `json:"stop_criteria,omitempty"`
		Planner         string             `json:"planner,omitempty"`
		PermissionMode  string             `json:"permission_mode,omitempty"`
		MaxParallelism  int                `json:"max_parallelism,omitempty"`
		MaxAttempts     int                `json:"max_attempts,omitempty"`
		DisruptiveOptIn bool               `json:"disruptive_opt_in,omitempty"`
	}
	var raw rawScenario
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.ID = strings.TrimSpace(raw.ID)
	s.Name = strings.TrimSpace(raw.Name)
	s.Goal = normalizeGoal(raw.Goal)
	s.Scope = raw.Scope
	s.Scope.Networks = compactStrings(raw.Scope.Networks)
	s.Scope.Targets = compactStrings(raw.Scope.Targets)
	s.Scope.DenyTargets = compactStrings(raw.Scope.DenyTargets)
	s.Constraints = compactStrings(raw.Constraints)
	s.SuccessCriteria = compactStrings(raw.SuccessCriteria)
	s.StopCriteria = compactStrings(raw.StopCriteria)
	s.Planner = strings.TrimSpace(raw.Planner)
	s.PermissionMode = strings.TrimSpace(raw.PermissionMode)
	s.MaxParallelism = raw.MaxParallelism
	s.MaxAttempts = raw.MaxAttempts
	s.DisruptiveOptIn = raw.DisruptiveOptIn
	return nil
}

type benchmarkRunMetrics struct {
	TotalTasks                          int            `json:"total_tasks"`
	CompletedTasks                      int            `json:"completed_tasks"`
	FailedTasks                         int            `json:"failed_tasks"`
	BlockedTasks                        int            `json:"blocked_tasks"`
	CanceledTasks                       int            `json:"canceled_tasks"`
	TaskSuccessRate                     float64        `json:"task_success_rate"`
	TotalFailures                       int            `json:"total_failures"`
	RecoveredFailures                   int            `json:"recovered_failures"`
	RecoverySuccessRate                 float64        `json:"recovery_success_rate"`
	LoopIncidents                       int            `json:"loop_incidents"`
	LoopIncidentRate                    float64        `json:"loop_incident_rate"`
	TotalFindings                       int            `json:"total_findings"`
	VerifiedFindings                    int            `json:"verified_findings"`
	UnverifiedFindings                  int            `json:"unverified_findings"`
	VerifiedFindingPrecision            float64        `json:"verified_finding_precision"`
	TimeToFirstVerifiedFindingSeconds   float64        `json:"time_to_first_verified_finding_seconds"`
	TimeToFirstVerifiedFindingAvailable bool           `json:"time_to_first_verified_finding_available"`
	FailureReasonCounts                 map[string]int `json:"failure_reason_counts,omitempty"`
	TerminalReason                      string         `json:"terminal_reason,omitempty"`
}

type benchmarkRunScorecard struct {
	BenchmarkID   string              `json:"benchmark_id"`
	PackVersion   string              `json:"pack_version"`
	ScenarioID    string              `json:"scenario_id"`
	ScenarioName  string              `json:"scenario_name"`
	Iteration     int                 `json:"iteration"`
	RunID         string              `json:"run_id"`
	Seed          int64               `json:"seed"`
	StartedAt     time.Time           `json:"started_at"`
	FinishedAt    time.Time           `json:"finished_at"`
	DurationSec   float64             `json:"duration_seconds"`
	ExitCode      int                 `json:"exit_code"`
	Succeeded     bool                `json:"succeeded"`
	Error         string              `json:"error,omitempty"`
	StdoutLogPath string              `json:"stdout_log_path"`
	StderrLogPath string              `json:"stderr_log_path"`
	Metrics       benchmarkRunMetrics `json:"metrics"`
}

type benchmarkDistribution struct {
	Count  int     `json:"count"`
	Median float64 `json:"median"`
	P90    float64 `json:"p90"`
}

type benchmarkScenarioAggregate struct {
	Runs                          int                   `json:"runs"`
	SucceededRuns                 int                   `json:"succeeded_runs"`
	FailedRuns                    int                   `json:"failed_runs"`
	TaskSuccessRate               benchmarkDistribution `json:"task_success_rate"`
	VerifiedFindingPrecision      benchmarkDistribution `json:"verified_finding_precision"`
	LoopIncidentRate              benchmarkDistribution `json:"loop_incident_rate"`
	RecoverySuccessRate           benchmarkDistribution `json:"recovery_success_rate"`
	TimeToFirstVerifiedFindingSec benchmarkDistribution `json:"time_to_first_verified_finding_seconds"`
	DurationSec                   benchmarkDistribution `json:"duration_seconds"`
	FailureReasonCounts           map[string]int        `json:"failure_reason_counts,omitempty"`
	TerminalReasonCounts          map[string]int        `json:"terminal_reason_counts,omitempty"`
}

type benchmarkScenarioSummary struct {
	Scenario  benchmarkScenario          `json:"scenario"`
	Runs      []benchmarkRunScorecard    `json:"runs"`
	Aggregate benchmarkScenarioAggregate `json:"aggregate"`
}

type benchmarkSummary struct {
	BenchmarkID  string                     `json:"benchmark_id"`
	PackVersion  string                     `json:"pack_version"`
	GeneratedAt  time.Time                  `json:"generated_at"`
	SessionsDir  string                     `json:"sessions_dir"`
	OutputDir    string                     `json:"output_dir"`
	ScenarioPack string                     `json:"scenario_pack"`
	Seed         int64                      `json:"seed"`
	Repeat       int                        `json:"repeat"`
	Shuffle      bool                       `json:"shuffle"`
	Scenarios    []benchmarkScenarioSummary `json:"scenarios"`
	Aggregate    benchmarkScenarioAggregate `json:"aggregate"`
}

type benchmarkBaseline struct {
	Version     string                      `json:"version"`
	GeneratedAt time.Time                   `json:"generated_at"`
	BenchmarkID string                      `json:"benchmark_id"`
	PackVersion string                      `json:"pack_version"`
	Seed        int64                       `json:"seed"`
	Repeat      int                         `json:"repeat"`
	Scenarios   []benchmarkBaselineScenario `json:"scenarios"`
	Aggregate   benchmarkScenarioAggregate  `json:"aggregate"`
}

type benchmarkBaselineScenario struct {
	ScenarioID string                     `json:"scenario_id"`
	Name       string                     `json:"name"`
	Aggregate  benchmarkScenarioAggregate `json:"aggregate"`
}

func runBenchmark(args []string, stdout, stderr io.Writer) int {
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	return runBenchmarkWith(args, stdout, stderr, signalCtx, runRun)
}

func runBenchmarkWith(args []string, stdout, stderr io.Writer, signalCtx context.Context, runExecutor func(args []string, stdout, stderr io.Writer) int) int {
	if signalCtx == nil {
		signalCtx = context.Background()
	}
	if runExecutor == nil {
		runExecutor = runRun
	}
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		sessionsDir     string
		scenarioPack    string
		outputDir       string
		benchmarkID     string
		baselineOut     string
		workerCmd       string
		plannerMode     string
		permissionMode  string
		repeat          int
		maxParallelism  int
		maxAttempts     int
		seed            int64
		shuffle         bool
		lockBaseline    bool
		continueOnErr   bool
		disruptiveOptIn bool
		tick            time.Duration
		startupTimeout  time.Duration
		staleTimeout    time.Duration
		softStallGrace  time.Duration
		approvalTimeout time.Duration
		stopGrace       time.Duration
		scenarioFilter  stringFlags
		workerArgs      stringFlags
		workerEnv       stringFlags
	)
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&scenarioPack, "scenario-pack", defaultBenchmarkScenarioPackPath, "path to benchmark scenario pack json")
	fs.StringVar(&outputDir, "out-dir", "", "benchmark output directory (default: <sessions-dir>/benchmarks)")
	fs.StringVar(&benchmarkID, "benchmark-id", "", "benchmark run id (default: benchmark-<timestamp>)")
	fs.StringVar(&baselineOut, "baseline-out", defaultBenchmarkBaselineOutPath, "baseline lock output path")
	fs.StringVar(&workerCmd, "worker-cmd", "", "worker command to launch for each task")
	fs.Var(&workerArgs, "worker-arg", "worker argument (repeatable)")
	fs.Var(&workerEnv, "worker-env", "worker environment variable KEY=VALUE (repeatable)")
	fs.StringVar(&plannerMode, "planner", "static", "default planner mode for scenarios without override: static|llm|auto")
	fs.StringVar(&permissionMode, "permissions", string(orchestrator.PermissionDefault), "default permission mode for scenarios without override: readonly|default|all")
	fs.Var(&scenarioFilter, "scenario", "scenario id filter (repeatable)")
	fs.IntVar(&repeat, "repeat", 5, "number of runs per scenario")
	fs.IntVar(&maxParallelism, "max-parallelism", 1, "default max parallelism for scenarios without override")
	fs.IntVar(&maxAttempts, "max-attempts", 2, "default max attempts for scenarios without override")
	fs.Int64Var(&seed, "seed", 42, "seed for deterministic scenario ordering")
	fs.BoolVar(&shuffle, "shuffle", false, "shuffle scenario execution order using --seed")
	fs.BoolVar(&lockBaseline, "lock-baseline", false, "write baseline file with median/p90 aggregates")
	fs.BoolVar(&continueOnErr, "continue-on-error", true, "continue running scenarios when one run fails")
	fs.BoolVar(&disruptiveOptIn, "disruptive-opt-in", false, "allow disruptive actions to enter approval flow (global default)")
	fs.DurationVar(&tick, "tick", 250*time.Millisecond, "coordinator tick interval")
	fs.DurationVar(&startupTimeout, "startup-timeout", 30*time.Second, "startup SLA timeout")
	fs.DurationVar(&staleTimeout, "stale-timeout", 20*time.Second, "stale lease timeout")
	fs.DurationVar(&softStallGrace, "soft-stall-grace", 30*time.Second, "soft stall progress grace")
	fs.DurationVar(&approvalTimeout, "approval-timeout", 45*time.Minute, "approval wait timeout")
	fs.DurationVar(&stopGrace, "stop-grace", 2*time.Second, "worker stop grace period")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if repeat <= 0 {
		fmt.Fprintln(stderr, "benchmark requires --repeat >= 1")
		return 2
	}
	if strings.TrimSpace(workerCmd) == "" {
		fmt.Fprintln(stderr, "benchmark requires --worker-cmd")
		return 2
	}
	if _, err := orchestrator.ParsePlannerMode(plannerMode); err != nil {
		fmt.Fprintf(stderr, "benchmark invalid --planner: %v\n", err)
		return 2
	}
	if err := validatePermissionMode(permissionMode); err != nil {
		fmt.Fprintf(stderr, "benchmark invalid --permissions: %v\n", err)
		return 2
	}
	if tick <= 0 || startupTimeout <= 0 || staleTimeout <= 0 || softStallGrace <= 0 || approvalTimeout <= 0 || stopGrace <= 0 {
		fmt.Fprintln(stderr, "benchmark timeout/tick values must be > 0")
		return 2
	}
	scenarioPack = strings.TrimSpace(scenarioPack)
	if scenarioPack == "" {
		scenarioPack = defaultBenchmarkScenarioPackPath
	}
	pack, err := loadBenchmarkScenarioPack(scenarioPack)
	if err != nil {
		fmt.Fprintf(stderr, "benchmark failed loading scenario pack: %v\n", err)
		return 1
	}
	selected, err := selectBenchmarkScenarios(pack.Scenarios, compactStringFlags(scenarioFilter))
	if err != nil {
		fmt.Fprintf(stderr, "benchmark scenario selection failed: %v\n", err)
		return 2
	}
	if len(selected) == 0 {
		fmt.Fprintln(stderr, "benchmark has no scenarios to run")
		return 2
	}
	if shuffle {
		r := rand.New(rand.NewSource(seed))
		r.Shuffle(len(selected), func(i, j int) {
			selected[i], selected[j] = selected[j], selected[i]
		})
	}
	sessionsDir = benchmarkResolvePath(sessionsDir)
	if benchmarkID == "" {
		benchmarkID = "benchmark-" + time.Now().UTC().Format("20060102-150405")
	}
	benchmarkID = benchmarkSlug(benchmarkID)
	if benchmarkID == "" {
		fmt.Fprintln(stderr, "benchmark id became empty after normalization")
		return 2
	}
	if outputDir == "" {
		outputDir = filepath.Join(sessionsDir, "benchmarks")
	}
	outputDir = benchmarkResolvePath(outputDir)
	benchmarkRoot := filepath.Join(outputDir, benchmarkID)
	if err := os.MkdirAll(benchmarkRoot, 0o755); err != nil {
		fmt.Fprintf(stderr, "benchmark failed creating output dir: %v\n", err)
		return 1
	}

	runStarted := time.Now().UTC()
	summary := benchmarkSummary{
		BenchmarkID:  benchmarkID,
		PackVersion:  pack.Version,
		GeneratedAt:  runStarted,
		SessionsDir:  sessionsDir,
		OutputDir:    benchmarkRoot,
		ScenarioPack: scenarioPack,
		Seed:         seed,
		Repeat:       repeat,
		Shuffle:      shuffle,
		Scenarios:    make([]benchmarkScenarioSummary, 0, len(selected)),
	}

	fmt.Fprintf(stdout, "benchmark started: id=%s scenarios=%d repeat=%d seed=%d\n", benchmarkID, len(selected), repeat, seed)

	anyFailures := false
	interrupted := false
scenarioLoop:
	for scenarioIndex, scenario := range selected {
		if signalCtx.Err() != nil {
			interrupted = true
			break
		}
		scenarioRuns := make([]benchmarkRunScorecard, 0, repeat)
		scenarioDir := filepath.Join(benchmarkRoot, benchmarkSlug(scenario.ID))
		if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
			fmt.Fprintf(stderr, "benchmark failed creating scenario dir (%s): %v\n", scenario.ID, err)
			return 1
		}
		for iteration := 1; iteration <= repeat; iteration++ {
			if signalCtx.Err() != nil {
				interrupted = true
				break
			}
			runSeed := seed + int64(scenarioIndex*1000) + int64(iteration)
			runID := benchmarkRunID(benchmarkID, scenario.ID, iteration)
			uniqueRunID, err := ensureBenchmarkRunIDAvailable(sessionsDir, runID)
			if err != nil {
				fmt.Fprintf(stderr, "benchmark failed preparing run id: %v\n", err)
				return 1
			}
			runID = uniqueRunID
			runDir := filepath.Join(scenarioDir, fmt.Sprintf("run-%02d", iteration))
			if err := os.MkdirAll(runDir, 0o755); err != nil {
				fmt.Fprintf(stderr, "benchmark failed creating run output dir: %v\n", err)
				return 1
			}

			runStdoutPath := filepath.Join(runDir, "run.stdout.log")
			runStderrPath := filepath.Join(runDir, "run.stderr.log")
			scorecardPath := filepath.Join(runDir, "scorecard.json")
			startedAt := time.Now().UTC()
			runArgs := benchmarkScenarioRunArgs(
				sessionsDir,
				runID,
				scenario,
				plannerMode,
				permissionMode,
				maxParallelism,
				maxAttempts,
				disruptiveOptIn,
				tick,
				startupTimeout,
				staleTimeout,
				softStallGrace,
				approvalTimeout,
				stopGrace,
				strings.TrimSpace(workerCmd),
				workerArgs,
				append(compactStringFlags(workerEnv),
					fmt.Sprintf("BIRDHACKBOT_BENCHMARK_ID=%s", benchmarkID),
					fmt.Sprintf("BIRDHACKBOT_BENCHMARK_SCENARIO=%s", scenario.ID),
					fmt.Sprintf("BIRDHACKBOT_BENCHMARK_RUN=%d", iteration),
					fmt.Sprintf("BIRDHACKBOT_BENCHMARK_SEED=%d", runSeed),
				),
			)
			var runStdout bytes.Buffer
			var runStderr bytes.Buffer
			autoApproveCtx, stopAutoApprove := context.WithCancel(signalCtx)
			waitAutoApprove := startBenchmarkAutoApproveLoop(autoApproveCtx, sessionsDir, runID, stderr)
			exitCode := runExecutor(runArgs, &runStdout, &runStderr)
			stopAutoApprove()
			waitAutoApprove()
			finishedAt := time.Now().UTC()
			_ = os.WriteFile(runStdoutPath, runStdout.Bytes(), 0o644)
			_ = os.WriteFile(runStderrPath, runStderr.Bytes(), 0o644)

			scorecard := benchmarkRunScorecard{
				BenchmarkID:   benchmarkID,
				PackVersion:   pack.Version,
				ScenarioID:    scenario.ID,
				ScenarioName:  scenario.Name,
				Iteration:     iteration,
				RunID:         runID,
				Seed:          runSeed,
				StartedAt:     startedAt,
				FinishedAt:    finishedAt,
				DurationSec:   finishedAt.Sub(startedAt).Seconds(),
				ExitCode:      exitCode,
				Succeeded:     exitCode == 0,
				StdoutLogPath: filepath.ToSlash(runStdoutPath),
				StderrLogPath: filepath.ToSlash(runStderrPath),
			}
			metrics, metricsErr := collectBenchmarkRunMetrics(sessionsDir, runID)
			if metricsErr != nil {
				scorecard.Error = metricsErr.Error()
			}
			scorecard.Metrics = metrics
			if !scorecard.Succeeded && scorecard.Error == "" {
				errText := strings.TrimSpace(runStderr.String())
				if errText == "" {
					errText = strings.TrimSpace(runStdout.String())
				}
				scorecard.Error = truncateForScorecard(errText, 600)
			}
			if signalCtx.Err() != nil {
				interrupted = true
				scorecard.Succeeded = false
				if scorecard.ExitCode == 0 {
					scorecard.ExitCode = 130
				}
				if strings.TrimSpace(scorecard.Error) == "" {
					scorecard.Error = "benchmark interrupted by signal"
				}
			}
			if writeErr := writeBenchmarkJSON(scorecardPath, scorecard); writeErr != nil {
				fmt.Fprintf(stderr, "benchmark failed writing scorecard: %v\n", writeErr)
				return 1
			}
			scenarioRuns = append(scenarioRuns, scorecard)

			status := "ok"
			if !scorecard.Succeeded {
				status = "failed"
				anyFailures = true
				if interrupted {
					status = "interrupted"
				}
			}
			fmt.Fprintf(stdout, "benchmark run: scenario=%s iter=%d/%d run=%s status=%s scorecard=%s\n", scenario.ID, iteration, repeat, runID, status, filepath.ToSlash(scorecardPath))
			if interrupted {
				break
			}
			if !scorecard.Succeeded && !continueOnErr {
				fmt.Fprintln(stderr, "benchmark stopping on first failure (--continue-on-error=false)")
				break
			}
		}
		scenarioSummary := benchmarkScenarioSummary{
			Scenario:  scenario,
			Runs:      scenarioRuns,
			Aggregate: aggregateBenchmarkRuns(scenarioRuns),
		}
		summary.Scenarios = append(summary.Scenarios, scenarioSummary)
		if interrupted {
			break scenarioLoop
		}
		if len(scenarioRuns) < repeat && !continueOnErr {
			break
		}
	}

	allRuns := make([]benchmarkRunScorecard, 0, len(summary.Scenarios)*repeat)
	for _, scenario := range summary.Scenarios {
		allRuns = append(allRuns, scenario.Runs...)
	}
	summary.GeneratedAt = time.Now().UTC()
	summary.Aggregate = aggregateBenchmarkRuns(allRuns)

	summaryPath := filepath.Join(benchmarkRoot, "summary.json")
	if err := writeBenchmarkJSON(summaryPath, summary); err != nil {
		fmt.Fprintf(stderr, "benchmark failed writing summary: %v\n", err)
		return 1
	}
	markdownPath := filepath.Join(benchmarkRoot, "summary.md")
	if err := os.WriteFile(markdownPath, []byte(renderBenchmarkSummaryMarkdown(summary)), 0o644); err != nil {
		fmt.Fprintf(stderr, "benchmark failed writing markdown summary: %v\n", err)
		return 1
	}

	if lockBaseline && !interrupted {
		baseline := benchmarkBaseline{
			Version:     "benchmark_baseline_v1",
			GeneratedAt: summary.GeneratedAt,
			BenchmarkID: summary.BenchmarkID,
			PackVersion: summary.PackVersion,
			Seed:        summary.Seed,
			Repeat:      summary.Repeat,
			Scenarios:   make([]benchmarkBaselineScenario, 0, len(summary.Scenarios)),
			Aggregate:   summary.Aggregate,
		}
		for _, scenario := range summary.Scenarios {
			baseline.Scenarios = append(baseline.Scenarios, benchmarkBaselineScenario{
				ScenarioID: scenario.Scenario.ID,
				Name:       scenario.Scenario.Name,
				Aggregate:  scenario.Aggregate,
			})
		}
		baselineOut = benchmarkResolvePath(baselineOut)
		if err := writeBenchmarkJSON(baselineOut, baseline); err != nil {
			fmt.Fprintf(stderr, "benchmark failed writing baseline: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "baseline locked: %s\n", filepath.ToSlash(baselineOut))
	} else if lockBaseline && interrupted {
		fmt.Fprintln(stderr, "benchmark baseline not locked: run interrupted")
	}

	fmt.Fprintf(stdout, "benchmark complete: summary=%s markdown=%s\n", filepath.ToSlash(summaryPath), filepath.ToSlash(markdownPath))
	if interrupted {
		fmt.Fprintln(stderr, "benchmark interrupted; partial summary written")
		return 130
	}
	if anyFailures {
		return 1
	}
	return 0
}

func startBenchmarkAutoApproveLoop(ctx context.Context, sessionsDir, runID string, stderr io.Writer) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager := orchestrator.NewManager(sessionsDir)
		approved := map[string]struct{}{}
		approvePending := func() {
			pending, err := manager.PendingApprovals(runID)
			if err != nil {
				return
			}
			for _, req := range pending {
				if _, seen := approved[req.ApprovalID]; seen {
					continue
				}
				if err := manager.SubmitApprovalDecision(
					runID,
					req.ApprovalID,
					true,
					string(orchestrator.ApprovalScopeSession),
					"codex",
					"benchmark auto-approve",
					0,
				); err != nil {
					if stderr != nil {
						fmt.Fprintf(stderr, "benchmark auto-approve failed: run=%s approval=%s err=%v\n", runID, req.ApprovalID, err)
					}
					continue
				}
				approved[req.ApprovalID] = struct{}{}
			}
		}

		approvePending()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				approvePending()
			}
		}
	}()
	return func() {
		<-done
	}
}

func benchmarkScenarioRunArgs(
	sessionsDir string,
	runID string,
	scenario benchmarkScenario,
	defaultPlanner string,
	defaultPermissions string,
	defaultMaxParallelism int,
	defaultMaxAttempts int,
	defaultDisruptiveOptIn bool,
	tick time.Duration,
	startupTimeout time.Duration,
	staleTimeout time.Duration,
	softStallGrace time.Duration,
	approvalTimeout time.Duration,
	stopGrace time.Duration,
	workerCmd string,
	workerArgs []string,
	workerEnv []string,
) []string {
	planner := strings.TrimSpace(scenario.Planner)
	if planner == "" {
		planner = defaultPlanner
	}
	permissions := strings.TrimSpace(scenario.PermissionMode)
	if permissions == "" {
		permissions = defaultPermissions
	}
	maxParallelism := scenario.MaxParallelism
	if maxParallelism <= 0 {
		maxParallelism = defaultMaxParallelism
		if maxParallelism <= 0 {
			maxParallelism = 1
		}
	}
	maxAttempts := scenario.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 1
		}
	}
	disruptiveOptIn := scenario.DisruptiveOptIn || defaultDisruptiveOptIn
	args := []string{
		"--sessions-dir", sessionsDir,
		"--run", runID,
		"--goal", scenario.Goal,
		"--planner", planner,
		"--permissions", permissions,
		"--worker-cmd", workerCmd,
		"--plan-review", "approve",
		"--max-parallelism", strconv.Itoa(maxParallelism),
		"--max-attempts", strconv.Itoa(maxAttempts),
		"--tick", tick.String(),
		"--startup-timeout", startupTimeout.String(),
		"--stale-timeout", staleTimeout.String(),
		"--soft-stall-grace", softStallGrace.String(),
		"--approval-timeout", approvalTimeout.String(),
		"--stop-grace", stopGrace.String(),
	}
	if disruptiveOptIn {
		args = append(args, "--disruptive-opt-in")
	}
	for _, target := range scenario.Scope.Targets {
		args = append(args, "--scope-target", target)
	}
	for _, network := range scenario.Scope.Networks {
		args = append(args, "--scope-network", network)
	}
	for _, deny := range scenario.Scope.DenyTargets {
		args = append(args, "--scope-deny-target", deny)
	}
	for _, constraint := range scenario.Constraints {
		args = append(args, "--constraint", constraint)
	}
	for _, criterion := range scenario.SuccessCriteria {
		args = append(args, "--success-criterion", criterion)
	}
	for _, criterion := range scenario.StopCriteria {
		args = append(args, "--stop-criterion", criterion)
	}
	for _, arg := range workerArgs {
		args = append(args, "--worker-arg", arg)
	}
	for _, env := range workerEnv {
		args = append(args, "--worker-env", env)
	}
	return args
}

func collectBenchmarkRunMetrics(sessionsDir, runID string) (benchmarkRunMetrics, error) {
	manager := orchestrator.NewManager(sessionsDir)
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		return benchmarkRunMetrics{}, err
	}
	leases, err := manager.ReadLeases(runID)
	if err != nil {
		return benchmarkRunMetrics{}, err
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		return benchmarkRunMetrics{}, err
	}
	metrics := benchmarkRunMetrics{
		FailureReasonCounts: map[string]int{},
	}
	finalStateByTask := map[string]string{}
	for _, task := range plan.Tasks {
		finalStateByTask[task.TaskID] = orchestrator.LeaseStatusQueued
	}
	for _, lease := range leases {
		taskID := strings.TrimSpace(lease.TaskID)
		if taskID == "" {
			continue
		}
		finalStateByTask[taskID] = strings.TrimSpace(lease.Status)
	}
	metrics.TotalTasks = len(plan.Tasks)
	for _, state := range finalStateByTask {
		switch state {
		case orchestrator.LeaseStatusCompleted:
			metrics.CompletedTasks++
		case orchestrator.LeaseStatusFailed:
			metrics.FailedTasks++
		case orchestrator.LeaseStatusBlocked:
			metrics.BlockedTasks++
		case orchestrator.LeaseStatusCanceled:
			metrics.CanceledTasks++
		}
	}
	if metrics.TotalTasks > 0 {
		metrics.TaskSuccessRate = float64(metrics.CompletedTasks) / float64(metrics.TotalTasks)
	}

	failedTaskIDs := map[string]struct{}{}
	loopTaskIDs := map[string]struct{}{}
	var runStartTS time.Time
	var firstFindingTS time.Time
	var terminalEvent *orchestrator.EventEnvelope
	for _, event := range events {
		switch event.Type {
		case orchestrator.EventTypeRunStarted:
			if runStartTS.IsZero() || event.TS.Before(runStartTS) {
				runStartTS = event.TS
			}
		case orchestrator.EventTypeRunCompleted, orchestrator.EventTypeRunStopped:
			if terminalEvent == nil || event.TS.After(terminalEvent.TS) || (event.TS.Equal(terminalEvent.TS) && event.Seq > terminalEvent.Seq) {
				candidate := event
				terminalEvent = &candidate
			}
		case orchestrator.EventTypeTaskFinding:
			if firstFindingTS.IsZero() || event.TS.Before(firstFindingTS) {
				firstFindingTS = event.TS
			}
		case orchestrator.EventTypeTaskFailed:
			taskID := strings.TrimSpace(event.TaskID)
			if taskID != "" {
				failedTaskIDs[taskID] = struct{}{}
			}
			payload := map[string]any{}
			if len(event.Payload) > 0 {
				_ = json.Unmarshal(event.Payload, &payload)
			}
			reason := strings.TrimSpace(toString(payload["reason"]))
			if reason == "" {
				reason = "unknown"
			}
			metrics.FailureReasonCounts[reason]++
			if reason == "assist_loop_detected" && taskID != "" {
				loopTaskIDs[taskID] = struct{}{}
			}
		}
	}
	if len(metrics.FailureReasonCounts) == 0 {
		metrics.FailureReasonCounts = nil
	}
	metrics.TerminalReason = benchmarkTerminalReason(terminalEvent)
	metrics.TotalFailures = len(failedTaskIDs)
	for taskID := range failedTaskIDs {
		if finalStateByTask[taskID] == orchestrator.LeaseStatusCompleted {
			metrics.RecoveredFailures++
		}
	}
	if metrics.TotalFailures == 0 {
		metrics.RecoverySuccessRate = 1.0
	} else {
		metrics.RecoverySuccessRate = float64(metrics.RecoveredFailures) / float64(metrics.TotalFailures)
	}
	metrics.LoopIncidents = len(loopTaskIDs)
	if metrics.TotalTasks > 0 {
		metrics.LoopIncidentRate = float64(metrics.LoopIncidents) / float64(metrics.TotalTasks)
	}

	findings, findErr := manager.ListFindings(runID)
	if findErr != nil {
		return metrics, findErr
	}
	metrics.TotalFindings = len(findings)
	reportPath, reportReady, reportErr := manager.ResolveRunReportPath(runID)
	if reportErr == nil && reportReady {
		if total, verified, unverified, ok, parseErr := parseReportFindingCounts(reportPath); parseErr == nil && ok {
			metrics.TotalFindings = total
			metrics.VerifiedFindings = verified
			metrics.UnverifiedFindings = unverified
		}
	}
	if metrics.VerifiedFindings == 0 && metrics.UnverifiedFindings == 0 && metrics.TotalFindings > 0 {
		estimatedVerified := 0
		for _, finding := range findings {
			if len(finding.Evidence) > 0 {
				estimatedVerified++
			}
		}
		metrics.VerifiedFindings = estimatedVerified
		metrics.UnverifiedFindings = metrics.TotalFindings - estimatedVerified
	}
	if metrics.TotalFindings > 0 {
		metrics.VerifiedFindingPrecision = float64(metrics.VerifiedFindings) / float64(metrics.TotalFindings)
	}
	if metrics.VerifiedFindings > 0 && !firstFindingTS.IsZero() {
		metrics.TimeToFirstVerifiedFindingAvailable = true
		if runStartTS.IsZero() || !firstFindingTS.After(runStartTS) {
			metrics.TimeToFirstVerifiedFindingSeconds = 0
		} else {
			metrics.TimeToFirstVerifiedFindingSeconds = firstFindingTS.Sub(runStartTS).Seconds()
		}
	}

	return metrics, nil
}

func benchmarkTerminalReason(event *orchestrator.EventEnvelope) string {
	if event == nil {
		return "none"
	}
	payload := map[string]any{}
	if len(event.Payload) > 0 {
		_ = json.Unmarshal(event.Payload, &payload)
	}
	source := strings.TrimSpace(toString(payload["source"]))
	switch event.Type {
	case orchestrator.EventTypeRunCompleted:
		return "completed"
	case orchestrator.EventTypeRunStopped:
		if source == "" {
			return "stopped"
		}
		return "stopped:" + source
	default:
		return event.Type
	}
}

func parseReportFindingCounts(path string) (int, int, int, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, false, err
	}
	defer file.Close()
	var (
		total      = -1
		verified   = -1
		unverified = -1
	)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "- Findings: ") {
			total = parseTrailingInt(line, "- Findings: ")
		} else if strings.HasPrefix(line, "- Verified findings: ") {
			verified = parseTrailingInt(line, "- Verified findings: ")
		} else if strings.HasPrefix(line, "- Unverified findings: ") {
			unverified = parseTrailingInt(line, "- Unverified findings: ")
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return 0, 0, 0, false, scanErr
	}
	if total < 0 || verified < 0 || unverified < 0 {
		return 0, 0, 0, false, nil
	}
	return total, verified, unverified, true, nil
}

func parseTrailingInt(line, prefix string) int {
	raw := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	n, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return n
}

func aggregateBenchmarkRuns(runs []benchmarkRunScorecard) benchmarkScenarioAggregate {
	out := benchmarkScenarioAggregate{
		Runs:                 len(runs),
		FailureReasonCounts:  map[string]int{},
		TerminalReasonCounts: map[string]int{},
	}
	if len(runs) == 0 {
		out.FailureReasonCounts = nil
		out.TerminalReasonCounts = nil
		return out
	}
	taskRates := make([]float64, 0, len(runs))
	precision := make([]float64, 0, len(runs))
	loopRates := make([]float64, 0, len(runs))
	recoveryRates := make([]float64, 0, len(runs))
	timeToFirst := make([]float64, 0, len(runs))
	durations := make([]float64, 0, len(runs))
	for _, run := range runs {
		if run.Succeeded {
			out.SucceededRuns++
		} else {
			out.FailedRuns++
		}
		taskRates = append(taskRates, run.Metrics.TaskSuccessRate)
		precision = append(precision, run.Metrics.VerifiedFindingPrecision)
		loopRates = append(loopRates, run.Metrics.LoopIncidentRate)
		recoveryRates = append(recoveryRates, run.Metrics.RecoverySuccessRate)
		durations = append(durations, run.DurationSec)
		for reason, count := range run.Metrics.FailureReasonCounts {
			if count <= 0 {
				continue
			}
			out.FailureReasonCounts[reason] += count
		}
		terminal := strings.TrimSpace(run.Metrics.TerminalReason)
		if terminal == "" {
			terminal = "none"
		}
		out.TerminalReasonCounts[terminal]++
		if run.Metrics.TimeToFirstVerifiedFindingAvailable {
			timeToFirst = append(timeToFirst, run.Metrics.TimeToFirstVerifiedFindingSeconds)
		}
	}
	out.TaskSuccessRate = distribution(taskRates)
	out.VerifiedFindingPrecision = distribution(precision)
	out.LoopIncidentRate = distribution(loopRates)
	out.RecoverySuccessRate = distribution(recoveryRates)
	out.TimeToFirstVerifiedFindingSec = distribution(timeToFirst)
	out.DurationSec = distribution(durations)
	if len(out.FailureReasonCounts) == 0 {
		out.FailureReasonCounts = nil
	}
	if len(out.TerminalReasonCounts) == 0 {
		out.TerminalReasonCounts = nil
	}
	return out
}

func distribution(values []float64) benchmarkDistribution {
	clean := make([]float64, 0, len(values))
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		clean = append(clean, value)
	}
	if len(clean) == 0 {
		return benchmarkDistribution{}
	}
	sort.Float64s(clean)
	dist := benchmarkDistribution{
		Count:  len(clean),
		Median: percentile(clean, 50),
		P90:    percentile(clean, 90),
	}
	return dist
}

func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	if p == 50 {
		mid := len(sorted) / 2
		if len(sorted)%2 == 0 {
			return (sorted[mid-1] + sorted[mid]) / 2
		}
		return sorted[mid]
	}
	rank := int(math.Ceil((float64(p) / 100.0) * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func loadBenchmarkScenarioPack(path string) (benchmarkScenarioPack, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return benchmarkScenarioPack{}, err
	}
	pack := benchmarkScenarioPack{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pack); err != nil {
		return benchmarkScenarioPack{}, err
	}
	pack.Version = strings.TrimSpace(pack.Version)
	if pack.Version == "" {
		return benchmarkScenarioPack{}, errors.New("scenario pack version is required")
	}
	if len(pack.Scenarios) == 0 {
		return benchmarkScenarioPack{}, errors.New("scenario pack has no scenarios")
	}
	seen := map[string]struct{}{}
	for i := range pack.Scenarios {
		scenario := &pack.Scenarios[i]
		if scenario.ID == "" {
			return benchmarkScenarioPack{}, fmt.Errorf("scenario[%d] id is required", i)
		}
		if _, ok := seen[scenario.ID]; ok {
			return benchmarkScenarioPack{}, fmt.Errorf("duplicate scenario id %q", scenario.ID)
		}
		seen[scenario.ID] = struct{}{}
		if scenario.Name == "" {
			scenario.Name = scenario.ID
		}
		if scenario.Goal == "" {
			return benchmarkScenarioPack{}, fmt.Errorf("scenario %q goal is required", scenario.ID)
		}
		if len(scenario.Scope.Targets) == 0 && len(scenario.Scope.Networks) == 0 {
			return benchmarkScenarioPack{}, fmt.Errorf("scenario %q requires at least one scope target/network", scenario.ID)
		}
		if len(scenario.Constraints) == 0 {
			return benchmarkScenarioPack{}, fmt.Errorf("scenario %q requires at least one constraint", scenario.ID)
		}
		if scenario.MaxParallelism < 0 {
			return benchmarkScenarioPack{}, fmt.Errorf("scenario %q max_parallelism must be >= 0", scenario.ID)
		}
		if scenario.MaxAttempts < 0 {
			return benchmarkScenarioPack{}, fmt.Errorf("scenario %q max_attempts must be >= 0", scenario.ID)
		}
		if scenario.Planner != "" {
			if _, err := orchestrator.ParsePlannerMode(scenario.Planner); err != nil {
				return benchmarkScenarioPack{}, fmt.Errorf("scenario %q invalid planner: %w", scenario.ID, err)
			}
		}
		if scenario.PermissionMode != "" {
			if err := validatePermissionMode(scenario.PermissionMode); err != nil {
				return benchmarkScenarioPack{}, fmt.Errorf("scenario %q invalid permission_mode: %w", scenario.ID, err)
			}
		}
	}
	return pack, nil
}

func validatePermissionMode(raw string) error {
	mode := orchestrator.PermissionMode(strings.TrimSpace(raw))
	switch mode {
	case orchestrator.PermissionReadonly, orchestrator.PermissionDefault, orchestrator.PermissionAll:
		return nil
	default:
		return fmt.Errorf("invalid permission mode %q", raw)
	}
}

func selectBenchmarkScenarios(all []benchmarkScenario, filter []string) ([]benchmarkScenario, error) {
	if len(filter) == 0 {
		return append([]benchmarkScenario{}, all...), nil
	}
	allowed := map[string]struct{}{}
	for _, value := range filter {
		allowed[strings.TrimSpace(value)] = struct{}{}
	}
	selected := make([]benchmarkScenario, 0, len(filter))
	for _, scenario := range all {
		if _, ok := allowed[scenario.ID]; ok {
			selected = append(selected, scenario)
		}
	}
	if len(selected) != len(allowed) {
		missing := make([]string, 0)
		for id := range allowed {
			found := false
			for _, scenario := range selected {
				if scenario.ID == id {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("unknown scenario ids: %s", strings.Join(missing, ", "))
	}
	return selected, nil
}

func benchmarkRunID(benchmarkID, scenarioID string, iteration int) string {
	return fmt.Sprintf("%s-%s-r%02d", benchmarkSlug(benchmarkID), benchmarkSlug(scenarioID), iteration)
}

func ensureBenchmarkRunIDAvailable(sessionsDir, preferred string) (string, error) {
	candidate := strings.TrimSpace(preferred)
	if candidate == "" {
		return "", errors.New("run id is required")
	}
	for i := 0; i < 1000; i++ {
		runID := candidate
		if i > 0 {
			runID = fmt.Sprintf("%s-%d", candidate, i+1)
		}
		paths := orchestrator.BuildRunPaths(sessionsDir, runID)
		_, err := os.Stat(paths.Root)
		if os.IsNotExist(err) {
			return runID, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate run id from %q", preferred)
}

func benchmarkResolvePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return trimmed
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return trimmed
	}
	return abs
}

func benchmarkSlug(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	normalized = strings.ReplaceAll(normalized, " ", "-")
	normalized = benchmarkSlugPattern.ReplaceAllString(normalized, "-")
	normalized = strings.Trim(normalized, "-")
	return normalized
}

func writeBenchmarkJSON(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func renderBenchmarkSummaryMarkdown(summary benchmarkSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Autonomy Benchmark Summary\n\n")
	fmt.Fprintf(&b, "- Benchmark ID: `%s`\n", summary.BenchmarkID)
	fmt.Fprintf(&b, "- Generated: %s\n", summary.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Pack version: `%s`\n", summary.PackVersion)
	fmt.Fprintf(&b, "- Scenarios: %d\n", len(summary.Scenarios))
	fmt.Fprintf(&b, "- Repeat: %d\n", summary.Repeat)
	fmt.Fprintf(&b, "- Seed: %d\n", summary.Seed)
	fmt.Fprintf(&b, "- Output: `%s`\n", filepath.ToSlash(summary.OutputDir))
	fmt.Fprintf(&b, "\n## Aggregate\n\n")
	fmt.Fprintf(&b, "- Runs: %d (success=%d failed=%d)\n", summary.Aggregate.Runs, summary.Aggregate.SucceededRuns, summary.Aggregate.FailedRuns)
	fmt.Fprintf(&b, "- Task success rate median/P90: %.3f / %.3f\n", summary.Aggregate.TaskSuccessRate.Median, summary.Aggregate.TaskSuccessRate.P90)
	fmt.Fprintf(&b, "- Verified finding precision median/P90: %.3f / %.3f\n", summary.Aggregate.VerifiedFindingPrecision.Median, summary.Aggregate.VerifiedFindingPrecision.P90)
	fmt.Fprintf(&b, "- Loop incident rate median/P90: %.3f / %.3f\n", summary.Aggregate.LoopIncidentRate.Median, summary.Aggregate.LoopIncidentRate.P90)
	fmt.Fprintf(&b, "- Recovery success rate median/P90: %.3f / %.3f\n", summary.Aggregate.RecoverySuccessRate.Median, summary.Aggregate.RecoverySuccessRate.P90)
	fmt.Fprintf(&b, "- Duration seconds median/P90: %.3f / %.3f\n", summary.Aggregate.DurationSec.Median, summary.Aggregate.DurationSec.P90)
	fmt.Fprintf(&b, "- Dominant failure reasons: %s\n", formatTopCountMap(summary.Aggregate.FailureReasonCounts, 5))
	fmt.Fprintf(&b, "- Terminalization reasons: %s\n", formatTopCountMap(summary.Aggregate.TerminalReasonCounts, 5))
	for _, scenario := range summary.Scenarios {
		fmt.Fprintf(&b, "\n## Scenario `%s`\n\n", scenario.Scenario.ID)
		fmt.Fprintf(&b, "- Name: %s\n", scenario.Scenario.Name)
		fmt.Fprintf(&b, "- Runs: %d (success=%d failed=%d)\n", scenario.Aggregate.Runs, scenario.Aggregate.SucceededRuns, scenario.Aggregate.FailedRuns)
		fmt.Fprintf(&b, "- Task success rate median/P90: %.3f / %.3f\n", scenario.Aggregate.TaskSuccessRate.Median, scenario.Aggregate.TaskSuccessRate.P90)
		fmt.Fprintf(&b, "- Verified finding precision median/P90: %.3f / %.3f\n", scenario.Aggregate.VerifiedFindingPrecision.Median, scenario.Aggregate.VerifiedFindingPrecision.P90)
		fmt.Fprintf(&b, "- Loop incident rate median/P90: %.3f / %.3f\n", scenario.Aggregate.LoopIncidentRate.Median, scenario.Aggregate.LoopIncidentRate.P90)
		fmt.Fprintf(&b, "- Dominant failure reasons: %s\n", formatTopCountMap(scenario.Aggregate.FailureReasonCounts, 5))
		fmt.Fprintf(&b, "- Terminalization reasons: %s\n", formatTopCountMap(scenario.Aggregate.TerminalReasonCounts, 5))
	}
	return b.String()
}

func formatTopCountMap(counts map[string]int, limit int) string {
	if len(counts) == 0 {
		return "none"
	}
	type item struct {
		key   string
		count int
	}
	items := make([]item, 0, len(counts))
	for key, count := range counts {
		if strings.TrimSpace(key) == "" || count <= 0 {
			continue
		}
		items = append(items, item{key: key, count: count})
	}
	if len(items) == 0 {
		return "none"
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].key < items[j].key
		}
		return items[i].count > items[j].count
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s=%d", it.key, it.count))
	}
	return strings.Join(parts, ", ")
}

func truncateForScorecard(text string, maxChars int) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= maxChars {
		return trimmed
	}
	if maxChars < 4 {
		return trimmed[:maxChars]
	}
	return trimmed[:maxChars-3] + "..."
}

func toString(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		if math.Mod(typed, 1) == 0 {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}
