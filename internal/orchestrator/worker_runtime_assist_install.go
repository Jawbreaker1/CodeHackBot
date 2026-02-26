package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type ToolInstallPolicy string

const (
	ToolInstallPolicyAsk   ToolInstallPolicy = "ask"
	ToolInstallPolicyNever ToolInstallPolicy = "never"
	ToolInstallPolicyAuto  ToolInstallPolicy = "auto"
)

var (
	missingExecFromErrPattern     = regexp.MustCompile(`exec:\s*"([^"]+)"`)
	shellCommandNotFoundPattern   = regexp.MustCompile(`(?m)(?:^|:\s*)([A-Za-z0-9._+-]+):\s+command not found\b`)
	workerInstallSafeTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)
)

type missingToolRemediation struct {
	Handled     bool
	RecoverHint string
	Observation string
	ToolName    string
}

func parseToolInstallPolicy(raw string) ToolInstallPolicy {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ToolInstallPolicyAsk):
		return ToolInstallPolicyAsk
	case string(ToolInstallPolicyNever):
		return ToolInstallPolicyNever
	case string(ToolInstallPolicyAuto):
		return ToolInstallPolicyAuto
	default:
		return ToolInstallPolicyAsk
	}
}

func ParseToolInstallPolicy(raw string) (ToolInstallPolicy, error) {
	policy := strings.ToLower(strings.TrimSpace(raw))
	switch policy {
	case "", string(ToolInstallPolicyAsk):
		return ToolInstallPolicyAsk, nil
	case string(ToolInstallPolicyNever):
		return ToolInstallPolicyNever, nil
	case string(ToolInstallPolicyAuto):
		return ToolInstallPolicyAuto, nil
	default:
		return "", fmt.Errorf("invalid tool install policy %q", raw)
	}
}

func detectMissingExecutable(command string, runErr error, output []byte) string {
	if runErr == nil {
		return ""
	}
	var execErr *exec.Error
	if errors.As(runErr, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		if name := normalizeToolToken(execErr.Name); name != "" {
			return name
		}
	}
	lowerErr := strings.ToLower(strings.TrimSpace(runErr.Error()))
	if strings.Contains(lowerErr, "executable file not found") || strings.Contains(lowerErr, "command not found") {
		if matches := missingExecFromErrPattern.FindStringSubmatch(runErr.Error()); len(matches) > 1 {
			if name := normalizeToolToken(matches[1]); name != "" {
				return name
			}
		}
	}
	if matches := shellCommandNotFoundPattern.FindStringSubmatch(string(output)); len(matches) > 1 {
		if name := normalizeToolToken(matches[1]); name != "" {
			return name
		}
	}
	if isShellCommand(command) && commandExitCode(runErr) == 127 {
		if matches := shellCommandNotFoundPattern.FindStringSubmatch(string(output)); len(matches) > 1 {
			if name := normalizeToolToken(matches[1]); name != "" {
				return name
			}
		}
	}
	return ""
}

func isShellCommand(command string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	return base == "bash" || base == "sh" || base == "zsh"
}

func normalizeToolToken(raw string) string {
	token := strings.TrimSpace(raw)
	token = strings.Trim(token, "\"'`")
	if token == "" {
		return ""
	}
	token = filepath.Base(token)
	if token == "." || token == ".." {
		return ""
	}
	if !workerInstallSafeTokenPattern.MatchString(token) {
		return ""
	}
	return strings.ToLower(token)
}

