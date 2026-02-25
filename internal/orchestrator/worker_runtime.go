package orchestrator

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Jawbreaker1/CodeHackBot/internal/msf"
)

const (
	OrchSessionsDirEnv = "BIRDHACKBOT_ORCH_SESSIONS_DIR"
	OrchRunIDEnv       = "BIRDHACKBOT_ORCH_RUN_ID"
	OrchTaskIDEnv      = "BIRDHACKBOT_ORCH_TASK_ID"
	OrchWorkerIDEnv    = "BIRDHACKBOT_ORCH_WORKER_ID"
	OrchAttemptEnv     = "BIRDHACKBOT_ORCH_ATTEMPT"
	OrchPermissionEnv  = "BIRDHACKBOT_ORCH_PERMISSION_MODE"
	OrchDisruptiveEnv  = "BIRDHACKBOT_ORCH_DISRUPTIVE_OPT_IN"
)

const dependencyArtifactReferenceMaxBytes = 64 * 1024
const wordlistDecompressMaxBytes = 512 * 1024 * 1024

const (
	WorkerFailureInvalidTaskAction    = "invalid_task_action"
	WorkerFailureWorkspaceCreate      = "workspace_create_failed"
	WorkerFailureInvalidWorkingDir    = "invalid_working_dir"
	WorkerFailureWorkingDirCreate     = "working_dir_create_failed"
	WorkerFailureArtifactWrite        = "artifact_write_failed"
	WorkerFailureCommandFailed        = "command_failed"
	WorkerFailureCommandTimeout       = "command_timeout"
	WorkerFailureCommandInterrupted   = "command_interrupted"
	WorkerFailureScopeDenied          = "scope_denied"
	WorkerFailurePolicyDenied         = "policy_denied"
	WorkerFailurePolicyInvalid        = "policy_invalid"
	WorkerFailureBootstrapFailed      = "worker_bootstrap_failed"
	WorkerFailureInsufficientEvidence = "insufficient_evidence"
	WorkerFailureAssistUnavailable    = "assist_unavailable"
	WorkerFailureAssistParseFailure   = "assist_parse_failure"
	WorkerFailureAssistTimeout        = "assist_timeout"
	WorkerFailureAssistNeedsInput     = "assist_needs_input"
	WorkerFailureAssistNoAction       = "assist_no_action"
	WorkerFailureAssistLoopDetected   = "assist_loop_detected"
	WorkerFailureAssistExhausted      = "assist_budget_exhausted"
)

var (
	errWorkerCommandTimeout     = errors.New("worker command timeout")
	errWorkerCommandInterrupted = errors.New("worker command interrupted")
	shellPathArgPattern         = regexp.MustCompile(`(?:\.\./|\./|/)[^\s"'` + "`" + `|&;<>]+`)
	artifactPathLinePattern     = regexp.MustCompile(`(?mi)^\s*path:\s*(.+?)\s*$`)
	artifactJSONPathPattern     = regexp.MustCompile(`(?mi)"path"\s*:\s*"([^"]+)"`)
	nmapCommandTokenPattern     = regexp.MustCompile(`(?i)(^|[\s;|&()])nmap([\s])`)
	genericArtifactHintTokens   = map[string]struct{}{
		"log":       {},
		"logs":      {},
		"output":    {},
		"outputs":   {},
		"result":    {},
		"results":   {},
		"report":    {},
		"reports":   {},
		"artifact":  {},
		"artifacts": {},
		"file":      {},
		"files":     {},
		"data":      {},
		"raw":       {},
		"txt":       {},
		"json":      {},
		"xml":       {},
		"md":        {},
		"csv":       {},
	}
)

const workerCommandStopGrace = 1500 * time.Millisecond

const (
	workerCommandOutputMaxBytes = 2 * 1024 * 1024
	workerCommandTruncationNote = "\n...[output truncated]...\n"
)

type WorkerRunConfig struct {
	SessionsDir string
	RunID       string
	TaskID      string
	WorkerID    string
	Attempt     int
	Permission  PermissionMode
	Disruptive  bool
}

func ParseWorkerRunConfig(getenv func(string) string) WorkerRunConfig {
	attempt, _ := strconv.Atoi(strings.TrimSpace(getenv(OrchAttemptEnv)))
	disruptive, _ := strconv.ParseBool(strings.TrimSpace(getenv(OrchDisruptiveEnv)))
	mode := PermissionMode(strings.TrimSpace(getenv(OrchPermissionEnv)))
	if mode == "" {
		mode = PermissionDefault
	}
	return WorkerRunConfig{
		SessionsDir: strings.TrimSpace(getenv(OrchSessionsDirEnv)),
		RunID:       strings.TrimSpace(getenv(OrchRunIDEnv)),
		TaskID:      strings.TrimSpace(getenv(OrchTaskIDEnv)),
		WorkerID:    strings.TrimSpace(getenv(OrchWorkerIDEnv)),
		Attempt:     attempt,
		Permission:  mode,
		Disruptive:  disruptive,
	}
}

