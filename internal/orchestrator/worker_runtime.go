package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	OrchDiagnosticEnv  = "BIRDHACKBOT_ORCH_DIAGNOSTIC_MODE"
	OrchApprovalEnv    = "BIRDHACKBOT_ORCH_APPROVAL_TIMEOUT"
	OrchToolInstallEnv = "BIRDHACKBOT_ORCH_TOOL_INSTALL_POLICY"
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
	WorkerFailureNoProgress           = "no_progress"
)

var (
	errWorkerCommandTimeout      = errors.New("worker command timeout")
	errWorkerCommandInterrupted  = errors.New("worker command interrupted")
	shellPathArgPattern          = regexp.MustCompile(`(?:\.\./|\./|/)[^\s"'` + "`" + `|&;<>]+`)
	artifactPathLinePattern      = regexp.MustCompile(`(?mi)^\s*path:\s*(.+?)\s*$`)
	artifactJSONPathPattern      = regexp.MustCompile(`(?mi)"path"\s*:\s*"([^"]+)"`)
	archiveNamePattern           = regexp.MustCompile(`[a-z0-9._-]+\.zip`)
	nmapCommandTokenPattern      = regexp.MustCompile(`(?i)(^|[\s;|&()])nmap([\s])`)
	unzipPasswordPipelinePattern = regexp.MustCompile(`unzip\s+-P\s+\$\([^)]*john_output[^)]*\)`)
	genericArtifactHintTokens    = map[string]struct{}{
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
	SessionsDir       string
	RunID             string
	TaskID            string
	WorkerID          string
	Attempt           int
	Permission        PermissionMode
	Disruptive        bool
	Diagnostic        bool
	ApprovalTimeout   time.Duration
	ToolInstallPolicy ToolInstallPolicy
	assistantBuilder  func() (string, string, workerAssistant, error)
}

func ParseWorkerRunConfig(getenv func(string) string) WorkerRunConfig {
	attempt, _ := strconv.Atoi(strings.TrimSpace(getenv(OrchAttemptEnv)))
	disruptive, _ := strconv.ParseBool(strings.TrimSpace(getenv(OrchDisruptiveEnv)))
	diagnostic, _ := strconv.ParseBool(strings.TrimSpace(getenv(OrchDiagnosticEnv)))
	mode := PermissionMode(strings.TrimSpace(getenv(OrchPermissionEnv)))
	if mode == "" {
		mode = PermissionDefault
	}
	approvalTimeout := 2 * time.Minute
	if raw := strings.TrimSpace(getenv(OrchApprovalEnv)); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			approvalTimeout = parsed
		}
	}
	toolInstallPolicy := parseToolInstallPolicy(getenv(OrchToolInstallEnv))
	return WorkerRunConfig{
		SessionsDir:       strings.TrimSpace(getenv(OrchSessionsDirEnv)),
		RunID:             strings.TrimSpace(getenv(OrchRunIDEnv)),
		TaskID:            strings.TrimSpace(getenv(OrchTaskIDEnv)),
		WorkerID:          strings.TrimSpace(getenv(OrchWorkerIDEnv)),
		Attempt:           attempt,
		Permission:        mode,
		Disruptive:        disruptive,
		Diagnostic:        diagnostic,
		ApprovalTimeout:   approvalTimeout,
		ToolInstallPolicy: toolInstallPolicy,
	}
}

