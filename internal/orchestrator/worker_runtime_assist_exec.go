package orchestrator

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/msf"
)

func executeWorkerAssistCommand(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, command string, args []string, workDir string) workerToolResult {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	runtimeNotes := []string{}
	if cfg.Diagnostic {
		runtimeNotes = append(runtimeNotes, "diagnostic mode: skipped runtime input repair")
	} else {
		if repairedArgs, repairNotes, repaired, repairErr := repairMissingCommandInputPaths(cfg, task, command, args); repairErr != nil {
			runtimeNotes = append(runtimeNotes, fmt.Sprintf("runtime input repair skipped: %v", repairErr))
		} else if repaired {
			args = repairedArgs
			runtimeNotes = append(runtimeNotes, repairNotes...)
		}
	}

	if isBuiltinListDir(command) {
		output, err := builtinListDir(args, workDir)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "list_dir", args: args, output: output, runErr: err}
	}
	if isBuiltinReadFile(command) {
		output, err := builtinReadFile(args, workDir)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "read_file", args: args, output: output, runErr: err}
	}
	if isBuiltinWriteFile(command) {
		output, err := builtinWriteFile(args, workDir)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "write_file", args: args, output: output, runErr: err}
	}
	if isBuiltinBrowse(command) {
		output, err := builtinBrowse(ctx, args)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "browse", args: args, output: output, runErr: err}
	}
	if isBuiltinCrawl(command) {
		output, err := builtinCrawl(ctx, args)
		output = prependWorkerOutputNotes(output, runtimeNotes)
		return workerToolResult{command: "crawl", args: args, output: output, runErr: err}
	}
	msfNotes := []string{}
	if !cfg.Diagnostic {
		adaptedCmd, adaptedArgs, nextNotes, adaptErr := msf.AdaptRuntimeCommand(command, args, workDir)
		if adaptErr != nil {
			output := prependWorkerOutputNotes([]byte(adaptErr.Error()+"\n"), runtimeNotes)
			return workerToolResult{command: command, args: args, output: output, runErr: adaptErr}
		}
		command = adaptedCmd
		args = adaptedArgs
		msfNotes = nextNotes
	} else {
		runtimeNotes = append(runtimeNotes, "diagnostic mode: skipped runtime command adaptation")
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	env, guardrailNotes, envErr := applyWorkerRuntimeGuardrails(os.Environ(), workDir)
	if envErr != nil {
		return workerToolResult{command: command, args: args, output: []byte(envErr.Error() + "\n"), runErr: envErr}
	}
	cmd.Env = env
	output, err := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
	noteLines := append([]string{}, runtimeNotes...)
	noteLines = append(noteLines, guardrailNotes...)
	noteLines = append(noteLines, msfNotes...)
	output = prependWorkerOutputNotes(output, noteLines)
	return workerToolResult{command: command, args: args, output: output, runErr: err}
}