func RunWorkerTask(cfg WorkerRunConfig) error {
	if strings.TrimSpace(cfg.SessionsDir) == "" {
		return fmt.Errorf("sessions dir is required")
	}
	if strings.TrimSpace(cfg.RunID) == "" {
		return fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(cfg.TaskID) == "" {
		return fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(cfg.WorkerID) == "" {
		return fmt.Errorf("worker id is required")
	}
	if cfg.Attempt < 0 {
		return fmt.Errorf("attempt must be >= 0")
	}
	if cfg.Permission == "" {
		cfg.Permission = PermissionDefault
	}

	manager := NewManager(cfg.SessionsDir)
	done, err := workerAttemptAlreadyCompleted(manager, cfg)
	if err != nil {
		return err
	}
	if done {
		return nil
	}
	task, err := manager.ReadTask(cfg.RunID, cfg.TaskID)
	if err != nil {
		return err
	}
	plan, err := manager.LoadRunPlan(cfg.RunID)
	if err != nil {
		return err
	}
	scopePolicy := NewScopePolicy(plan.Scope)
	signalWorkerID := WorkerSignalID(cfg.WorkerID)
	if err := scopePolicy.ValidateTaskTargets(task); err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureScopeDenied, nil)
		return err
	}
	action, err := normalizeTaskAction(task.Action)
	if err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureInvalidTaskAction, nil)
		return err
	}
	requiredAttribution := targetAttribution{}
	if action.Type != "assist" {
		if taskNeedsTargetAttribution(task) {
			requiredAttribution, err = resolveTaskTargetAttribution(cfg, task, scopePolicy)
			if err != nil {
				_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureInsufficientEvidence, map[string]any{
					"detail": "target_attribution_unresolved",
				})
				return err
			}
			if strings.TrimSpace(requiredAttribution.Target) != "" {
				task.Targets = []string{strings.TrimSpace(requiredAttribution.Target)}
			}
			if nextArgs, note, rewritten := enforceAttributedCommandTarget(action.Command, action.Args, requiredAttribution.Target); rewritten {
				action.Args = nextArgs
				_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":   note,
					"step":      0,
					"tool_call": 0,
					"command":   action.Command,
					"args":      action.Args,
				})
			}
		}
		var injected bool
		var target string
		action.Args, injected, target = applyCommandTargetFallback(scopePolicy, task, action.Command, action.Args)
		if injected {
			_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":   fmt.Sprintf("auto-injected target %s for command %s", target, action.Command),
				"step":      0,
				"tool_call": 0,
				"command":   action.Command,
				"args":      action.Args,
			})
		}
		var note string
		action.Command, action.Args, note, injected = adaptCommandForRuntime(scopePolicy, action.Command, action.Args)
		if injected && note != "" {
			_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":   note,
				"step":      0,
				"tool_call": 0,
				"command":   action.Command,
				"args":      action.Args,
			})
		}
		action.Args, injected, target = applyCommandTargetFallback(scopePolicy, task, action.Command, action.Args)
		if injected {
			_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":   fmt.Sprintf("auto-injected target %s for command %s after runtime adaptation", target, action.Command),
				"step":      0,
				"tool_call": 0,
				"command":   action.Command,
				"args":      action.Args,
			})
		}
		if nextCommand, nextArgs, note, rewritten := adaptWeakReportAction(cfg, task, scopePolicy, action.Command, action.Args, requiredAttribution); rewritten {
			action.Command = nextCommand
			action.Args = nextArgs
			_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":   note,
				"step":      0,
				"tool_call": 0,
				"command":   action.Command,
				"args":      action.Args,
			})
		}
		if nextArgs, note, rewritten := ensureVulnerabilityEvidenceActionWithGoal(task, plan.Metadata.Goal, action.Command, action.Args); rewritten {
			action.Args = nextArgs
			_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":   note,
				"step":      0,
				"tool_call": 0,
				"command":   action.Command,
				"args":      action.Args,
			})
		}
		if nextCommand, nextArgs, note, rewritten := adaptWeakVulnerabilityAction(task, scopePolicy, action.Command, action.Args); rewritten {
			action.Command = nextCommand
			action.Args = nextArgs
			_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":   note,
				"step":      0,
				"tool_call": 0,
				"command":   action.Command,
				"args":      action.Args,
			})
		}
	}
	if requiresCommandScopeValidation(action.Command, action.Args) {
		if err := scopePolicy.ValidateCommandTargets(action.Command, action.Args); err != nil {
			_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureScopeDenied, nil)
			return err
		}
	}
	if reason, err := enforceWorkerRiskPolicy(task, cfg); err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, reason, nil)
		return err
	}
	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskStarted, map[string]any{
		"attempt":   cfg.Attempt,
		"worker_id": cfg.WorkerID,
		"goal":      task.Goal,
	})

	workspaceDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).Root, "workers", cfg.WorkerID)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureWorkspaceCreate, nil)
		return fmt.Errorf("create worker workspace: %w", err)
	}
	workDir, err := resolveActionWorkingDir(action.WorkingDir, workspaceDir)
	if err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureInvalidWorkingDir, nil)
		return err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureWorkingDirCreate, nil)
		return fmt.Errorf("create action working dir: %w", err)
	}
	if action.Type != "assist" {
		if adaptedCmd, adaptedArgs, notes, adaptErr := msf.AdaptRuntimeCommand(action.Command, action.Args, workDir); adaptErr != nil {
			_ = emitWorkerFailure(manager, cfg, task, adaptErr, WorkerFailureBootstrapFailed, map[string]any{
				"command": action.Command,
				"args":    action.Args,
			})
			return adaptErr
		} else {
			action.Command = adaptedCmd
			action.Args = adaptedArgs
			for _, note := range notes {
				if strings.TrimSpace(note) == "" {
					continue
				}
				_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":   note,
					"step":      0,
					"tool_call": 0,
					"command":   action.Command,
					"args":      action.Args,
				})
			}
		}
		if repairedArgs, repairNotes, repaired, repairErr := repairMissingCommandInputPaths(cfg, task, action.Command, action.Args); repairErr != nil {
			_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":   fmt.Sprintf("runtime input repair skipped: %v", repairErr),
				"step":      0,
				"tool_call": 0,
				"command":   action.Command,
				"args":      action.Args,
			})
		} else if repaired {
			action.Args = repairedArgs
			for _, note := range repairNotes {
				if strings.TrimSpace(note) == "" {
					continue
				}
				_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":   note,
					"step":      0,
					"tool_call": 0,
					"command":   action.Command,
					"args":      action.Args,
				})
			}
		}
		if requiresCommandScopeValidation(action.Command, action.Args) {
			if err := scopePolicy.ValidateCommandTargets(action.Command, action.Args); err != nil {
				_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureScopeDenied, nil)
				return err
			}
		}
	}

	timeout := resolveActionTimeout(task.Budget.MaxRuntime, action.TimeoutSeconds)
	signalCtx, stopSignals := withWorkerSignalContext(context.Background())
	defer stopSignals()
	ctx, cancel := context.WithTimeout(signalCtx, timeout)
	defer cancel()

	if action.Type == "assist" {
		return runWorkerAssistTask(ctx, manager, cfg, task, action, scopePolicy, plan.Scope, workDir)
	}

	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
		"message":    "executing action command",
		"step":       1,
		"tool_call":  1,
		"command":    action.Command,
		"args":       action.Args,
		"workingDir": workDir,
		"timeout":    timeout.String(),
	})

	execCommand := strings.TrimSpace(action.Command)
	execArgs := append([]string{}, action.Args...)
	var output []byte
	var runErr error
	if isWorkerBuiltinCommand(execCommand) {
		result := executeWorkerAssistCommand(ctx, execCommand, execArgs, workDir)
		execCommand = strings.TrimSpace(result.command)
		execArgs = append([]string{}, result.args...)
		output = result.output
		runErr = result.runErr
	} else {
		cmd := exec.Command(execCommand, execArgs...)
		cmd.Dir = workDir
		cmd.Env = os.Environ()
		output, runErr = runWorkerCommand(ctx, cmd, workerCommandStopGrace)
		if shouldRetryNmapForHostTimeout(execCommand, execArgs, output, runErr) {
			remaining := remainingContextDuration(ctx)
			if retryArgs, retryNote, retryOK := buildNmapEvidenceRetryArgs(execArgs, remaining); retryOK {
				_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":   retryNote,
					"step":      1,
					"tool_call": 2,
					"command":   execCommand,
					"args":      retryArgs,
				})
				retryCmd := exec.Command(execCommand, retryArgs...)
				retryCmd.Dir = workDir
				retryCmd.Env = os.Environ()
				retryOutput, retryErr := runWorkerCommand(ctx, retryCmd, workerCommandStopGrace)
				output = mergeRetryOutput(output, retryOutput, retryNote)
				runErr = retryErr
				execArgs = retryArgs
			}
		}
	}
	exitCode := 0
	if runErr != nil {
		exitCode = commandExitCode(runErr)
	}

	logPath, logErr := writeWorkerCommandLog(cfg, output)
	if logErr != nil {
		_ = emitWorkerFailure(manager, cfg, task, logErr, WorkerFailureArtifactWrite, map[string]any{
			"command_error": runErrString(runErr),
		})
		if runErr != nil {
			return fmt.Errorf("%w (command error: %v)", logErr, runErr)
		}
		return logErr
	}

	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskArtifact, map[string]any{
		"type":      "command_log",
		"title":     fmt.Sprintf("worker command output (%s)", cfg.TaskID),
		"path":      logPath,
		"command":   execCommand,
		"args":      execArgs,
		"exit_code": exitCode,
	})
	producedArtifacts := []string{logPath}

	if runErr != nil {
		reason := WorkerFailureCommandFailed
		if errors.Is(runErr, errWorkerCommandTimeout) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = WorkerFailureCommandTimeout
		} else if errors.Is(runErr, errWorkerCommandInterrupted) {
			reason = WorkerFailureCommandInterrupted
		}
		_ = emitWorkerFailure(manager, cfg, task, runErr, reason, map[string]any{
			"command":   execCommand,
			"args":      execArgs,
			"log_path":  logPath,
			"exit_code": exitCode,
		})
		return runErr
	}
	if evidenceErr := validateCommandOutputEvidence(execCommand, execArgs, output); evidenceErr != nil {
		_ = emitWorkerFailure(manager, cfg, task, evidenceErr, WorkerFailureInsufficientEvidence, map[string]any{
			"command":   execCommand,
			"args":      execArgs,
			"log_path":  logPath,
			"exit_code": exitCode,
		})
		return evidenceErr
	}
	if evidenceErr := validateVulnerabilityEvidenceAuthenticity(task, execCommand, execArgs, output); evidenceErr != nil {
		_ = emitWorkerFailure(manager, cfg, task, evidenceErr, WorkerFailureInsufficientEvidence, map[string]any{
			"command":   execCommand,
			"args":      execArgs,
			"log_path":  logPath,
			"exit_code": exitCode,
		})
		return evidenceErr
	}
	// Goal-seeded plans frequently specify semantic artifact names (for example
	// "service_version_output.txt") that command tasks may not publish into the
	// orchestrator artifact store by default. Prefer copying real files produced
	// in the action working directory; fall back to command output only when no
	// produced file exists for the expected artifact name.
	materializedArtifacts, materializeErr := writeExpectedCommandArtifacts(cfg, task, workDir, output, logPath)
	if materializeErr != nil {
		_ = emitWorkerFailure(manager, cfg, task, materializeErr, WorkerFailureArtifactWrite, map[string]any{
			"command":  execCommand,
			"args":     execArgs,
			"log_path": logPath,
		})
		return materializeErr
	}
	for _, artifactPath := range materializedArtifacts {
		producedArtifacts = appendUnique(producedArtifacts, artifactPath)
		_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskArtifact, map[string]any{
			"type":      "derived_command_output",
			"title":     fmt.Sprintf("derived expected artifact (%s)", cfg.TaskID),
			"path":      artifactPath,
			"command":   execCommand,
			"args":      execArgs,
			"exit_code": exitCode,
		})
	}
	attrForArtifact := inferTaskCompletionTargetAttribution(task, output)
	if strings.TrimSpace(requiredAttribution.Target) != "" {
		attrForArtifact = requiredAttribution
	}
	if attrPath, attrErr := writeTargetAttributionArtifact(cfg, task, attrForArtifact); attrErr != nil {
		_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":   fmt.Sprintf("target attribution artifact skipped: %v", attrErr),
			"step":      1,
			"tool_call": 1,
			"command":   execCommand,
			"args":      execArgs,
		})
	} else if strings.TrimSpace(attrPath) != "" {
		producedArtifacts = appendUnique(producedArtifacts, attrPath)
		_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskArtifact, map[string]any{
			"type":       "target_attribution",
			"title":      fmt.Sprintf("resolved target attribution (%s)", cfg.TaskID),
			"path":       attrPath,
			"target":     attrForArtifact.Target,
			"confidence": attrForArtifact.Confidence,
			"source":     attrForArtifact.Source,
		})
	}
	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskFinding, map[string]any{
		"target":       primaryTaskTarget(task),
		"finding_type": "task_execution_result",
		"title":        "task action completed",
		"state":        FindingStateVerified,
		"severity":     "info",
		"confidence":   "high",
		"source":       "worker_runtime",
		"evidence":     producedArtifacts,
		"metadata": map[string]any{
			"attempt": cfg.Attempt,
			"reason":  "action_completed",
		},
	})

	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskCompleted, map[string]any{
		"attempt":   cfg.Attempt,
		"worker_id": cfg.WorkerID,
		"reason":    "action_completed",
		"log_path":  logPath,
		"completion_contract": map[string]any{
			"status_reason":       "action_completed",
			"required_artifacts":  task.ExpectedArtifacts,
			"produced_artifacts":  producedArtifacts,
			"required_findings":   []string{"task_execution_result"},
			"produced_findings":   []string{"task_execution_result"},
			"verification_status": "reported_by_worker",
		},
	})
	return nil
}

