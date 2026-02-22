package cli

import (
	"context"
	"errors"
	neturl "net/url"
	"os"
	goexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func (r *Runner) recordActionArtifact(logPath string) {
	path := strings.TrimSpace(logPath)
	if path == "" {
		return
	}
	r.lastActionLogPath = path
}

func (r *Runner) recordObservationFromResult(kind string, result exec.CommandResult, err error) {
	command := strings.TrimSpace(result.Command)
	if command == "" {
		return
	}
	excerpt := firstLines(result.Output, 12)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	r.recordObservationWithCommand(kind, command, result.Args, result.LogPath, excerpt, errText, exitCodeFromErr(err))
}

func (r *Runner) recordObservation(kind string, args []string, logPath string, outputExcerpt string, err error) {
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	r.recordObservationWithCommand(kind, kind, args, logPath, outputExcerpt, errText, exitCodeFromErr(err))
}

func (r *Runner) recordObservationWithCommand(kind string, command string, args []string, logPath string, outputExcerpt string, errText string, exitCode int) {
	logPath = strings.TrimSpace(logPath)
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	manager := r.memoryManager(sessionDir)
	_, _ = manager.RecordObservation(memory.Observation{
		Time:          time.Now().UTC().Format(time.RFC3339),
		Kind:          strings.TrimSpace(kind),
		Command:       strings.TrimSpace(command),
		Args:          args,
		ExitCode:      exitCode,
		Error:         strings.TrimSpace(errText),
		LogPath:       logPath,
		OutputExcerpt: strings.TrimSpace(outputExcerpt),
	})
	if logPath != "" && exitCode == 0 {
		r.lastSuccessLogPath = logPath
	}
}

func (r *Runner) clearActionContext() {
	r.lastActionLogPath = ""
	r.lastSuccessLogPath = ""
	r.lastBrowseLogPath = ""
}

func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return 124
	}
	var exitErr *goexec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ProcessState != nil {
			return exitErr.ProcessState.ExitCode()
		}
	}
	lower := strings.ToLower(err.Error())
	if idx := strings.LastIndex(lower, "exit status "); idx >= 0 {
		tail := strings.TrimSpace(lower[idx+len("exit status "):])
		end := len(tail)
		for i, r := range tail {
			if r < '0' || r > '9' {
				end = i
				break
			}
		}
		if end > 0 {
			if n, convErr := strconv.Atoi(tail[:end]); convErr == nil {
				return n
			}
		}
	}
	return 1
}

func (r *Runner) updateKnownTargetFromText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if url := extractFirstURL(text); url != "" {
		if normalized, err := normalizeURL(url); err == nil {
			if parsed, err := neturl.Parse(normalized); err == nil && parsed.Host != "" {
				r.lastKnownTarget = parsed.Hostname()
				return
			}
		}
	}
	token := extractHostLikeToken(text)
	if token != "" {
		r.lastKnownTarget = token
	}
}

func (r *Runner) bestKnownTarget() string {
	target := strings.TrimSpace(r.lastKnownTarget)
	if target == "" {
		return ""
	}
	if strings.Contains(target, "://") {
		parsed, err := neturl.Parse(target)
		if err == nil && parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(target, "."))
}

func (r *Runner) appendConversation(role, content string) {
	content = strings.TrimSpace(content)
	if role == "" || content == "" {
		return
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return
	}
	r.appendChatHistory(artifacts.ChatPath, role, content)
	r.maybeAutoSummarizeChat(sessionDir, artifacts.ChatPath)
}

func (r *Runner) updateTaskFoundation(goal string) {
	goal = collapseWhitespace(strings.TrimSpace(goal))
	if goal == "" {
		return
	}
	if len(goal) > 240 {
		goal = goal[:240]
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return
	}
	existing, err := memory.ReadBullets(artifacts.FocusPath)
	if err != nil {
		existing = nil
	}
	items := []string{goal}
	for _, item := range existing {
		item = collapseWhitespace(strings.TrimSpace(item))
		if item == "" || strings.EqualFold(item, goal) || strings.EqualFold(item, "Not set.") {
			continue
		}
		items = append(items, item)
		if len(items) >= 12 {
			break
		}
	}
	_ = memory.WriteFocus(artifacts.FocusPath, items)
}

func (r *Runner) maybeAutoSummarizeChat(sessionDir, chatPath string) {
	if chatPath == "" {
		return
	}
	manager := r.memoryManager(sessionDir)
	state, err := manager.RecordLog(chatPath)
	if err != nil {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Context tracking failed: %v", err)
		}
		return
	}
	if !manager.ShouldSummarize(state) {
		return
	}
	if r.cfg.Permissions.Level == "readonly" {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Auto-summarize skipped (readonly)")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summarize")
	defer stopIndicator()
	if err := manager.Summarize(ctx, r.summaryGenerator(), "chat"); err != nil {
		r.logger.Printf("Auto-summarize failed: %v", err)
		return
	}
	if r.cfg.UI.Verbose {
		r.logger.Printf("Auto-summary updated")
	}
}

func (r *Runner) summaryArtifactPath(goal string) string {
	lastAction := strings.TrimSpace(r.lastActionLogPath)
	lastSuccess := strings.TrimSpace(r.lastSuccessLogPath)
	if isFailureSummaryIntent(goal) {
		if fileExists(lastAction) {
			return lastAction
		}
	}
	if fileExists(lastSuccess) {
		return lastSuccess
	}
	if fileExists(lastAction) {
		return lastAction
	}
	successFromState, latestFromState := r.recentObservationLogPaths()
	if !isFailureSummaryIntent(goal) && fileExists(successFromState) {
		return successFromState
	}
	if fileExists(latestFromState) {
		return latestFromState
	}
	return ""
}

func (r *Runner) recentObservationLogPaths() (string, string) {
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return "", ""
	}
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil || len(state.RecentObservations) == 0 {
		return "", ""
	}
	latest := ""
	success := ""
	for i := len(state.RecentObservations) - 1; i >= 0; i-- {
		obs := state.RecentObservations[i]
		logPath := strings.TrimSpace(obs.LogPath)
		if !fileExists(logPath) {
			continue
		}
		if latest == "" {
			latest = logPath
		}
		if success == "" && obs.ExitCode == 0 {
			success = logPath
		}
		if latest != "" && success != "" {
			break
		}
	}
	return success, latest
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func (r *Runner) latestObservation() (memory.Observation, bool) {
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return memory.Observation{}, false
	}
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil || len(state.RecentObservations) == 0 {
		return memory.Observation{}, false
	}
	return state.RecentObservations[len(state.RecentObservations)-1], true
}