func executeWorkerAssistTool(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, scopePolicy *ScopePolicy, workDir string, spec *assist.ToolSpec) (workerToolResult, error) {
	if spec == nil {
		return workerToolResult{}, fmt.Errorf("tool suggestion missing spec")
	}
	runCommand := strings.TrimSpace(spec.Run.Command)
	if runCommand == "" {
		return workerToolResult{}, fmt.Errorf("tool suggestion missing run.command")
	}
	files := spec.Files
	if len(files) == 0 {
		return workerToolResult{}, fmt.Errorf("tool suggestion has no files")
	}
	if len(files) > 20 {
		return workerToolResult{}, fmt.Errorf("tool suggestion too many files (%d > 20)", len(files))
	}

	toolRoot := filepath.Join(workDir, "tools")
	if err := os.MkdirAll(toolRoot, 0o755); err != nil {
		return workerToolResult{}, fmt.Errorf("create tool root: %w", err)
	}

	var builder strings.Builder
	for _, file := range files {
		relPath := strings.TrimSpace(file.Path)
		if relPath == "" {
			continue
		}
		if filepath.IsAbs(relPath) {
			return workerToolResult{}, fmt.Errorf("tool file must be relative: %s", relPath)
		}
		dst := filepath.Clean(filepath.Join(toolRoot, relPath))
		if !pathWithinRoot(dst, toolRoot) {
			return workerToolResult{}, fmt.Errorf("tool file escapes tool root: %s", relPath)
		}
		if len(file.Content) > workerAssistWriteMaxBytes {
			return workerToolResult{}, fmt.Errorf("tool file too large: %s", relPath)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return workerToolResult{}, fmt.Errorf("create tool parent dir: %w", err)
		}
		if err := os.WriteFile(dst, []byte(file.Content), 0o644); err != nil {
			return workerToolResult{}, fmt.Errorf("write tool file: %w", err)
		}
		_, _ = fmt.Fprintf(&builder, "Wrote tool file: %s\n", dst)
	}

	args := normalizeArgs(spec.Run.Args)
	if !cfg.Diagnostic {
		runCommand, args = normalizeWorkerAssistCommand(runCommand, args)
	} else {
		_, _ = fmt.Fprintf(&builder, "Diagnostic mode: skipped shell command normalization\n")
	}
	if isWorkerBuiltinCommand(runCommand) {
		if !cfg.Diagnostic {
			var injected bool
			var target string
			args, injected, target = applyCommandTargetFallback(scopePolicy, task, runCommand, args)
			if injected {
				_, _ = fmt.Fprintf(&builder, "Runtime adaptation: auto-injected target %s for command %s\n", target, runCommand)
			}
		} else {
			_, _ = fmt.Fprintf(&builder, "Diagnostic mode: skipped target auto-injection for builtin command\n")
		}
		if requiresCommandScopeValidation(runCommand, args) {
			if err := scopePolicy.ValidateCommandTargets(runCommand, args); err != nil {
				return workerToolResult{}, fmt.Errorf("%s: %w", WorkerFailureScopeDenied, err)
			}
		}
		result := executeWorkerAssistCommand(ctx, cfg, task, runCommand, args, workDir)
		builder.Write(result.output)
		return workerToolResult{
			command: result.command,
			args:    result.args,
			output:  capBytes([]byte(builder.String()), workerAssistOutputLimit),
			runErr:  result.runErr,
		}, nil
	}
	var adaptedNote string
	msfNotes := []string{}
	if !cfg.Diagnostic {
		args, _, _ = applyCommandTargetFallback(scopePolicy, task, runCommand, args)
		runCommand, args, adaptedNote, _ = adaptCommandForRuntime(scopePolicy, runCommand, args)
		if nextArgs, injected, target := applyCommandTargetFallback(scopePolicy, task, runCommand, args); injected {
			args = nextArgs
			_, _ = fmt.Fprintf(&builder, "Runtime adaptation follow-up: auto-injected target %s for command %s\n", target, runCommand)
		}
		adaptedCmd, adaptedArgs, nextNotes, adaptErr := msf.AdaptRuntimeCommand(runCommand, args, workDir)
		if adaptErr != nil {
			return workerToolResult{}, adaptErr
		}
		runCommand = adaptedCmd
		args = adaptedArgs
		msfNotes = nextNotes
	} else {
		_, _ = fmt.Fprintf(&builder, "Diagnostic mode: skipped target auto-injection and runtime command adaptation\n")
	}
	for _, note := range msfNotes {
		if strings.TrimSpace(note) != "" {
			_, _ = fmt.Fprintf(&builder, "%s\n", note)
		}
	}
	if err := scopePolicy.ValidateCommandTargets(runCommand, args); err != nil {
		return workerToolResult{}, fmt.Errorf("%s: %w", WorkerFailureScopeDenied, err)
	}
	cmd := exec.Command(runCommand, args...)
	cmd.Dir = workDir
	env, guardrailNotes, envErr := applyWorkerRuntimeGuardrails(os.Environ(), workDir)
	if envErr != nil {
		return workerToolResult{}, envErr
	}
	cmd.Env = env
	for _, note := range guardrailNotes {
		if strings.TrimSpace(note) != "" {
			_, _ = fmt.Fprintf(&builder, "%s\n", note)
		}
	}
	output, runErr := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
	if adaptedNote != "" {
		_, _ = fmt.Fprintf(&builder, "Runtime adaptation: %s\n", adaptedNote)
	}
	builder.Write(output)
	return workerToolResult{
		command: runCommand,
		args:    args,
		output:  capBytes([]byte(builder.String()), workerAssistOutputLimit),
		runErr:  runErr,
	}, nil
}