func handleMissingToolRemediation(ctx context.Context, manager *Manager, cfg WorkerRunConfig, task TaskSpec, workDir string, step, turn, toolCalls int, command string, args []string, missingTool string, attempted map[string]struct{}) missingToolRemediation {
	tool := normalizeToolToken(missingTool)
	if tool == "" {
		return missingToolRemediation{}
	}
	key := strings.ToLower(tool)
	if _, seen := attempted[key]; seen {
		msg := fmt.Sprintf("tool %s remains unavailable after a previous install attempt; pivot to available tools (%s)", tool, discoverAvailableFallbackTools())
		return missingToolRemediation{
			Handled:     true,
			ToolName:    tool,
			Observation: "runtime remediation skipped: " + msg,
			RecoverHint: msg,
		}
	}
	attempted[key] = struct{}{}

	policy := cfg.ToolInstallPolicy
	if policy == "" {
		policy = ToolInstallPolicyAsk
	}
	if cfg.Diagnostic {
		policy = ToolInstallPolicyNever
	}
	installSpec, ok := buildToolInstallCommand(tool)
	if !ok {
		msg := fmt.Sprintf("tool %s is unavailable and no supported package manager command was found; pivot using available tools (%s)", tool, discoverAvailableFallbackTools())
		return missingToolRemediation{
			Handled:     true,
			ToolName:    tool,
			Observation: "runtime remediation blocked: " + msg,
			RecoverHint: msg,
		}
	}
	if policy == ToolInstallPolicyNever {
		msg := fmt.Sprintf("tool %s unavailable and install policy is never; pivot using available tools (%s)", tool, discoverAvailableFallbackTools())
		return missingToolRemediation{
			Handled:     true,
			ToolName:    tool,
			Observation: "runtime remediation skipped: " + msg,
			RecoverHint: msg,
		}
	}

	approved := policy == ToolInstallPolicyAuto
	approvalID := "apr-tool-install-" + NewEventID()
	expiresAt := manager.Now().Add(cfg.ApprovalTimeout)
	commandLabel := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	if commandLabel == "" {
		commandLabel = "(unknown command)"
	}
	installLabel := strings.TrimSpace(strings.Join(append([]string{installSpec.Command}, installSpec.Args...), " "))
	if installLabel == "" {
		installLabel = installSpec.Command
	}
	if !approved {
		reason := fmt.Sprintf("missing tool %s for command %q; approve install command: %s", tool, commandLabel, installLabel)
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeApprovalRequested, map[string]any{
			"approval_id": approvalID,
			"tier":        "tool_install",
			"reason":      reason,
			"expires_at":  expiresAt.Format(time.RFC3339),
			"tool":        tool,
		})
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":           fmt.Sprintf("missing tool %s; install approval requested", tool),
			"step":              step,
			"turn":              turn,
			"tool_calls":        toolCalls,
			"mode":              "recover",
			"approval_id":       approvalID,
			"approval_reason":   reason,
			"approval_expires":  expiresAt.Format(time.RFC3339),
			"approve_hint":      fmt.Sprintf("birdhackbot-orchestrator approve --sessions-dir %s --run %s --approval %s --scope task --reason \"authorized\"", cfg.SessionsDir, cfg.RunID, approvalID),
			"deny_hint":         fmt.Sprintf("birdhackbot-orchestrator deny --sessions-dir %s --run %s --approval %s --reason \"not authorized\"", cfg.SessionsDir, cfg.RunID, approvalID),
			"install_candidate": installLabel,
		})
		decision, decisionReason := waitForInstallApprovalDecision(ctx, manager, cfg.RunID, approvalID, cfg.ApprovalTimeout)
		switch decision {
		case "approved":
			approved = true
		case "denied":
			msg := fmt.Sprintf("install denied for missing tool %s (%s); pivot using available tools (%s)", tool, decisionReason, discoverAvailableFallbackTools())
			return missingToolRemediation{
				Handled:     true,
				ToolName:    tool,
				Observation: "runtime remediation denied: " + msg,
				RecoverHint: msg,
			}
		default:
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeApprovalExpired, map[string]any{
				"approval_id": approvalID,
				"tier":        "tool_install",
				"reason":      "approval_timeout",
			})
			msg := fmt.Sprintf("install approval timed out for missing tool %s; pivot using available tools (%s)", tool, discoverAvailableFallbackTools())
			return missingToolRemediation{
				Handled:     true,
				ToolName:    tool,
				Observation: "runtime remediation timed out: " + msg,
				RecoverHint: msg,
			}
		}
	}

	if !approved {
		msg := fmt.Sprintf("tool %s unavailable and install not approved; pivot using available tools (%s)", tool, discoverAvailableFallbackTools())
		return missingToolRemediation{
			Handled:     true,
			ToolName:    tool,
			Observation: "runtime remediation blocked: " + msg,
			RecoverHint: msg,
		}
	}

	_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
		"message":           fmt.Sprintf("installing missing tool %s using approved command", tool),
		"step":              step,
		"turn":              turn,
		"tool_calls":        toolCalls,
		"mode":              "recover",
		"install_candidate": installLabel,
		"approval_id":       approvalID,
	})
	installResult := executeWorkerAssistCommand(ctx, cfg, task, installSpec.Command, installSpec.Args, workDir)
	logPath, logErr := writeWorkerActionLog(cfg, fmt.Sprintf("%s-a%d-s%d-install-%s.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt, step, sanitizePathComponent(tool)), installResult.output)
	if logErr == nil {
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskArtifact, map[string]any{
			"type":      "runtime_install_log",
			"title":     fmt.Sprintf("runtime install output (%s)", cfg.TaskID),
			"path":      logPath,
			"command":   installResult.command,
			"args":      installResult.args,
			"step":      step,
			"turn":      turn,
			"tool_call": toolCalls,
			"exit_code": commandExitCode(installResult.runErr),
			"tool":      tool,
		})
	}
	if installResult.runErr != nil {
		msg := fmt.Sprintf("install failed for tool %s via %s; pivot using available tools (%s)", tool, installLabel, discoverAvailableFallbackTools())
		if strings.TrimSpace(logPath) != "" {
			msg += fmt.Sprintf("; see %s", logPath)
		}
		return missingToolRemediation{
			Handled:     true,
			ToolName:    tool,
			Observation: "runtime remediation failed: " + msg,
			RecoverHint: msg,
		}
	}
	if _, lookErr := exec.LookPath(tool); lookErr != nil {
		msg := fmt.Sprintf("install command completed but tool %s is still unavailable; pivot using available tools (%s)", tool, discoverAvailableFallbackTools())
		if strings.TrimSpace(logPath) != "" {
			msg += fmt.Sprintf("; see %s", logPath)
		}
		return missingToolRemediation{
			Handled:     true,
			ToolName:    tool,
			Observation: "runtime remediation incomplete: " + msg,
			RecoverHint: msg,
		}
	}
	msg := fmt.Sprintf("tool %s installed successfully; retry the blocked step or continue with the planned method", tool)
	return missingToolRemediation{
		Handled:     true,
		ToolName:    tool,
		Observation: "runtime remediation succeeded: " + msg,
		RecoverHint: msg,
	}
}