func (cfg WorkerRunConfig) resolveAssistantBuilder() func() (string, string, workerAssistant, error) {
	if cfg.assistantBuilder != nil {
		return cfg.assistantBuilder
	}
	return buildWorkerAssistant
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
	if cfg.ApprovalTimeout <= 0 {
		cfg.ApprovalTimeout = 2 * time.Minute
	}
	if cfg.ToolInstallPolicy == "" {
		cfg.ToolInstallPolicy = ToolInstallPolicyAsk
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
	if action.Type != "assist" && !cfg.Diagnostic {
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
		prepared := prepareRuntimeCommand(scopePolicy, task, action.Command, action.Args)
		action.Command = prepared.Command
		action.Args = prepared.Args
		emitRuntimePreparationProgress(
			manager,
			cfg.RunID,
			signalWorkerID,
			cfg.TaskID,
			0,
			0,
			0,
			"tool_call",
			"",
			action.Command,
			action.Args,
			runtimePreparationMessages(prepared),
		)
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
	if cfg.Diagnostic {
		_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":   "diagnostic mode active: adaptive runtime rewrites disabled for this attempt",
			"step":      0,
			"tool_call": 0,
			"mode":      "diagnostic",
		})
	}

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
	if taskRequiresValidatorRole(task) {
		if !strings.EqualFold(strings.TrimSpace(task.RiskLevel), string(RiskReconReadonly)) {
			err := fmt.Errorf("validator role requires recon_readonly risk level, got %s", strings.TrimSpace(task.RiskLevel))
			_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailurePolicyInvalid, map[string]any{
				"detail": "validator_role_requires_read_only_risk",
			})
			return err
		}
		return runWorkerValidatorTask(manager, cfg, task, workDir)
	}
	if action.Type != "assist" && !cfg.Diagnostic {
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
		if adaptedCommand, adaptedArgs, adaptNotes, adapted, adaptErr := adaptArchiveWorkflowCommand(cfg, task, action.Command, action.Args, workDir); adaptErr != nil {
			_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":   fmt.Sprintf("archive workflow adaptation skipped: %v", adaptErr),
				"step":      0,
				"tool_call": 0,
				"command":   action.Command,
				"args":      action.Args,
			})
		} else if adapted {
			action.Command = adaptedCommand
			action.Args = adaptedArgs
			for _, note := range adaptNotes {
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
		result := executeWorkerAssistCommand(ctx, cfg, task, execCommand, execArgs, workDir)
		execCommand = strings.TrimSpace(result.command)
		execArgs = append([]string{}, result.args...)
		output = result.output
		runErr = result.runErr
	} else {
		cmd := exec.Command(execCommand, execArgs...)
		cmd.Dir = workDir
		commandEnv := os.Environ()
		if adaptedEnv, envNotes, envErr := applyArchiveToolRuntimeEnv(commandEnv, task, execCommand, workDir); envErr != nil {
			_ = emitWorkerFailure(manager, cfg, task, envErr, WorkerFailureBootstrapFailed, map[string]any{
				"command": execCommand,
				"args":    execArgs,
			})
			return envErr
		} else {
			commandEnv = adaptedEnv
			for _, note := range envNotes {
				if strings.TrimSpace(note) == "" {
					continue
				}
				_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":   note,
					"step":      0,
					"tool_call": 0,
					"command":   execCommand,
					"args":      execArgs,
				})
			}
		}
		cmd.Env = commandEnv
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
	if repairResult, repairErr := attemptCommandContractRepair(ctx, cfg, task, scopePolicy, workDir, execCommand, execArgs, output, runErr); repairErr != nil {
		_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":   fmt.Sprintf("command contract repair skipped: %v", repairErr),
			"step":      1,
			"tool_call": 2,
			"command":   execCommand,
			"args":      execArgs,
		})
	} else if repairResult.Used {
		_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":   strings.TrimSpace(repairResult.Note),
			"step":      1,
			"tool_call": 2,
			"command":   repairResult.Command,
			"args":      repairResult.Args,
		})
		output = mergeCommandContractRepairOutput(output, repairResult.Output, repairResult.Note)
		runErr = repairResult.RunErr
		execCommand = repairResult.Command
		execArgs = append([]string{}, repairResult.Args...)
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
	if evidenceErr := validateCommandOutputEvidence(task, execCommand, execArgs, output); evidenceErr != nil {
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
	supplementalArtifacts, supplementalErr := writeArchiveWorkflowSupplementalArtifacts(ctx, cfg, task, execCommand, execArgs, workDir)
	if supplementalErr != nil {
		_ = emitWorkerFailure(manager, cfg, task, supplementalErr, WorkerFailureArtifactWrite, map[string]any{
			"command":  execCommand,
			"args":     execArgs,
			"log_path": logPath,
		})
		return supplementalErr
	}
	for _, artifactPath := range supplementalArtifacts {
		producedArtifacts = appendUnique(producedArtifacts, artifactPath)
		_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskArtifact, map[string]any{
			"type":      "derived_command_output",
			"title":     fmt.Sprintf("supplemental archive artifact (%s)", cfg.TaskID),
			"path":      artifactPath,
			"command":   execCommand,
			"args":      execArgs,
			"exit_code": exitCode,
		})
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
	requiredArtifacts := requiredArtifactsForCompletion(task)

	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskCompleted, map[string]any{
		"attempt":   cfg.Attempt,
		"worker_id": cfg.WorkerID,
		"reason":    "action_completed",
		"log_path":  logPath,
		"completion_contract": map[string]any{
			"status_reason":       "action_completed",
			"required_artifacts":  requiredArtifacts,
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
		if len(content) == 0 && expectedArtifactRequiresConcreteSource(task, name) {
			continue
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

func expectedArtifactRequiresConcreteSource(task TaskSpec, artifactName string) bool {
	if !taskLikelyLocalFileWorkflow(task) {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(filepath.Base(artifactName)))
	return name == "password_found" || name == "password_found.txt" || name == "recovered_password.txt"
}

func requiredArtifactsForCompletion(task TaskSpec) []string {
	if len(task.ExpectedArtifacts) == 0 {
		return nil
	}
	required := make([]string, 0, len(task.ExpectedArtifacts))
	for _, expected := range task.ExpectedArtifacts {
		expected = strings.TrimSpace(expected)
		if expected == "" {
			continue
		}
		if expectedArtifactOptionalForCompletion(task, expected) {
			continue
		}
		required = append(required, expected)
	}
	return compactStringSlice(required)
}

func expectedArtifactOptionalForCompletion(task TaskSpec, artifactName string) bool {
	if !taskLikelyLocalFileWorkflow(task) {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(filepath.Base(artifactName)))
	return name == "password_found" || name == "password_found.txt" || name == "recovered_password.txt"
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