func normalizeTaskAction(action TaskAction) (TaskAction, error) {
	a := action
	a.Type = strings.ToLower(strings.TrimSpace(a.Type))
	a.Command = strings.TrimSpace(a.Command)
	a.Args = normalizeActionArgs(a.Args)
	if a.Type == "" {
		a.Type = "command"
	}
	switch a.Type {
	case "command":
		if a.Command == "" {
			return TaskAction{}, fmt.Errorf("task action command is required")
		}
		a.Command, a.Args = normalizeCommandAction(a.Command, a.Args)
	case "shell":
		if a.Command == "" {
			return TaskAction{}, fmt.Errorf("task shell action command is required")
		}
		a.Args = []string{"-lc", a.Command}
		a.Command = "bash"
	case "assist":
		if a.Command != "" || len(a.Args) > 0 {
			return TaskAction{}, fmt.Errorf("assist action cannot set command/args")
		}
	default:
		return TaskAction{}, fmt.Errorf("unsupported action type %q", a.Type)
	}
	return a, nil
}

func normalizeActionArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func normalizeCommandAction(command string, args []string) (string, []string) {
	command = strings.TrimSpace(command)
	args = normalizeActionArgs(args)
	if command == "" {
		return "", args
	}
	if len(args) > 0 {
		return command, args
	}
	if looksLikeShellExpression(command) {
		return "bash", []string{"-lc", command}
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return command, nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return parts[0], parts[1:]
}

func looksLikeShellExpression(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	shellTokens := []string{
		"||", "&&", "|", ";", "$(", "`", ">", "<", "\n",
	}
	for _, token := range shellTokens {
		if strings.Contains(command, token) {
			return true
		}
	}
	return false
}

func resolveActionWorkingDir(configured, workspaceDir string) (string, error) {
	trimmed := strings.TrimSpace(configured)
	if trimmed == "" {
		return workspaceDir, nil
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	base := filepath.Clean(workspaceDir)
	resolved := filepath.Clean(filepath.Join(base, trimmed))
	rel, err := filepath.Rel(base, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve action working dir: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("working dir escapes worker workspace: %s", configured)
	}
	return resolved, nil
}

func resolveActionTimeout(maxRuntime time.Duration, timeoutSeconds int) time.Duration {
	if timeoutSeconds > 0 {
		actionTimeout := time.Duration(timeoutSeconds) * time.Second
		if maxRuntime > 0 && actionTimeout > maxRuntime {
			return maxRuntime
		}
		return actionTimeout
	}
	if maxRuntime > 0 {
		return maxRuntime
	}
	return 5 * time.Minute
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func writeWorkerCommandLog(cfg WorkerRunConfig, output []byte) (string, error) {
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	name := fmt.Sprintf("%s-a%d.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt)
	path := filepath.Join(artifactDir, name)
	if err := os.WriteFile(path, output, 0o644); err != nil {
		return "", fmt.Errorf("write command log: %w", err)
	}
	return path, nil
}

func writeExpectedCommandArtifacts(cfg WorkerRunConfig, task TaskSpec, workDir string, output []byte, commandLogPath string) ([]string, error) {
	if len(task.ExpectedArtifacts) == 0 {
		return nil, nil
	}
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact dir: %w", err)
	}
	cleanLogPath := filepath.Clean(commandLogPath)
	written := []string{}
	seen := map[string]struct{}{}
	for _, expected := range task.ExpectedArtifacts {
		expected = strings.TrimSpace(expected)
		name := strings.TrimSpace(filepath.Base(expected))
		if name == "" || name == "." {
			continue
		}
		path := filepath.Join(artifactDir, name)
		cleanPath := filepath.Clean(path)
		if cleanPath == cleanLogPath {
			continue
		}
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}
		var (
			content []byte
			err     error
		)
		if !preferCommandOutputForExpectedArtifact(task, name) {
			content, _, err = loadExpectedArtifactContent(workDir, expected, name)
			if err != nil {
				return nil, fmt.Errorf("load expected artifact %q: %w", name, err)
			}
		}
		if len(content) == 0 {
			content = output
		}
		if len(content) == 0 {
			content = []byte("no command output captured\n")
		}
		if err := os.WriteFile(cleanPath, content, 0o644); err != nil {
			return nil, fmt.Errorf("write expected artifact %q: %w", name, err)
		}
		written = append(written, cleanPath)
	}
	return appendUnique(nil, written...), nil
}

func preferCommandOutputForExpectedArtifact(task TaskSpec, artifactName string) bool {
	name := strings.TrimSpace(filepath.Base(artifactName))
	if name == "" {
		return false
	}
	if !taskRequiresReportSynthesis(task) {
		return false
	}
	return hasSecurityReportArtifact([]string{name})
}

func loadExpectedArtifactContent(workDir, expected, baseName string) ([]byte, string, error) {
	sourcePath, ok := resolveExpectedArtifactSourcePath(workDir, expected, baseName)
	if !ok {
		return nil, "", nil
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, sourcePath, err
	}
	return data, sourcePath, nil
}

func resolveExpectedArtifactSourcePath(workDir, expected, baseName string) (string, bool) {
	candidates := []string{}
	expected = strings.TrimSpace(expected)
	if expected != "" {
		if filepath.IsAbs(expected) {
			candidates = append(candidates, filepath.Clean(expected))
		} else {
			trimmed := strings.TrimPrefix(expected, "."+string(filepath.Separator))
			if trimmed != "" {
				candidates = append(candidates, filepath.Clean(filepath.Join(workDir, trimmed)))
			}
		}
	}
	if strings.TrimSpace(baseName) != "" {
		candidates = append(candidates, filepath.Clean(filepath.Join(workDir, baseName)))
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, done := seen[candidate]; done {
			continue
		}
		seen[candidate] = struct{}{}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		return candidate, true
	}
	return "", false
}

func repairMissingCommandInputPaths(cfg WorkerRunConfig, task TaskSpec, command string, args []string) ([]string, []string, bool, error) {
	if len(args) == 0 {
		return args, nil, false, nil
	}
	localTargetsOnly := taskTargetsLocalhostOnly(task)
	localWorkflow := taskLikelyLocalFileWorkflow(task)
	candidates := []string{}
	candidateSources := map[string]string{}
	wordlistCandidates, wordlistSources := collectWordlistCandidates(args)
	if len(wordlistCandidates) > 0 {
		candidates = append(candidates, wordlistCandidates...)
		for path, source := range wordlistSources {
			candidateSources[path] = source
		}
	}
	if len(task.DependsOn) > 0 {
		depCandidates, err := collectDependencyArtifactCandidates(cfg, task.DependsOn)
		if err != nil {
			return args, nil, false, err
		}
		candidates = append(candidates, depCandidates...)
	}
	shellCandidates := append([]string{}, candidates...)
	if localTargetsOnly {
		workspaceCandidates := collectWorkspaceCandidatesFromArgs(cfg, args)
		shellCandidates = append(workspaceCandidates, shellCandidates...)
	}
	if repairedArgs, repairNotes, repaired := repairMissingCommandInputPathsForShellWrapper(command, args, shellCandidates, candidateSources); repaired {
		return repairedArgs, appendUnique(nil, repairNotes...), true, nil
	}
	if !commandLikelyReadsLocalFiles(command) {
		return args, nil, false, nil
	}

	nextArgs := append([]string{}, args...)
	notes := []string{}
	changed := false
	used := map[string]struct{}{}

	for i, arg := range args {
		if !looksLikePathArg(arg) {
			continue
		}
		candidate := strings.TrimSpace(arg)
		if candidate == "" {
			continue
		}
		if _, statErr := os.Stat(candidate); statErr == nil {
			continue
		}
		source := ""
		preferWorkspace := localTargetsOnly && (localWorkflow || pathLikelyWorkspaceInput(candidate))
		replacement := ""
		if preferWorkspace {
			replacement = bestWorkspaceCandidateForMissingPath(cfg, candidate)
			if replacement != "" {
				source = "local workspace"
			}
		}
		if replacement == "" {
			replacement = bestArtifactCandidateForMissingPath(candidate, candidates, used)
			if replacement != "" {
				if customSource, ok := candidateSources[replacement]; ok && strings.TrimSpace(customSource) != "" {
					source = customSource
				} else {
					source = "dependency artifact"
				}
			}
		}
		if replacement == "" && localTargetsOnly {
			replacement = bestWorkspaceCandidateForMissingPath(cfg, candidate)
			if replacement != "" {
				source = "local workspace"
			}
		}
		if replacement == "" {
			continue
		}
		nextArgs[i] = replacement
		used[replacement] = struct{}{}
		changed = true
		if source == "" {
			source = "fallback candidate"
		}
		notes = append(notes, fmt.Sprintf("runtime input repair: replaced missing path %s with %s %s", candidate, source, replacement))
	}

	if !changed {
		return args, nil, false, nil
	}
	return nextArgs, appendUnique(nil, notes...), true, nil
}

func bestWorkspaceCandidateForMissingPath(cfg WorkerRunConfig, missingPath string) string {
	baseName := filepath.Base(strings.TrimSpace(missingPath))
	if baseName == "" || baseName == "." || baseName == ".." {
		return ""
	}
	for _, root := range localWorkspaceRoots(cfg) {
		candidate := filepath.Join(root, baseName)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		return candidate
	}
	return ""
}

func localWorkspaceRoots(cfg WorkerRunConfig) []string {
	roots := []string{}
	if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
		roots = append(roots, wd)
	}
	sessionsDir := strings.TrimSpace(cfg.SessionsDir)
	if sessionsDir != "" {
		abs := sessionsDir
		if !filepath.IsAbs(abs) {
			if resolved, err := filepath.Abs(abs); err == nil {
				abs = resolved
			}
		}
		if strings.TrimSpace(abs) != "" {
			roots = append(roots, filepath.Dir(abs))
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func taskLikelyLocalFileWorkflow(task TaskSpec) bool {
	if !taskTargetsLocalhostOnly(task) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		strings.Join(task.ExpectedArtifacts, " "),
	}, " ")))
	return containsAnySubstring(text, "archive", "zip", "password", "extract", "decrypt", "file")
}