func prependWorkerOutputNotes(output []byte, notes []string) []byte {
	lines := make([]string, 0, len(notes))
	for _, note := range notes {
		trimmed := strings.TrimSpace(note)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	if len(lines) == 0 {
		return output
	}
	prefix := strings.Join(lines, "\n")
	if prefix != "" {
		prefix += "\n"
	}
	return append([]byte(prefix), output...)
}

func normalizeWorkerAssistCommand(command string, args []string) (string, []string) {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	if command == "" {
		return "", args
	}
	parts := strings.Fields(command)
	if len(parts) > 1 && len(args) == 0 {
		command = parts[0]
		args = parts[1:]
	}
	base := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	if base == "bash" || base == "sh" || base == "zsh" {
		if normalized, rewritten := normalizeSingleArgShellCommand(args); rewritten {
			return command, normalized
		}
	}
	return command, args
}

func normalizeSingleArgShellCommand(args []string) ([]string, bool) {
	if len(args) != 1 {
		return args, false
	}
	payload := strings.TrimSpace(args[0])
	if payload == "" || strings.HasPrefix(payload, "-") {
		return args, false
	}
	if !strings.ContainsAny(payload, " \t\r\n;&|<>`$()") {
		return args, false
	}
	lower := strings.ToLower(payload)
	if strings.Contains(payload, "/") ||
		strings.HasSuffix(lower, ".sh") ||
		strings.HasSuffix(lower, ".bash") ||
		strings.HasSuffix(lower, ".zsh") {
		return args, false
	}
	return []string{"-lc", payload}, true
}

func isAssistInvalidToolSpecError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	if lower == "" {
		return false
	}
	invalidHints := []string{
		"tool suggestion missing spec",
		"tool suggestion has no files",
		"tool suggestion too many files",
		"tool suggestion missing run.command",
	}
	for _, hint := range invalidHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func applyCommandTargetFallback(scopePolicy *ScopePolicy, task TaskSpec, command string, args []string) ([]string, bool, string) {
	if scopePolicy == nil {
		return args, false, ""
	}
	command = strings.TrimSpace(command)
	if command == "" || !isNetworkSensitiveCommand(command) {
		return args, false, ""
	}
	if isNmapCommand(command) && nmapHasInputListArg(args) {
		return args, false, ""
	}
	if len(scopePolicy.extractTargets(command, args)) > 0 {
		return args, false, ""
	}
	fallback := firstTaskTarget(task.Targets)
	if fallback == "" {
		fallback = scopePolicy.FirstAllowedTarget()
	}
	if fallback == "" {
		return args, false, ""
	}
	updated := append(append([]string{}, args...), fallback)
	return updated, true, fallback
}

func nmapHasInputListArg(args []string) bool {
	for _, raw := range args {
		arg := strings.ToLower(strings.TrimSpace(raw))
		if arg == "-il" || strings.HasPrefix(arg, "-il") {
			return true
		}
	}
	return false
}

func firstTaskTarget(targets []string) string {
	for _, target := range targets {
		trimmed := strings.TrimSpace(target)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeArgs(args []string) []string {
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

func writeWorkerActionLog(cfg WorkerRunConfig, filename string, output []byte) (string, error) {
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	if strings.TrimSpace(filename) == "" {
		filename = fmt.Sprintf("%s-a%d.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt)
	}
	path := filepath.Join(artifactDir, sanitizePathComponent(filename))
	if err := os.WriteFile(path, capBytes(output, workerAssistOutputLimit), 0o644); err != nil {
		return "", fmt.Errorf("write action log: %w", err)
	}
	return path, nil
}

func summarizeCommandResult(command string, args []string, runErr error, output []byte) string {
	summary, _ := summarizeCommandResultWithMeta(command, args, runErr, output)
	return summary
}

type workerAssistCommandSummaryMeta struct {
	OutputBytes        int  `json:"output_bytes"`
	OutputByteCapped   bool `json:"output_byte_capped"`
	OutputCharCapped   bool `json:"output_char_capped"`
	OutputWasEmptyText bool `json:"output_was_empty_text"`
}

func summarizeCommandResultWithMeta(command string, args []string, runErr error, output []byte) (string, workerAssistCommandSummaryMeta) {
	joined := strings.TrimSpace(strings.Join(append([]string{strings.TrimSpace(command)}, args...), " "))
	if joined == "" {
		joined = "(command)"
	}
	outputSummary, meta := summarizeOutputWithMeta(output)
	if runErr != nil {
		return fmt.Sprintf("command failed: %s | %v | output: %s", joined, runErr, outputSummary), meta
	}
	return fmt.Sprintf("command ok: %s | output: %s", joined, outputSummary), meta
}

func summarizeSuggestion(suggestion assist.Suggestion) string {
	parts := []string{suggestion.Type}
	if s := strings.TrimSpace(suggestion.Summary); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(suggestion.Plan); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " | ")
}

func summarizeOutput(output []byte) string {
	text, _ := summarizeOutputWithMeta(output)
	return text
}

func summarizeOutputWithMeta(output []byte) (string, workerAssistCommandSummaryMeta) {
	meta := workerAssistCommandSummaryMeta{
		OutputBytes:      len(output),
		OutputByteCapped: len(output) > 800,
	}
	text := strings.TrimSpace(string(capBytes(output, 800)))
	if text == "" {
		meta.OutputWasEmptyText = true
		return "(no output)", meta
	}
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 300 {
		meta.OutputCharCapped = true
		return text[:300] + "...", meta
	}
	return text, meta
}

func appendObservation(observations []string, entry string) []string {
	entry = normalizeObservationEntry(entry)
	if entry == "" {
		return observations
	}
	observations = append(observations, entry)
	return compactObservations(observations, workerAssistObsLimit, workerAssistObsTokenBudget)
}

var (
	obsErrorHintTerms = []string{
		"failed", "error", "timeout", "timed out", "denied", "not found", "no such file", "scope_denied", "invalid",
	}
	obsIPv4LikePattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?:/\d{1,2})?\b`)
	obsURLPattern      = regexp.MustCompile(`https?://[^\s]+`)
)

type observationItem struct {
	Index     int
	Entry     string
	Tokens    int
	HasPath   bool
	HasTarget bool
	HasError  bool
	Score     int
}

func compactObservations(observations []string, maxItems int, tokenBudget int) []string {
	normalized := make([]string, 0, len(observations))
	for _, entry := range observations {
		trimmed := normalizeObservationEntry(entry)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	if maxItems <= 0 {
		return []string{normalized[len(normalized)-1]}
	}
	if tokenBudget <= 0 {
		tokenBudget = workerAssistObsTokenBudget
	}

	items := make([]observationItem, 0, len(normalized))
	totalTokens := 0
	for idx, entry := range normalized {
		item := buildObservationItem(idx, entry)
		items = append(items, item)
		totalTokens += item.Tokens
	}
	if len(items) <= maxItems && totalTokens <= tokenBudget {
		return append([]string{}, normalized...)
	}

	selected := map[int]struct{}{}
	selectedTokens := 0
	selectIndex := func(idx int) {
		if _, exists := selected[idx]; exists {
			return
		}
		selected[idx] = struct{}{}
		selectedTokens += items[idx].Tokens
	}

	latestIndex := len(items) - 1
	selectIndex(latestIndex)
	minKeep := obsMinInt(4, maxItems)

	priorityOrder := make([]int, 0, len(items))
	for idx := range items {
		if idx == latestIndex {
			continue
		}
		priorityOrder = append(priorityOrder, idx)
	}
	sort.Slice(priorityOrder, func(i, j int) bool {
		left := items[priorityOrder[i]]
		right := items[priorityOrder[j]]
		if left.Score == right.Score {
			return left.Index > right.Index
		}
		return left.Score > right.Score
	})

	for _, idx := range priorityOrder {
		if len(selected) >= maxItems {
			break
		}
		item := items[idx]
		needsSignalRetention := item.Score >= 3
		if selectedTokens+item.Tokens > tokenBudget && len(selected) >= minKeep && !needsSignalRetention {
			continue
		}
		selectIndex(idx)
	}

	for idx := latestIndex - 1; idx >= 0 && len(selected) < maxItems; idx-- {
		if _, exists := selected[idx]; exists {
			continue
		}
		item := items[idx]
		if selectedTokens+item.Tokens > tokenBudget && len(selected) >= minKeep {
			continue
		}
		selectIndex(idx)
	}

	selectedIndices := make([]int, 0, len(selected))
	for idx := range selected {
		selectedIndices = append(selectedIndices, idx)
	}
	sort.Ints(selectedIndices)
	retained := make([]string, 0, len(selectedIndices))
	for _, idx := range selectedIndices {
		retained = append(retained, items[idx].Entry)
	}

	dropped := make([]string, 0, len(items)-len(selectedIndices))
	for idx, item := range items {
		if _, exists := selected[idx]; exists {
			continue
		}
		dropped = append(dropped, item.Entry)
	}
	if summary := buildObservationCompactionSummary(dropped); summary != "" {
		retained = prependObservationSummary(retained, summary, maxItems, tokenBudget)
	}
	return retained
}

func normalizeObservationEntry(entry string) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(entry)), " ")
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= workerAssistObsMaxEntryChars {
		return trimmed
	}
	head := (workerAssistObsMaxEntryChars * 2) / 3
	tail := workerAssistObsMaxEntryChars - head - 3
	if tail < 0 {
		tail = 0
	}
	if head > len(trimmed) {
		head = len(trimmed)
	}
	if tail > len(trimmed)-head {
		tail = len(trimmed) - head
	}
	if tail <= 0 {
		return trimmed[:head]
	}
	return trimmed[:head] + "..." + trimmed[len(trimmed)-tail:]
}

func buildObservationItem(index int, entry string) observationItem {
	hasPath := len(extractObservationPaths(entry)) > 0
	hasTarget := len(extractObservationTargets(entry)) > 0
	hasError := containsObservationErrorHint(entry)
	score := 0
	if hasPath {
		score += 4
	}
	if hasTarget {
		score += 4
	}
	if hasError {
		score += 4
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(entry)), "recovery:") {
		score++
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(entry)), "command failed:") {
		score++
	}
	return observationItem{
		Index:     index,
		Entry:     entry,
		Tokens:    estimateObservationTokens(entry),
		HasPath:   hasPath,
		HasTarget: hasTarget,
		HasError:  hasError,
		Score:     score,
	}
}

