package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
)

const (
	WorkerFailureInvalidTaskAction  = "invalid_task_action"
	WorkerFailureWorkspaceCreate    = "workspace_create_failed"
	WorkerFailureInvalidWorkingDir  = "invalid_working_dir"
	WorkerFailureWorkingDirCreate   = "working_dir_create_failed"
	WorkerFailureArtifactWrite      = "artifact_write_failed"
	WorkerFailureCommandFailed      = "command_failed"
	WorkerFailureCommandTimeout     = "command_timeout"
	WorkerFailureCommandInterrupted = "command_interrupted"
	WorkerFailureScopeDenied        = "scope_denied"
	WorkerFailurePolicyDenied       = "policy_denied"
	WorkerFailurePolicyInvalid      = "policy_invalid"
	WorkerFailureBootstrapFailed    = "worker_bootstrap_failed"
	WorkerFailureAssistUnavailable  = "assist_unavailable"
	WorkerFailureAssistTimeout      = "assist_timeout"
	WorkerFailureAssistNeedsInput   = "assist_needs_input"
	WorkerFailureAssistNoAction     = "assist_no_action"
	WorkerFailureAssistLoopDetected = "assist_loop_detected"
	WorkerFailureAssistExhausted    = "assist_budget_exhausted"
)

var (
	errWorkerCommandTimeout     = errors.New("worker command timeout")
	errWorkerCommandInterrupted = errors.New("worker command interrupted")
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
	if action.Type != "assist" {
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
	}
	if err := scopePolicy.ValidateCommandTargets(action.Command, action.Args); err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureScopeDenied, nil)
		return err
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
		if err := scopePolicy.ValidateCommandTargets(action.Command, action.Args); err != nil {
			_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureScopeDenied, nil)
			return err
		}
	}

	timeout := resolveActionTimeout(task.Budget.MaxRuntime, action.TimeoutSeconds)
	signalCtx, stopSignals := withWorkerSignalContext(context.Background())
	defer stopSignals()
	ctx, cancel := context.WithTimeout(signalCtx, timeout)
	defer cancel()

	if action.Type == "assist" {
		return runWorkerAssistTask(ctx, manager, cfg, task, action, scopePolicy, workDir)
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

	cmd := exec.Command(action.Command, action.Args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	output, runErr := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
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
		"command":   action.Command,
		"args":      action.Args,
		"exit_code": exitCode,
	})

	if runErr != nil {
		reason := WorkerFailureCommandFailed
		if errors.Is(runErr, errWorkerCommandTimeout) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = WorkerFailureCommandTimeout
		} else if errors.Is(runErr, errWorkerCommandInterrupted) {
			reason = WorkerFailureCommandInterrupted
		}
		_ = emitWorkerFailure(manager, cfg, task, runErr, reason, map[string]any{
			"command":   action.Command,
			"args":      action.Args,
			"log_path":  logPath,
			"exit_code": exitCode,
		})
		return runErr
	}
	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskFinding, map[string]any{
		"target":       primaryTaskTarget(task),
		"finding_type": "task_execution_result",
		"title":        "task action completed",
		"severity":     "info",
		"confidence":   "high",
		"source":       "worker_runtime",
		"evidence":     []string{logPath},
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
			"produced_artifacts":  []string{logPath},
			"required_findings":   []string{"task_execution_result"},
			"produced_findings":   []string{"task_execution_result"},
			"verification_status": "satisfied",
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