func taskTargetsLocalhostOnly(task TaskSpec) bool {
	if len(task.Targets) == 0 {
		return true
	}
	for _, target := range task.Targets {
		normalized := strings.ToLower(strings.TrimSpace(target))
		if normalized == "" {
			continue
		}
		if normalized != "127.0.0.1" && normalized != "localhost" {
			return false
		}
	}
	return true
}

func pathLikelyWorkspaceInput(path string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	if base == "" || base == "." || base == ".." {
		return false
	}
	switch filepath.Ext(base) {
	case ".zip", ".7z", ".tar", ".gz", ".bz2", ".xz", ".tgz":
		return true
	}
	return strings.HasSuffix(base, ".hash")
}

func collectWorkspaceCandidatesFromArgs(cfg WorkerRunConfig, args []string) []string {
	candidates := []string{}
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if !looksLikePathArg(trimmed) {
			continue
		}
		if candidate := bestWorkspaceCandidateForMissingPath(cfg, trimmed); candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	if body, ok := shellWrapperBody(args); ok {
		for _, match := range shellPathArgPattern.FindAllString(body, -1) {
			if !looksLikePathArg(match) {
				continue
			}
			if candidate := bestWorkspaceCandidateForMissingPath(cfg, match); candidate != "" {
				candidates = append(candidates, candidate)
			}
		}
	}
	return appendUnique(nil, candidates...)
}

func shellWrapperBody(args []string) (string, bool) {
	if len(args) < 2 {
		return "", false
	}
	mode := strings.TrimSpace(args[0])
	if mode != "-c" && mode != "-lc" {
		return "", false
	}
	body := strings.TrimSpace(args[1])
	if body == "" {
		return "", false
	}
	return body, true
}

func collectWordlistCandidates(args []string) ([]string, map[string]string) {
	missingPaths := missingWordlistPathsFromArgs(args)
	if len(missingPaths) == 0 {
		return nil, map[string]string{}
	}
	candidates := []string{}
	sources := map[string]string{}
	for _, missingPath := range missingPaths {
		candidate, source := resolveWordlistCandidate(missingPath)
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		candidates = append(candidates, candidate)
		if strings.TrimSpace(source) != "" {
			sources[candidate] = source
		}
	}
	return appendUnique(nil, candidates...), sources
}

func missingWordlistPathsFromArgs(args []string) []string {
	paths := []string{}
	for _, arg := range args {
		candidate := strings.TrimSpace(arg)
		if looksLikePathArg(candidate) && looksLikeWordlistPath(candidate) {
			if _, statErr := os.Stat(candidate); statErr != nil {
				paths = append(paths, candidate)
			}
		}
	}
	if body, ok := shellWrapperBody(args); ok {
		for _, path := range shellPathArgPattern.FindAllString(body, -1) {
			candidate := strings.TrimSpace(path)
			if !looksLikeWordlistPath(candidate) {
				continue
			}
			if _, statErr := os.Stat(candidate); statErr != nil {
				paths = append(paths, candidate)
			}
		}
	}
	return appendUnique(nil, paths...)
}

func looksLikeWordlistPath(path string) bool {
	normalized := strings.ToLower(strings.TrimSpace(path))
	if normalized == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(normalized))
	if strings.Contains(normalized, "/wordlist") || strings.Contains(normalized, "/dict/") {
		return true
	}
	if strings.HasSuffix(base, ".lst") || strings.HasSuffix(base, ".dic") {
		return true
	}
	if strings.HasSuffix(base, ".txt") && (strings.Contains(base, "rockyou") || strings.Contains(base, "word") || strings.Contains(base, "pass")) {
		return true
	}
	return false
}