func estimateObservationTokens(entry string) int {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return 0
	}
	wordApprox := len(strings.Fields(trimmed))
	charApprox := (len(trimmed) + 3) / 4
	if charApprox > wordApprox {
		return charApprox
	}
	if wordApprox < 1 {
		return 1
	}
	return wordApprox
}

func containsObservationErrorHint(entry string) bool {
	lower := strings.ToLower(strings.TrimSpace(entry))
	if lower == "" {
		return false
	}
	for _, hint := range obsErrorHintTerms {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func extractObservationPaths(entry string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, token := range splitAnchorTokens(entry) {
		anchor := normalizePathAnchorToken(token)
		if anchor == "" {
			continue
		}
		if _, exists := seen[anchor]; exists {
			continue
		}
		seen[anchor] = struct{}{}
		out = append(out, anchor)
		if len(out) >= workerAssistObsCompactionAnchorCap {
			break
		}
	}
	return out
}

func extractObservationTargets(entry string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	add := func(value string) {
		trimmed := strings.TrimSpace(strings.Trim(value, "\"'()[]{}<>.,;:"))
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	for _, match := range obsURLPattern.FindAllString(entry, -1) {
		add(match)
		if len(out) >= workerAssistObsCompactionAnchorCap {
			return out
		}
	}
	for _, match := range obsIPv4LikePattern.FindAllString(entry, -1) {
		if strings.Contains(match, "/") {
			if _, _, err := net.ParseCIDR(match); err == nil {
				add(match)
			}
		} else if net.ParseIP(match) != nil {
			add(match)
		}
		if len(out) >= workerAssistObsCompactionAnchorCap {
			return out
		}
	}
	return out
}

func buildObservationCompactionSummary(dropped []string) string {
	if len(dropped) == 0 {
		return ""
	}
	paths := []string{}
	targets := []string{}
	errors := []string{}
	seenPath := map[string]struct{}{}
	seenTarget := map[string]struct{}{}
	seenErr := map[string]struct{}{}
	for _, entry := range dropped {
		for _, path := range extractObservationPaths(entry) {
			if _, exists := seenPath[path]; exists {
				continue
			}
			seenPath[path] = struct{}{}
			paths = append(paths, path)
			if len(paths) >= workerAssistObsCompactionAnchorCap {
				break
			}
		}
		for _, target := range extractObservationTargets(entry) {
			if _, exists := seenTarget[target]; exists {
				continue
			}
			seenTarget[target] = struct{}{}
			targets = append(targets, target)
			if len(targets) >= workerAssistObsCompactionAnchorCap {
				break
			}
		}
		if containsObservationErrorHint(entry) {
			errKey := summarizeObservationError(entry)
			if errKey != "" {
				if _, exists := seenErr[errKey]; !exists {
					seenErr[errKey] = struct{}{}
					errors = append(errors, errKey)
				}
			}
			if len(errors) >= workerAssistObsCompactionAnchorCap {
				break
			}
		}
	}
	parts := []string{fmt.Sprintf("compaction_summary: dropped=%d", len(dropped))}
	if len(paths) > 0 {
		parts = append(parts, "paths="+strings.Join(paths, ","))
	}
	if len(targets) > 0 {
		parts = append(parts, "targets="+strings.Join(targets, ","))
	}
	if len(errors) > 0 {
		parts = append(parts, "errors="+strings.Join(errors, ","))
	}
	if len(parts) <= 1 {
		return ""
	}
	return normalizeObservationEntry(strings.Join(parts, " | "))
}

func summarizeObservationError(entry string) string {
	normalized := normalizeObservationEntry(entry)
	if normalized == "" {
		return ""
	}
	if len(normalized) > workerAssistObsCompactionErrMaxChars {
		normalized = normalized[:workerAssistObsCompactionErrMaxChars] + "..."
	}
	return normalized
}

func prependObservationSummary(retained []string, summary string, maxItems int, tokenBudget int) []string {
	summary = normalizeObservationEntry(summary)
	if summary == "" {
		return retained
	}
	if tokenBudget <= 0 {
		tokenBudget = workerAssistObsTokenBudget
	}
	out := append([]string{}, retained...)
	if len(out) >= maxItems {
		dropIdx := observationDropIndexForSummary(out)
		if dropIdx < 0 {
			return out
		}
		next := make([]string, 0, len(out))
		next = append(next, out[:dropIdx]...)
		next = append(next, out[dropIdx+1:]...)
		out = next
		out = append([]string{summary}, out...)
	} else {
		out = append([]string{summary}, out...)
	}
	for observationTokens(out) > tokenBudget && len(out) > 1 {
		// Prefer dropping oldest non-summary entries first.
		if len(out) > 2 {
			out = append(out[:1], out[2:]...)
			continue
		}
		out = out[:1]
	}
	return out
}

func observationDropIndexForSummary(entries []string) int {
	if len(entries) == 0 {
		return -1
	}
	latest := len(entries) - 1
	dropIdx := -1
	dropScore := 1 << 30
	for idx, entry := range entries {
		if idx == latest {
			continue
		}
		score := buildObservationItem(idx, entry).Score
		if score < dropScore {
			dropScore = score
			dropIdx = idx
		}
	}
	if dropIdx >= 0 {
		return dropIdx
	}
	return 0
}

func observationTokens(values []string) int {
	total := 0
	for _, value := range values {
		total += estimateObservationTokens(value)
	}
	return total
}

func obsMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func capBytes(data []byte, max int) []byte {
	if max <= 0 || len(data) <= max {
		return data
	}
	out := make([]byte, max)
	copy(out, data[:max])
	return out
}

func buildAssistActionKey(command string, args []string) string {
	canonicalCommand := canonicalAssistActionCommand(command)
	canonicalArgs := canonicalAssistActionArgs(canonicalCommand, args)
	parts := append([]string{canonicalCommand}, canonicalArgs...)
	return strings.Join(parts, "\x1f")
}

var assistActionCommandAliases = map[string]string{
	"dir":         "list_dir",
	"ls":          "list_dir",
	"list_dir":    "list_dir",
	"read":        "read_file",
	"read_file":   "read_file",
	"write":       "write_file",
	"write_file":  "write_file",
	"links":       "parse_links",
	"parse_links": "parse_links",
}

var assistActionOrderInsensitiveCommands = map[string]struct{}{
	"list_dir": {},
}

func canonicalAssistActionCommand(command string) string {
	normalized := strings.ToLower(strings.TrimSpace(command))
	if normalized == "" {
		return ""
	}
	normalized = filepath.Base(normalized)
	if alias, ok := assistActionCommandAliases[normalized]; ok {
		return alias
	}
	return normalized
}

func canonicalAssistActionArgs(command string, args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.ToLower(strings.TrimSpace(arg))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) <= 1 {
		return out
	}
	if isAssistShellWrapper(command) {
		return out
	}
	if _, ok := assistActionOrderInsensitiveCommands[command]; !ok {
		return out
	}
	sort.Strings(out)
	return out
}

func isAssistShellWrapper(command string) bool {
	switch strings.ToLower(filepath.Base(strings.TrimSpace(command))) {
	case "bash", "sh", "zsh", "ash", "dash", "ksh":
		return true
	default:
		return false
	}
}
