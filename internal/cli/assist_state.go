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

func isPlaceholderCommand(cmd string) bool {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	switch cmd {
	case "scan", "recon", "enumerate", "probe", "test":
		return true
	default:
		return false
	}
}

func isSummaryIntent(goal string) bool {
	if goal == "" {
		return false
	}
	lower := strings.ToLower(goal)
	hints := []string{"summary", "summarize", "what did you find", "findings", "what is it about", "tell me what", "report"}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func isWriteCreationIntent(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))
	if goal == "" {
		return false
	}
	actionHints := []string{
		"create", "write", "generate", "save", "draft", "produce", "build",
	}
	hasAction := false
	for _, hint := range actionHints {
		if strings.Contains(goal, hint) {
			hasAction = true
			break
		}
	}
	if !hasAction {
		return false
	}
	artifactHints := []string{
		"file", "report", "document", "markdown", ".md", ".txt", ".json", ".csv", ".xml", ".html", "owasp",
	}
	for _, hint := range artifactHints {
		if strings.Contains(goal, hint) {
			return true
		}
	}
	return false
}

func indicatesMissingCreation(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	missingHints := []string{
		"not created", "didn't create", "didnt create", "doesn't exist", "doesnt exist",
		"no file", "missing file", "can't find", "cannot find", "i dont think", "i don't think",
	}
	for _, hint := range missingHints {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

func isFailureSummaryIntent(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))
	if goal == "" {
		return false
	}
	hints := []string{
		"fail", "failed", "failure", "error", "didn't", "didnt", "couldn't", "couldnt",
		"why not", "what went wrong", "issue",
	}
	for _, hint := range hints {
		if strings.Contains(goal, hint) {
			return true
		}
	}
	return false
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

func (r *Runner) recoveryDirectiveForGoal(goal, mode string) string {
	if mode != "recover" && mode != "follow-up" {
		return ""
	}
	if !isWriteCreationIntent(goal) {
		return ""
	}
	obs, ok := r.latestObservation()
	if !ok || obs.ExitCode != 0 {
		return ""
	}
	command := strings.ToLower(strings.TrimSpace(obs.Command))
	switch command {
	case "list_dir", "ls", "read_file", "read":
		return "Recovery directive: previous step only inspected existing files and produced no write output. Choose a create/write action now (for example, write_file with the requested report filename). Do not repeat list/read checks."
	default:
		return ""
	}
}

func shouldStartBaselineScan(answer string) bool {
	normalized := strings.ToLower(strings.TrimSpace(answer))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "scan") && (strings.Contains(normalized, "start") || strings.Contains(normalized, "first") || strings.HasPrefix(normalized, "yes")) {
		return true
	}
	if strings.Contains(normalized, "non intrusive") && strings.Contains(normalized, "start") {
		return true
	}
	return false
}

func extractHostLikeToken(text string) string {
	candidates := strings.Fields(strings.ToLower(text))
	for _, token := range candidates {
		token = strings.Trim(token, "\"'()[]{}<>.,;:")
		if token == "" {
			continue
		}
		if strings.Contains(token, "/") || strings.Contains(token, ":") {
			continue
		}
		if strings.Count(token, ".") >= 1 && !strings.HasPrefix(token, ".") && !strings.HasSuffix(token, ".") {
			return token
		}
	}
	return ""
}

func isBenignNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		return false
	}
	if cmdErr.Err == nil || !strings.Contains(strings.ToLower(cmdErr.Err.Error()), "exit status 1") {
		return false
	}
	command := strings.ToLower(strings.TrimSpace(cmdErr.Result.Command))
	switch command {
	case "grep", "rg":
		return strings.TrimSpace(cmdErr.Result.Output) == ""
	case "bash", "sh":
		if len(cmdErr.Result.Args) == 0 {
			return false
		}
		joined := strings.ToLower(strings.Join(cmdErr.Result.Args, " "))
		return strings.Contains(joined, "grep") && strings.TrimSpace(cmdErr.Result.Output) == ""
	default:
		return false
	}
}

func isAssistRepeatedGuard(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "assistant loop guard: repeated command blocked") ||
		strings.Contains(lower, "assistant loop guard: repeated question blocked")
}

func shouldTreatPendingInputAsNewGoal(answer, question string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	if isLikelyFollowUpAnswer(answer, question) {
		return false
	}
	return true
}

func isLikelyFollowUpAnswer(answer, question string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	lower := strings.ToLower(answer)
	if shouldUseDefaultChoice(answer, question) {
		return true
	}
	switch lower {
	case "y", "yes", "n", "no", "ok", "okay", "continue", "proceed", "go ahead", "stop", "skip":
		return true
	}
	if extractFirstURL(answer) != "" || extractHostLikeToken(answer) != "" {
		return true
	}
	if looksLikePathOrFilename(answer) {
		return true
	}
	// Short phrases are often direct answers to a clarifying question.
	if !strings.Contains(answer, "?") && len(strings.Fields(answer)) <= 5 {
		return true
	}
	return false
}

func looksLikePathOrFilename(text string) bool {
	token := strings.TrimSpace(text)
	if token == "" {
		return false
	}
	token = strings.Trim(token, "\"'`")
	if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
		return true
	}
	if strings.Contains(token, "/") {
		return true
	}
	base := filepath.Base(token)
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".zip", ".log", ".csv", ".xml", ".html":
		return true
	default:
		return false
	}
}

func shouldUseDefaultChoice(answer, question string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	question = strings.ToLower(strings.TrimSpace(question))
	if answer == "" || question == "" {
		return false
	}
	defaultSignals := []string{"default", "go ahead", "proceed", "use that", "continue", "yes"}
	hasDefault := false
	for _, signal := range defaultSignals {
		if strings.Contains(answer, signal) {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		return false
	}
	optionSignals := []string{"default", "option", "wordlist", "password list", "profile", "mode"}
	for _, signal := range optionSignals {
		if strings.Contains(question, signal) {
			return true
		}
	}
	return false
}

func autoAssistFollowUpAnswer(question string) (string, bool) {
	question = strings.ToLower(strings.TrimSpace(question))
	if question == "" {
		return "", false
	}
	authConfirmHints := []string{
		"is this correct and authorized",
		"is this authorized",
		"within your lab environment",
		"confirm authorization",
	}
	for _, hint := range authConfirmHints {
		if strings.Contains(question, hint) {
			return "Yes. Proceed within the configured in-scope lab targets only.", true
		}
	}
	requiresSpecificInput := []string{
		"which file", "file path", "path", "target", "ip", "hostname", "url", "password",
		"wordlist path", "token", "credential", "username", "scope",
	}
	for _, hint := range requiresSpecificInput {
		if strings.Contains(question, hint) {
			return "", false
		}
	}
	autoContinueHints := []string{
		"default", "proceed", "continue", "go ahead", "do you want me", "should i", "run now", "start now",
	}
	for _, hint := range autoContinueHints {
		if strings.Contains(question, hint) {
			return "go ahead with default", true
		}
	}
	return "", false
}