func resolveWordlistCandidate(missingPath string) (string, string) {
	requested := strings.TrimSpace(missingPath)
	if requested == "" {
		return "", ""
	}
	if info, err := os.Stat(requested); err == nil && !info.IsDir() {
		return requested, "local wordlist"
	}
	localArchive := requested + ".gz"
	if info, err := os.Stat(localArchive); err == nil && !info.IsDir() {
		path, decompressErr := ensureDecompressedWordlist(requested, localArchive)
		if decompressErr == nil {
			return path, "decompressed local wordlist archive"
		}
	}
	baseName := strings.TrimSpace(filepath.Base(requested))
	if baseName == "" || baseName == "." || baseName == ".." {
		return "", ""
	}
	systemWordlist := filepath.Join("/usr/share/wordlists", baseName)
	if info, err := os.Stat(systemWordlist); err == nil && !info.IsDir() {
		return systemWordlist, "system wordlist"
	}
	systemArchive := systemWordlist + ".gz"
	if info, err := os.Stat(systemArchive); err == nil && !info.IsDir() {
		path, decompressErr := ensureDecompressedWordlist(systemWordlist, systemArchive)
		if decompressErr == nil {
			return path, "decompressed system wordlist archive"
		}
	}
	return "", ""
}

func ensureDecompressedWordlist(requestedPath, archivePath string) (string, error) {
	wordlistDir := filepath.Join(os.TempDir(), "birdhackbot-wordlists")
	if err := os.MkdirAll(wordlistDir, 0o755); err != nil {
		return "", err
	}
	outName := strings.TrimSpace(filepath.Base(requestedPath))
	if outName == "" || outName == "." || outName == ".." {
		outName = strings.TrimSuffix(filepath.Base(strings.TrimSpace(archivePath)), ".gz")
	}
	if strings.HasSuffix(strings.ToLower(outName), ".gz") {
		outName = strings.TrimSuffix(outName, ".gz")
	}
	if outName == "" || outName == "." || outName == ".." {
		outName = "wordlist.txt"
	}
	outPath := filepath.Join(wordlistDir, outName)
	if info, err := os.Stat(outPath); err == nil && !info.IsDir() && info.Size() > 0 {
		return outPath, nil
	}
	src, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer src.Close()
	reader, err := gzip.NewReader(src)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	tmpPath := fmt.Sprintf("%s.tmp-%d", outPath, time.Now().UTC().UnixNano())
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	limited := &io.LimitedReader{R: reader, N: wordlistDecompressMaxBytes + 1}
	n, copyErr := io.Copy(dst, limited)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", closeErr
	}
	if n > wordlistDecompressMaxBytes {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("decompressed wordlist exceeds limit")
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return outPath, nil
}

func repairMissingCommandInputPathsForShellWrapper(command string, args []string, candidates []string, candidateSources map[string]string) ([]string, []string, bool) {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if base != "bash" && base != "sh" && base != "zsh" {
		return args, nil, false
	}
	body, ok := shellWrapperBody(args)
	if !ok || !shellScriptLikelyReadsLocalFiles(body) {
		return args, nil, false
	}

	changedBody := body
	notes := []string{}
	changed := false
	used := map[string]struct{}{}
	fields := strings.Fields(body)
	skipNextAsOutputPath := false

	for _, field := range fields {
		token := strings.TrimSpace(field)
		if token == "" {
			continue
		}
		if isOutputRedirectionOperatorToken(token) {
			skipNextAsOutputPath = true
			continue
		}
		if skipNextAsOutputPath {
			skipNextAsOutputPath = false
			continue
		}
		if tokenStartsWithOutputRedirection(token) {
			continue
		}

		for _, match := range shellPathArgPattern.FindAllStringIndex(token, -1) {
			if len(match) != 2 {
				continue
			}
			start := match[0]
			end := match[1]
			if start < 0 || end <= start || end > len(token) {
				continue
			}
			if !isShellPathMatchBoundary(token, start) {
				continue
			}
			candidate := token[start:end]
			if !looksLikePathArg(candidate) {
				continue
			}
			if _, statErr := os.Stat(candidate); statErr == nil {
				continue
			}
			replacement := bestArtifactCandidateForMissingPath(candidate, candidates, used)
			if replacement == "" {
				continue
			}
			replacementToken := replacement
			if !tokenContainsQuotedPath(token, candidate) {
				replacementToken = shellQuotePath(replacement)
			}
			changedBody = strings.Replace(changedBody, candidate, replacementToken, 1)
			used[replacement] = struct{}{}
			changed = true
			source := "dependency artifact"
			if customSource, ok := candidateSources[replacement]; ok && strings.TrimSpace(customSource) != "" {
				source = customSource
			}
			notes = append(notes, fmt.Sprintf("runtime input repair: replaced missing path %s with %s %s", candidate, source, replacement))
		}
	}

	if !changed {
		return args, nil, false
	}
	nextArgs := append([]string{}, args...)
	nextArgs[1] = changedBody
	return nextArgs, notes, true
}

func shellScriptLikelyReadsLocalFiles(body string) bool {
	for _, token := range shellCommandTokens(body) {
		if commandLikelyReadsLocalFiles(token) {
			return true
		}
	}
	return false
}

func isOutputRedirectionOperatorToken(token string) bool {
	switch strings.TrimSpace(token) {
	case ">", ">>", "1>", "1>>", "2>", "2>>", "&>", "&>>":
		return true
	default:
		return false
	}
}

func tokenStartsWithOutputRedirection(token string) bool {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, ">") ||
		strings.HasPrefix(trimmed, "1>") ||
		strings.HasPrefix(trimmed, "2>") ||
		strings.HasPrefix(trimmed, "&>")
}

func tokenContainsQuotedPath(token, path string) bool {
	return strings.Contains(token, `"`+path+`"`) || strings.Contains(token, `'`+path+`'`)
}

func shellQuotePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return path
	}
	if !strings.ContainsAny(trimmed, " \t\n\r\"'`$&;|<>") {
		return trimmed
	}
	escaped := strings.ReplaceAll(trimmed, `'`, `'"'"'`)
	return "'" + escaped + "'"
}

func isShellPathMatchBoundary(token string, start int) bool {
	if start <= 0 || start >= len(token) {
		return true
	}
	prev := rune(token[start-1])
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev) && prev != '_' && prev != '-' && prev != '.'
}

func commandLikelyReadsLocalFiles(command string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "7z", "awk", "cat", "cut", "egrep", "fcrackzip", "fgrep", "file", "grep", "head", "john", "jq", "less", "list_dir", "ls", "more", "sed", "sort", "stat", "tail", "uniq", "unzip", "wc", "zip2john", "zipinfo":
		return true
	default:
		return false
	}
}

func looksLikePathArg(arg string) bool {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../")
}

func collectDependencyArtifactCandidates(cfg WorkerRunConfig, dependencies []string) ([]string, error) {
	base := BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir
	candidates := []string{}
	for _, dep := range compactStringSlice(dependencies) {
		depDir := filepath.Join(base, dep)
		info, err := os.Stat(depDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			continue
		}
		if err := filepath.WalkDir(depDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			candidates = append(candidates, referencedExistingPathsFromArtifact(path)...)
			candidates = append(candidates, path)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return appendUnique(nil, candidates...), nil
}

func referencedExistingPathsFromArtifact(path string) []string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > dependencyArtifactReferenceMaxBytes {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	candidates := []string{}
	text := string(content)
	for _, match := range artifactPathLinePattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		if resolved := normalizeArtifactPathReference(match[1]); resolved != "" {
			candidates = append(candidates, resolved)
		}
	}
	for _, match := range artifactJSONPathPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		if resolved := normalizeArtifactPathReference(match[1]); resolved != "" {
			candidates = append(candidates, resolved)
		}
	}
	return appendUnique(nil, candidates...)
}