func waitForInstallApprovalDecision(ctx context.Context, manager *Manager, runID, approvalID string, timeout time.Duration) (decision string, reason string) {
	deadline := time.Now().Add(timeout)
	if timeout <= 0 {
		deadline = time.Now()
	}
	for {
		if ctx.Err() != nil {
			return "timeout", "worker context canceled"
		}
		events, err := manager.Events(runID, 0)
		if err == nil {
			for i := len(events) - 1; i >= 0; i-- {
				event := events[i]
				if event.Type != EventTypeApprovalGranted && event.Type != EventTypeApprovalDenied {
					continue
				}
				payload := map[string]any{}
				if len(event.Payload) > 0 {
					_ = json.Unmarshal(event.Payload, &payload)
				}
				if strings.TrimSpace(toString(payload["approval_id"])) != approvalID {
					continue
				}
				switch event.Type {
				case EventTypeApprovalGranted:
					return "approved", strings.TrimSpace(toString(payload["reason"]))
				case EventTypeApprovalDenied:
					return "denied", strings.TrimSpace(toString(payload["reason"]))
				}
			}
		}
		if !time.Now().Before(deadline) {
			return "timeout", "approval timeout"
		}
		timer := time.NewTimer(350 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "timeout", "worker context canceled"
		case <-timer.C:
		}
	}
}

type toolInstallCommand struct {
	Command string
	Args    []string
}

func buildToolInstallCommand(tool string) (toolInstallCommand, bool) {
	pkg := normalizeToolToken(tool)
	if pkg == "" {
		return toolInstallCommand{}, false
	}
	if _, err := exec.LookPath(pkg); err == nil {
		return toolInstallCommand{}, false
	}
	useSudo := os.Geteuid() != 0
	withPrivilege := func(base ...string) toolInstallCommand {
		if !useSudo {
			return toolInstallCommand{Command: base[0], Args: append([]string{}, base[1:]...)}
		}
		if _, err := exec.LookPath("sudo"); err != nil {
			return toolInstallCommand{}
		}
		return toolInstallCommand{
			Command: "sudo",
			Args:    append([]string{"-n"}, base...),
		}
	}
	if _, err := exec.LookPath("apt-get"); err == nil {
		cmd := withPrivilege("apt-get", "install", "-y", pkg)
		return cmd, strings.TrimSpace(cmd.Command) != ""
	}
	if _, err := exec.LookPath("apt"); err == nil {
		cmd := withPrivilege("apt", "install", "-y", pkg)
		return cmd, strings.TrimSpace(cmd.Command) != ""
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		cmd := withPrivilege("dnf", "install", "-y", pkg)
		return cmd, strings.TrimSpace(cmd.Command) != ""
	}
	if _, err := exec.LookPath("yum"); err == nil {
		cmd := withPrivilege("yum", "install", "-y", pkg)
		return cmd, strings.TrimSpace(cmd.Command) != ""
	}
	if _, err := exec.LookPath("apk"); err == nil {
		cmd := withPrivilege("apk", "add", pkg)
		return cmd, strings.TrimSpace(cmd.Command) != ""
	}
	if _, err := exec.LookPath("pacman"); err == nil {
		cmd := withPrivilege("pacman", "--noconfirm", "-S", pkg)
		return cmd, strings.TrimSpace(cmd.Command) != ""
	}
	if _, err := exec.LookPath("brew"); err == nil {
		return toolInstallCommand{Command: "brew", Args: []string{"install", pkg}}, true
	}
	return toolInstallCommand{}, false
}

func discoverAvailableFallbackTools() string {
	candidates := []string{"nmap", "curl", "wget", "openssl", "python3", "bash", "sh", "nc", "ss", "ip", "grep", "sed", "awk"}
	available := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate); err == nil {
			available = append(available, candidate)
		}
		if len(available) >= 8 {
			break
		}
	}
	if len(available) == 0 {
		return "none discovered"
	}
	return strings.Join(available, ", ")
}