func normalizeArtifactPathReference(raw string) string {
	candidate := strings.TrimSpace(raw)
	candidate = strings.Trim(candidate, `"'`)
	candidate = strings.TrimRight(candidate, ",")
	if candidate == "" || !filepath.IsAbs(candidate) {
		return ""
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return ""
	}
	return candidate
}

func bestArtifactCandidateForMissingPath(missingPath string, candidates []string, used map[string]struct{}) string {
	missingBase := strings.ToLower(strings.TrimSpace(filepath.Base(missingPath)))
	if missingBase == "" {
		return ""
	}

	for _, candidate := range candidates {
		if _, alreadyUsed := used[candidate]; alreadyUsed {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(filepath.Base(candidate)), missingBase) {
			return candidate
		}
	}

	missingTokens := tokenizeArtifactHint(missingBase)
	if len(missingTokens) == 0 {
		return ""
	}
	bestPath := ""
	bestScore := 0
	bestSpecificity := -1
	for _, candidate := range candidates {
		if _, alreadyUsed := used[candidate]; alreadyUsed {
			continue
		}
		candidateTokens := tokenizeArtifactHint(filepath.Base(candidate))
		if len(candidateTokens) == 0 {
			continue
		}
		score := artifactTokenMatchScore(missingTokens, candidateTokens)
		specificity := artifactCandidateSpecificity(candidate, candidateTokens)
		if score < bestScore {
			continue
		}
		if score == bestScore && specificity <= bestSpecificity {
			continue
		}
		bestScore = score
		bestSpecificity = specificity
		bestPath = candidate
	}
	if bestScore < 2 {
		return ""
	}
	return bestPath
}

func tokenizeArtifactHint(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if len(token) < 3 {
			continue
		}
		out = append(out, token)
	}
	return appendUnique(nil, out...)
}

func artifactTokenMatchScore(needles, haystack []string) int {
	score := 0
	for _, needle := range needles {
		bestTokenScore := 0
		for _, candidate := range haystack {
			if needle == candidate {
				if isGenericArtifactHintToken(needle) {
					bestTokenScore = max(bestTokenScore, 1)
				} else {
					bestTokenScore = max(bestTokenScore, 4)
				}
				continue
			}
			if commonPrefixLen(needle, candidate) >= 4 {
				if isGenericArtifactHintToken(needle) || isGenericArtifactHintToken(candidate) {
					bestTokenScore = max(bestTokenScore, 1)
				} else {
					bestTokenScore = max(bestTokenScore, 2)
				}
			}
		}
		score += bestTokenScore
	}
	score += artifactSemanticOverlapScore(needles, haystack)
	return score
}

func artifactCandidateSpecificity(path string, tokens []string) int {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	score := 0
	for _, token := range tokens {
		if isGenericArtifactHintToken(token) {
			continue
		}
		score++
	}
	if strings.HasPrefix(base, "worker-") && strings.HasSuffix(base, ".log") {
		score -= 2
	}
	return score
}

func isGenericArtifactHintToken(token string) bool {
	_, ok := genericArtifactHintTokens[strings.ToLower(strings.TrimSpace(token))]
	return ok
}

func artifactSemanticOverlapScore(needles, haystack []string) int {
	needleGroups := semanticGroupsForTokens(needles)
	if len(needleGroups) == 0 {
		return 0
	}
	candidateGroups := semanticGroupsForTokens(haystack)
	if len(candidateGroups) == 0 {
		return 0
	}
	score := 0
	for group := range needleGroups {
		if _, ok := candidateGroups[group]; !ok {
			continue
		}
		if group == "vuln" {
			score += 2
			continue
		}
		if group == "scan" {
			score += 2
			continue
		}
		score++
	}
	return score
}

func semanticGroupsForTokens(tokens []string) map[string]struct{} {
	if len(tokens) == 0 {
		return nil
	}
	groups := map[string]struct{}{}
	for _, token := range tokens {
		group := semanticGroupForToken(token)
		if group == "" {
			continue
		}
		groups[group] = struct{}{}
	}
	if len(groups) == 0 {
		return nil
	}
	return groups
}

func semanticGroupForToken(token string) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "scan", "service", "version", "port", "host", "nmap", "inventory", "discovery", "enum", "enumeration":
		return "scan"
	case "vuln", "vulnerability", "cve", "exploit", "misconfig", "misconfiguration", "exposure":
		return "vuln"
	case "report", "owasp", "finding", "evidence", "remediation", "summary":
		return "report"
	default:
		return ""
	}
}

func commonPrefixLen(a, b string) int {
	maxLen := min(len(a), len(b))
	count := 0
	for i := 0; i < maxLen; i++ {
		if a[i] != b[i] {
			break
		}
		count++
	}
	return count
}

func shouldRetryNmapForHostTimeout(command string, args []string, output []byte, runErr error) bool {
	if runErr != nil {
		return false
	}
	if !isNmapCommand(command) || !nmapRequiresActionableEvidence(args) {
		return false
	}
	return nmapOutputIndicatesHostTimeout(output)
}

func buildNmapEvidenceRetryArgs(args []string, remaining time.Duration) ([]string, string, bool) {
	if !nmapRequiresActionableEvidence(args) {
		return args, "", false
	}
	profile := selectNmapGuardrailProfile(args)
	targetHostTimeout := "75s"
	targetMaxRetries := "2"
	targetMaxRate := "900"
	if profile.name == "vuln_mapping" {
		targetHostTimeout = "120s"
		targetMaxRetries = "3"
		targetMaxRate = "600"
	}
	if bounded := fitRetryHostTimeoutToRemaining(targetHostTimeout, remaining); bounded != "" {
		targetHostTimeout = bounded
	} else if remaining > 0 {
		return args, "", false
	}
	next := setNmapOptionValue(args, "--host-timeout", targetHostTimeout)
	next = setNmapOptionValue(next, "--max-retries", targetMaxRetries)
	next = setNmapOptionValue(next, "--max-rate", targetMaxRate)
	if stringSlicesEqual(next, args) {
		return args, "", false
	}
	note := fmt.Sprintf("nmap host-timeout detected; retrying once with relaxed evidence profile (--host-timeout %s --max-retries %s --max-rate %s)", targetHostTimeout, targetMaxRetries, targetMaxRate)
	return next, note, true
}

func remainingContextDuration(ctx context.Context) time.Duration {
	if ctx == nil {
		return 0
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	return time.Until(deadline)
}

func fitRetryHostTimeoutToRemaining(target string, remaining time.Duration) string {
	if remaining <= 0 {
		return target
	}
	targetDur, err := time.ParseDuration(strings.TrimSpace(target))
	if err != nil || targetDur <= 0 {
		return target
	}
	// Reserve budget for process startup/teardown and result handling.
	available := remaining - 20*time.Second
	if available < 15*time.Second {
		return ""
	}
	if targetDur > available {
		targetDur = available
	}
	targetDur = targetDur.Round(time.Second)
	if targetDur < 15*time.Second {
		return ""
	}
	return targetDur.String()
}

func setNmapOptionValue(args []string, option, value string) []string {
	option = strings.ToLower(strings.TrimSpace(option))
	if option == "" || strings.TrimSpace(value) == "" {
		return append([]string{}, args...)
	}
	out := make([]string, 0, len(args)+2)
	set := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		if lower == option {
			if !set {
				out = append(out, arg)
				out = append(out, value)
				set = true
			}
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(lower, option+"=") {
			if !set {
				out = append(out, option+"="+value)
				set = true
			}
			continue
		}
		out = append(out, arg)
	}
	if !set {
		out = append(out, option, value)
	}
	return out
}

func mergeRetryOutput(first, second []byte, note string) []byte {
	const divider = "\n--- nmap retry ---\n"
	out := append([]byte{}, first...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	if strings.TrimSpace(note) != "" {
		out = append(out, []byte(note)...)
		out = append(out, '\n')
	}
	out = append(out, []byte(divider)...)
	out = append(out, second...)
	return out
}

func validateCommandOutputEvidence(command string, args []string, output []byte) error {
	if !isNmapCommand(command) || !nmapRequiresActionableEvidence(args) {
		return nil
	}
	if nmapOutputIndicatesHostTimeout(output) && !nmapOutputContainsActionableEvidence(output) {
		return fmt.Errorf("nmap output indicates host timeout without actionable service/vulnerability evidence")
	}
	return nil
}

func nmapRequiresActionableEvidence(args []string) bool {
	return nmapArgsSuggestServiceEnumeration(args) || nmapArgsContainScriptToken(args, "vuln")
}

func nmapOutputIndicatesHostTimeout(output []byte) bool {
	text := strings.ToLower(string(output))
	return strings.Contains(text, "skipping host") && strings.Contains(text, "due to host timeout")
}

var (
	nmapPortHeaderPattern = regexp.MustCompile(`(?m)^PORT\s+STATE\s+SERVICE`)
	nmapPortLinePattern   = regexp.MustCompile(`(?m)^\d+/(tcp|udp)\s+\S+`)
	nmapAllPortsPattern   = regexp.MustCompile(`(?m)all\s+\d+\s+scanned ports`)
	nmapCVEPattern        = regexp.MustCompile(`(?i)CVE-\d{4}-\d{4,}`)
)

func nmapOutputContainsActionableEvidence(output []byte) bool {
	text := string(output)
	return nmapPortHeaderPattern.MatchString(text) ||
		nmapPortLinePattern.MatchString(text) ||
		nmapAllPortsPattern.MatchString(strings.ToLower(text)) ||
		nmapCVEPattern.MatchString(text)
}

func adaptWeakVulnerabilityAction(task TaskSpec, scopePolicy *ScopePolicy, command string, args []string) (string, []string, string, bool) {
	if taskRequiresReportSynthesis(task) {
		return command, args, "", false
	}
	if !taskRequiresVulnerabilityEvidence(task) {
		return command, args, "", false
	}
	if !isWeakVulnerabilityMappingCommand(command, args) {
		return command, args, "", false
	}
	target := firstTaskTarget(task.Targets)
	if target == "" && scopePolicy != nil {
		target = scopePolicy.FirstAllowedTarget()
	}
	if strings.TrimSpace(target) == "" {
		return command, args, "", false
	}
	nextCommand := "nmap"
	nextArgs := []string{
		"--script", "vuln and safe",
		"--script-timeout", "20s",
		"-sV",
		"--version-light",
		"--top-ports", "20",
		"-Pn",
		target,
	}
	note := fmt.Sprintf("rewrote weak vulnerability-mapping command (%s) to concrete nmap vuln-mapping action against %s", command, target)
	return nextCommand, nextArgs, note, true
}

func isWeakVulnerabilityMappingCommand(command string, args []string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "nmap", "searchsploit", "msfconsole", "metasploit", "nikto", "nuclei":
		return false
	}
	switch base {
	case "cat", "echo", "printf", "true", "false":
		return true
	case "python", "python3":
		return true
	case "bash", "sh", "zsh":
		return true
	}
	return false
}

func isWeakPythonVulnMappingArgs(args []string) bool {
	code := inlinePythonCodeArg(args)
	if code == "" {
		return false
	}
	lower := strings.ToLower(code)
	if !strings.Contains(lower, "print(") {
		return false
	}
	if strings.Contains(lower, "subprocess") || strings.Contains(lower, "nmap") || strings.Contains(lower, "searchsploit") || strings.Contains(lower, "metasploit") {
		return false
	}
	return true
}

func inlinePythonCodeArg(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "-c" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
	}
	return ""
}

func isWeakShellVulnMappingArgs(args []string) bool {
	if len(args) < 2 {
		return false
	}
	if strings.TrimSpace(args[0]) != "-c" && strings.TrimSpace(args[0]) != "-lc" {
		return false
	}
	body := strings.ToLower(strings.TrimSpace(args[1]))
	if body == "" {
		return false
	}
	if containsAnySubstring(body, "nmap ", "searchsploit", "msfconsole", "metasploit", "nikto", "nuclei") {
		return false
	}
	return strings.Contains(body, "cat ") || strings.Contains(body, "echo ") || strings.Contains(body, "printf ")
}

func validateVulnerabilityEvidenceAuthenticity(task TaskSpec, command string, args []string, output []byte) error {
	if !taskRequiresVulnerabilityEvidence(task) {
		return nil
	}
	if !isLikelyPlaceholderVulnOutput(command, args, output) {
		return nil
	}
	return fmt.Errorf("vulnerability mapping output appears to be placeholder/example text without tool-backed evidence")
}

func ensureVulnerabilityEvidenceAction(task TaskSpec, command string, args []string) ([]string, string, bool) {
	return ensureVulnerabilityEvidenceActionWithGoal(task, "", command, args)
}

func ensureVulnerabilityEvidenceActionWithGoal(task TaskSpec, goal, command string, args []string) ([]string, string, bool) {
	if taskRequiresReportSynthesis(task) {
		return args, "", false
	}
	needsEvidence := taskRequiresVulnerabilityEvidence(task)
	if !needsEvidence && goalRequiresVulnerabilityEvidence(goal) && taskCanCarryVulnerabilityEvidence(task, command, args) {
		needsEvidence = true
	}
	if !needsEvidence {
		return args, "", false
	}
	if nextArgs, note, rewritten := ensureVulnerabilityEvidenceForWrappedNmap(command, args); rewritten {
		if !taskRequiresVulnerabilityEvidence(task) {
			note += " (goal-context trigger)"
		}
		return nextArgs, note, true
	}
	if !isNmapCommand(command) {
		return args, "", false
	}
	if nmapArgsContainScriptToken(args, "vuln") {
		return args, "", false
	}
	nextArgs := append([]string{}, args...)
	nextArgs = setNmapOptionValue(nextArgs, "--script", "vuln and safe")
	nextArgs = setNmapOptionValue(nextArgs, "--script-timeout", "20s")
	note := "enforced vulnerability evidence profile: set --script \"vuln and safe\" and --script-timeout 20s for CVE-capable output"
	if !taskRequiresVulnerabilityEvidence(task) {
		note += " (goal-context trigger)"
	}
	return nextArgs, note, true
}

func goalRequiresVulnerabilityEvidence(goal string) bool {
	text := strings.ToLower(strings.TrimSpace(goal))
	if text == "" {
		return false
	}
	return strings.Contains(text, "vulnerab") ||
		strings.Contains(text, "cve") ||
		strings.Contains(text, "owasp") ||
		strings.Contains(text, "security scan") ||
		strings.Contains(text, "security assessment")
}

func taskCanCarryVulnerabilityEvidence(task TaskSpec, command string, args []string) bool {
	if !isNmapCommand(command) && !isShellWrapperWithNmapCommand(command, args) {
		return false
	}
	if hasNmapFlag(args, "-sn") {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		strings.Join(task.ExpectedArtifacts, " "),
	}, " ")))
	return strings.Contains(text, "service") ||
		strings.Contains(text, "scan") ||
		strings.Contains(text, "recon") ||
		strings.Contains(text, "enum") ||
		strings.Contains(text, "port")
}

func isShellWrapperWithNmapCommand(command string, args []string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if base != "bash" && base != "sh" && base != "zsh" {
		return false
	}
	if len(args) < 2 {
		return false
	}
	mode := strings.TrimSpace(args[0])
	if mode != "-c" && mode != "-lc" {
		return false
	}
	return nmapCommandTokenPattern.MatchString(args[1])
}

func ensureVulnerabilityEvidenceForWrappedNmap(command string, args []string) ([]string, string, bool) {
	if !isShellWrapperWithNmapCommand(command, args) {
		return args, "", false
	}
	script := args[1]
	lower := strings.ToLower(script)
	if strings.Contains(lower, "-sn") {
		return args, "", false
	}
	if strings.Contains(lower, "--script") {
		if strings.Contains(lower, "vuln") && strings.Contains(lower, "--script-timeout") {
			return args, "", false
		}
		// Avoid mutating complex explicit script selections in shell bodies.
		return args, "", false
	}
	loc := nmapCommandTokenPattern.FindStringSubmatchIndex(script)
	if len(loc) < 6 {
		return args, "", false
	}
	cmdStart := loc[3]
	cmdEnd := loc[4]
	rewritten := script[:cmdStart] + `nmap --script "vuln and safe" --script-timeout 20s` + script[cmdEnd:]
	if rewritten == script {
		return args, "", false
	}
	next := append([]string{}, args...)
	next[1] = rewritten
	note := `enforced vulnerability evidence profile in shell wrapper: inserted --script "vuln and safe" and --script-timeout 20s for nmap`
	return next, note, true
}

func taskRequiresVulnerabilityEvidence(task TaskSpec) bool {
	fields := []string{
		task.Title,
		task.Goal,
		strings.Join(task.DoneWhen, " "),
		strings.Join(task.ExpectedArtifacts, " "),
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join(fields, " ")))
	if text == "" {
		return false
	}
	return strings.Contains(text, "vulnerab") ||
		strings.Contains(text, "cve") ||
		strings.Contains(text, "exploit") ||
		strings.Contains(text, "weakness")
}

func isLikelyPlaceholderVulnOutput(command string, args []string, output []byte) bool {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return true
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "example:") {
		return false
	}
	if !nmapCVEPattern.MatchString(text) {
		return false
	}
	if containsAnySubstring(lower,
		"nmap scan report",
		"searchsploit",
		"metasploit",
		"nse:",
		"vulners",
		"osv",
	) {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "python", "python3", "bash", "sh", "zsh":
		return true
	default:
		if base == "" {
			return false
		}
	}
	if len(args) == 0 {
		return false
	}
	lineCount := len(strings.Split(text, "\n"))
	return lineCount <= 12
}

func containsAnySubstring(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(needle))) {
			return true
		}
	}
	return false
}

func requiresCommandScopeValidation(command string, args []string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if base != "bash" && base != "sh" && base != "zsh" {
		return true
	}
	if len(args) < 2 {
		return true
	}
	mode := strings.TrimSpace(args[0])
	if mode != "-c" && mode != "-lc" {
		return true
	}
	body := strings.TrimSpace(args[1])
	if body == "" {
		return true
	}
	// Conservative fail-closed: uncertain shell expansions still require scope validation.
	if strings.Contains(body, "$(") || strings.Contains(body, "`") {
		return true
	}
	for _, token := range shellCommandTokens(body) {
		if isNetworkSensitiveCommand(token) {
			return true
		}
	}
	return false
}

func shellCommandTokens(body string) []string {
	segments := strings.FieldsFunc(body, func(r rune) bool {
		switch r {
		case '|', ';', '&', '\n':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		fields := strings.Fields(strings.TrimSpace(segment))
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields[0])
	}
	return out
}

func sanitizePathComponent(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return "worker"
	}
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

func WorkerSignalID(workerID string) string {
	return "signal-" + workerID
}

func emitWorkerFailure(manager *Manager, cfg WorkerRunConfig, task TaskSpec, cause error, reason string, extra map[string]any) error {
	payload := map[string]any{
		"attempt":   cfg.Attempt,
		"worker_id": cfg.WorkerID,
		"reason":    reason,
	}
	if cause != nil {
		payload["error"] = cause.Error()
	}
	for key, value := range extra {
		payload[key] = value
	}
	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFinding, map[string]any{
		"target":       primaryTaskTarget(task),
		"finding_type": "task_execution_failure",
		"title":        "task action failed",
		"state":        FindingStateVerified,
		"severity":     "low",
		"confidence":   "high",
		"source":       "worker_runtime",
		"metadata": map[string]any{
			"attempt": cfg.Attempt,
			"reason":  reason,
		},
	})
	return manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFailed, payload)
}

func runErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func runWorkerCommand(ctx context.Context, cmd *exec.Cmd, grace time.Duration) ([]byte, error) {
	if cmd == nil {
		return nil, fmt.Errorf("nil command")
	}
	configureWorkerCommandProcess(cmd)
	output := newCappedOutputBuffer(workerCommandOutputMaxBytes)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return output.Bytes(), err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return output.Bytes(), err
	case <-ctx.Done():
		terminateWorkerCommandProcess(cmd, grace)
		select {
		case <-done:
		case <-time.After(grace):
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return output.Bytes(), errWorkerCommandTimeout
		}
		return output.Bytes(), errWorkerCommandInterrupted
	}
}

type cappedOutputBuffer struct {
	data      []byte
	max       int
	truncated bool
}

func newCappedOutputBuffer(max int) *cappedOutputBuffer {
	if max <= 0 {
		max = workerCommandOutputMaxBytes
	}
	return &cappedOutputBuffer{
		data: make([]byte, 0, min(4096, max)),
		max:  max,
	}
}

func (c *cappedOutputBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.data) < c.max {
		remaining := c.max - len(c.data)
		if len(p) <= remaining {
			c.data = append(c.data, p...)
		} else {
			c.data = append(c.data, p[:remaining]...)
			c.truncated = true
		}
	} else {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedOutputBuffer) Bytes() []byte {
	out := append([]byte(nil), c.data...)
	if !c.truncated {
		return out
	}
	remaining := c.max - len(out)
	if remaining <= 0 {
		return out
	}
	note := []byte(workerCommandTruncationNote)
	if len(note) > remaining {
		note = note[:remaining]
	}
	return append(out, note...)
}

func enforceWorkerRiskPolicy(task TaskSpec, cfg WorkerRunConfig) (string, error) {
	broker, err := NewApprovalBroker(cfg.Permission, cfg.Disruptive, time.Hour)
	if err != nil {
		return WorkerFailurePolicyInvalid, fmt.Errorf("%s: %w", WorkerFailurePolicyInvalid, err)
	}
	decision, _, tier, err := broker.EvaluateTask(task, time.Now().UTC())
	if err != nil {
		return WorkerFailurePolicyInvalid, fmt.Errorf("%s: %w", WorkerFailurePolicyInvalid, err)
	}
	if decision == ApprovalDeny {
		return WorkerFailurePolicyDenied, fmt.Errorf("%s: mode=%s tier=%s", WorkerFailurePolicyDenied, cfg.Permission, tier)
	}
	return "", nil
}

func workerAttemptAlreadyCompleted(manager *Manager, cfg WorkerRunConfig) (bool, error) {
	if _, err := os.Stat(manager.eventPath(cfg.RunID)); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	events, err := manager.Events(cfg.RunID, 0)
	if err != nil {
		return false, err
	}
	workerID := WorkerSignalID(cfg.WorkerID)
	for _, event := range events {
		if event.TaskID != cfg.TaskID || event.WorkerID != workerID || event.Type != EventTypeTaskCompleted {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if attempt, ok := payloadInt(payload["attempt"]); ok && attempt == cfg.Attempt {
			return true, nil
		}
	}
	return false, nil
}

func primaryTaskTarget(task TaskSpec) string {
	if len(task.Targets) > 0 && strings.TrimSpace(task.Targets[0]) != "" {
		return strings.TrimSpace(task.Targets[0])
	}
	return "local"
}

func EmitWorkerBootstrapFailure(cfg WorkerRunConfig, cause error) error {
	if strings.TrimSpace(cfg.SessionsDir) == "" || strings.TrimSpace(cfg.RunID) == "" || strings.TrimSpace(cfg.TaskID) == "" || strings.TrimSpace(cfg.WorkerID) == "" {
		return nil
	}
	manager := NewManager(cfg.SessionsDir)
	recorded, err := workerFailureAlreadyRecorded(manager, cfg)
	if err == nil && recorded {
		return nil
	}
	payload := map[string]any{
		"attempt":   cfg.Attempt,
		"worker_id": cfg.WorkerID,
		"reason":    WorkerFailureBootstrapFailed,
	}
	if cause != nil {
		payload["error"] = cause.Error()
	}
	return manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFailed, payload)
}

func workerFailureAlreadyRecorded(manager *Manager, cfg WorkerRunConfig) (bool, error) {
	events, err := manager.Events(cfg.RunID, 0)
	if err != nil {
		return false, err
	}
	workerID := WorkerSignalID(cfg.WorkerID)
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.TaskID != cfg.TaskID || event.WorkerID != workerID || event.Type != EventTypeTaskFailed {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		attempt, ok := payloadInt(payload["attempt"])
		if !ok || attempt == cfg.Attempt {
			return true, nil
		}
	}
	return false, nil
}
