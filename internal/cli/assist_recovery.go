package cli

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func (r *Runner) handleAssistCommandFailure(goal string, suggestion assist.Suggestion, err error) bool {
	if err == nil {
		return false
	}
	// If the user denied an approval prompt, don't try to "recover" automatically.
	lowerErr := strings.ToLower(err.Error())
	if strings.Contains(lowerErr, "not approved") || strings.Contains(lowerErr, "fetch not approved") || strings.Contains(lowerErr, "network access not approved") {
		r.logger.Printf("Aborted by user.")
		return true
	}
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		cmdErr = commandError{
			Result: exec.CommandResult{
				Command: suggestion.Command,
				Args:    suggestion.Args,
			},
			Err: err,
		}
	}
	summary := summarizeCommandFailure(cmdErr)
	if summary != "" {
		r.logger.Printf("Command failed: %s", summary)
	} else {
		r.logger.Printf("Command failed: %v", cmdErr.Err)
	}
	if hint := assistFailureHint(suggestion, cmdErr); hint != "" {
		r.logger.Printf("Hint: %s", hint)
	}
	if r.tryAssistRecovery(suggestion, cmdErr) {
		return true
	}
	return r.suggestAssistRecovery(goal, suggestion, cmdErr)
}

func summarizeCommandFailure(cmdErr commandError) string {
	parts := []string{}
	if cmdErr.Err != nil {
		parts = append(parts, cmdErr.Err.Error())
	}
	if output := firstLines(cmdErr.Result.Output, 2); output != "" {
		parts = append(parts, output)
	}
	return strings.Join(parts, " | ")
}

func (r *Runner) suggestAssistRecovery(goal string, suggestion assist.Suggestion, cmdErr commandError) bool {
	if !r.llmAllowed() {
		r.logger.Printf("No recovery suggestion (LLM unavailable).")
		return true
	}
	recoveryGoal := buildRecoveryGoal(goal, suggestion, cmdErr)
	label := "recover"
	stopIndicator := r.startLLMIndicatorIfAllowed(label)
	recovery, err := r.getAssistSuggestion(recoveryGoal, "recover")
	stopIndicator()
	if err != nil {
		r.logger.Printf("Recovery suggestion failed: %v", err)
		return true
	}
	if recovery.Type == "noop" {
		r.logger.Printf("No recovery suggestion provided.")
		return true
	}
	if recovery.Type == "question" {
		safePrintln(recovery.Question)
		r.appendConversation("Assistant", recovery.Question)
		if strings.TrimSpace(goal) != "" {
			r.pendingAssistGoal = goal
		}
		return true
	}
	if recovery.Type == "plan" {
		_ = r.handlePlanSuggestion(recovery, true)
		return true
	}
	if recovery.Type == "tool" && recovery.Tool != nil {
		r.logger.Printf("Recovery suggestion: tool %s (%s)", fallbackBlock(recovery.Tool.Name), fallbackBlock(recovery.Tool.Language))
		if recovery.Tool.Purpose != "" {
			r.logger.Printf("Recovery purpose: %s", recovery.Tool.Purpose)
		}
		if err := r.executeToolSuggestion(*recovery.Tool, false); err != nil {
			r.logger.Printf("Recovery attempt failed: %v", err)
		}
		return true
	}
	if recovery.Type == "command" {
		r.logger.Printf("Recovery suggestion: %s %s", recovery.Command, strings.Join(recovery.Args, " "))
		if recovery.Summary != "" {
			r.logger.Printf("Recovery summary: %s", recovery.Summary)
		}
		if recovery.Risk != "" {
			r.logger.Printf("Recovery risk: %s", recovery.Risk)
		}
		if err := r.executeAssistSuggestion(recovery, false); err != nil {
			r.logger.Printf("Recovery attempt failed: %v", err)
		}
		return true
	}
	r.logger.Printf("Recovery suggestion returned unknown type: %s", recovery.Type)
	return true
}

func (r *Runner) maybeSuggestNextSteps(goal string, lastSuggestion assist.Suggestion) {
	if !r.llmAllowed() {
		return
	}
	if strings.TrimSpace(goal) == "" && lastSuggestion.Summary == "" {
		return
	}
	nextGoal := buildNextStepsGoal(goal, lastSuggestion)
	stopIndicator := r.startLLMIndicatorIfAllowed("next steps")
	next, err := r.getAssistSuggestion(nextGoal, "next-steps")
	stopIndicator()
	if err != nil {
		r.logger.Printf("Next-step suggestion failed: %v", err)
		return
	}
	switch next.Type {
	case "question":
		if next.Question != "" {
			safePrintln(next.Question)
			r.appendConversation("Assistant", next.Question)
			if strings.TrimSpace(goal) != "" {
				r.pendingAssistGoal = goal
			}
		}
	case "plan":
		steps := next.Steps
		if len(steps) == 0 && next.Plan != "" {
			safePrintln("Possible next steps:")
			safePrintln(next.Plan)
			r.appendConversation("Assistant", "Possible next steps: "+next.Plan)
			return
		}
		if len(steps) > 0 {
			safePrintln("Possible next steps:")
			for i, step := range steps {
				safePrintf("%d) %s\n", i+1, step)
			}
			r.appendConversation("Assistant", "Possible next steps: "+strings.Join(steps, " | "))
		}
	case "command":
		cmdLine := strings.TrimSpace(strings.Join(append([]string{next.Command}, next.Args...), " "))
		if cmdLine != "" {
			safePrintf("Suggested next command: %s\n", cmdLine)
		}
		if next.Summary != "" {
			safePrintf("Why: %s\n", next.Summary)
		}
		r.appendConversation("Assistant", fmt.Sprintf("Suggested next command: %s", cmdLine))
	default:
		if r.cfg.UI.Verbose {
			r.logger.Printf("No next-step suggestion.")
		}
	}
}

func (r *Runner) maybeEmitGoalSummary(goal string, dryRun bool) {
	if dryRun {
		return
	}
	goal = strings.TrimSpace(goal)
	if !isSummaryIntent(goal) {
		return
	}
	if strings.TrimSpace(r.summaryArtifactPath(goal)) == "" {
		return
	}
	if err := r.summarizeFromLatestArtifact(goal); err != nil && r.cfg.UI.Verbose {
		r.logger.Printf("Summary generation failed: %v", err)
	}
}

func (r *Runner) summarizeFromLatestArtifact(goal string) error {
	goal = strings.TrimSpace(goal)
	artifactPath := strings.TrimSpace(r.summaryArtifactPath(goal))
	if goal == "" || artifactPath == "" {
		return nil
	}
	if !r.llmAllowed() {
		return nil
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("read latest artifact: %w", err)
	}
	const maxArtifactBytes = 16000
	if len(data) > maxArtifactBytes {
		data = data[len(data)-maxArtifactBytes:]
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	artifacts, _ := memory.EnsureArtifacts(sessionDir)
	contextSummary := readFileTrimmed(artifacts.SummaryPath)
	contextFacts := readFileTrimmed(artifacts.FactsPath)
	contextFocus := readFileTrimmed(artifacts.FocusPath)
	prompt := strings.Builder{}
	prompt.WriteString("Goal:\n")
	prompt.WriteString(goal + "\n\n")
	prompt.WriteString("Latest action artifact path:\n")
	prompt.WriteString(artifactPath + "\n\n")
	prompt.WriteString("Latest action artifact content:\n")
	prompt.WriteString(string(data) + "\n\n")
	if contextSummary != "" {
		prompt.WriteString("Session summary:\n" + contextSummary + "\n\n")
	}
	if contextFacts != "" {
		prompt.WriteString("Known facts:\n" + contextFacts + "\n\n")
	}
	if contextFocus != "" {
		prompt.WriteString("Task foundation:\n" + contextFocus + "\n\n")
	}
	prompt.WriteString("Provide: 1) concise summary, 2) known findings, 3) unknown/missing data, 4) next 2-3 concrete steps.")

	client := llm.NewLMStudioClient(r.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summary")
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You are BirdHackBot. Summarize from provided artifact/content only. Do not ask user to paste files if artifact content is already present.",
			},
			{
				Role:    "user",
				Content: prompt.String(),
			},
		},
	})
	stopIndicator()
	if err != nil {
		r.recordLLMFailure(err)
		return err
	}
	r.recordLLMSuccess()
	text := normalizeAssistantOutput(resp.Content)
	text = r.enforceEvidenceClaims(text)
	safePrintln(text)
	r.appendConversation("Assistant", text)
	return nil
}

func assistFailureHint(suggestion assist.Suggestion, cmdErr commandError) string {
	command := strings.ToLower(strings.TrimSpace(suggestion.Command))
	outputLower := strings.ToLower(cmdErr.Result.Output)
	errLower := ""
	if cmdErr.Err != nil {
		errLower = strings.ToLower(cmdErr.Err.Error())
	}
	if command == "whois" {
		if strings.Contains(outputLower, "no match") || strings.Contains(outputLower, "not found") {
			return "WHOIS typically only supports root domains (e.g., systemverification.com), not subdomains."
		}
	}
	if command == "fcrackzip" {
		if strings.Contains(outputLower, "rockyou.txt: no such file or directory") || strings.Contains(errLower, "rockyou.txt: no such file or directory") {
			return "Kali often ships rockyou as /usr/share/wordlists/rockyou.txt.gz. Extract it or use that gz source to create a local wordlist file."
		}
	}
	if strings.Contains(errLower, "executable file not found") || strings.Contains(errLower, "not found") {
		return "Tool not available in PATH. Install it or update your tool inventory."
	}
	return ""
}

func (r *Runner) tryAssistRecovery(suggestion assist.Suggestion, cmdErr commandError) bool {
	command := strings.ToLower(strings.TrimSpace(suggestion.Command))
	switch command {
	case "whois":
		return r.tryWhoisRecovery(cmdErr)
	case "fcrackzip":
		return r.tryFcrackzipWordlistRecovery(cmdErr)
	default:
		return false
	}
}

func (r *Runner) tryWhoisRecovery(cmdErr commandError) bool {
	if len(cmdErr.Result.Args) == 0 {
		return false
	}
	original := cmdErr.Result.Args[0]
	alt, ok := normalizeWhoisTarget(original)
	if !ok || strings.EqualFold(alt, original) {
		return false
	}
	outputLower := strings.ToLower(cmdErr.Result.Output)
	if outputLower != "" && !strings.Contains(outputLower, "no match") && !strings.Contains(outputLower, "not found") {
		return false
	}
	r.logger.Printf("Retrying whois with root domain: %s", alt)
	if retryErr := r.handleRun([]string{"whois", alt}); retryErr != nil {
		r.logger.Printf("Retry failed: %v", retryErr)
	}
	return true
}

func (r *Runner) tryFcrackzipWordlistRecovery(cmdErr commandError) bool {
	args := append([]string{}, cmdErr.Result.Args...)
	if len(args) == 0 {
		return false
	}
	wordlistIdx, wordlistArg, ok := fcrackzipWordlistArg(args)
	if !ok {
		return false
	}
	if _, err := os.Stat(wordlistArg); err == nil {
		return false
	}
	combined := strings.ToLower(cmdErr.Result.Output)
	if cmdErr.Err != nil {
		combined += "\n" + strings.ToLower(cmdErr.Err.Error())
	}
	if !strings.Contains(combined, "no such file or directory") {
		return false
	}
	gzSource := strings.TrimSpace(wordlistArg + ".gz")
	if _, err := os.Stat(gzSource); err != nil {
		// Common Kali location fallback.
		gzSource = "/usr/share/wordlists/rockyou.txt.gz"
		if _, err := os.Stat(gzSource); err != nil {
			return false
		}
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		r.logger.Printf("Recovery setup failed: %v", err)
		return false
	}
	outDir := filepath.Join(sessionDir, "artifacts", "wordlists")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		r.logger.Printf("Recovery setup failed: %v", err)
		return false
	}
	extracted := filepath.Join(outDir, "rockyou.txt")
	if _, statErr := os.Stat(extracted); statErr != nil {
		if extractErr := extractGzipFile(gzSource, extracted); extractErr != nil {
			r.logger.Printf("Recovery extraction failed: %v", extractErr)
			return false
		}
	}
	if strings.HasPrefix(args[wordlistIdx], "-p") && args[wordlistIdx] != "-p" {
		args[wordlistIdx] = "-p" + extracted
	} else {
		args[wordlistIdx] = extracted
	}
	r.logger.Printf("Retrying fcrackzip with recovered wordlist: %s", extracted)
	if retryErr := r.handleRun(append([]string{"fcrackzip"}, args...)); retryErr != nil {
		r.logger.Printf("Retry failed: %v", retryErr)
	}
	return true
}

func fcrackzipWordlistArg(args []string) (int, string, bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "-p" {
			if i+1 < len(args) {
				return i + 1, strings.TrimSpace(args[i+1]), true
			}
			return -1, "", false
		}
		if strings.HasPrefix(args[i], "-p") && len(args[i]) > 2 {
			return i, strings.TrimSpace(args[i][2:]), true
		}
	}
	return -1, "", false
}

func extractGzipFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	reader, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer reader.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, reader); err != nil {
		return err
	}
	return nil
}

func buildRecoveryGoal(goal string, suggestion assist.Suggestion, cmdErr commandError) string {
	builder := strings.Builder{}
	if goal != "" {
		builder.WriteString("Original goal: " + goal + "\n")
	}
	if strings.ToLower(strings.TrimSpace(suggestion.Type)) == "tool" && suggestion.Tool != nil {
		builder.WriteString("Previous tool run failed.\n")
		if suggestion.Tool.Name != "" {
			builder.WriteString("Tool name: " + suggestion.Tool.Name + "\n")
		}
		if suggestion.Tool.Language != "" {
			builder.WriteString("Tool language: " + suggestion.Tool.Language + "\n")
		}
		if suggestion.Tool.Purpose != "" {
			builder.WriteString("Tool purpose: " + suggestion.Tool.Purpose + "\n")
		}
		runLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Tool.Run.Command}, suggestion.Tool.Run.Args...), " "))
		if runLine != "" {
			builder.WriteString("Tool run: " + runLine + "\n")
		}
		if len(suggestion.Tool.Files) > 0 {
			builder.WriteString("Tool files:\n")
			for _, f := range suggestion.Tool.Files {
				if strings.TrimSpace(f.Path) == "" {
					continue
				}
				builder.WriteString("- " + f.Path + "\n")
			}
		}
	} else {
		builder.WriteString("Previous command failed.\n")
		cmdLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Command}, suggestion.Args...), " "))
		if cmdLine != "" {
			builder.WriteString("Command: " + cmdLine + "\n")
		}
	}
	if cmdErr.Err != nil {
		builder.WriteString("Error: " + cmdErr.Err.Error() + "\n")
	}
	if output := firstLines(cmdErr.Result.Output, 3); output != "" {
		builder.WriteString("Output: " + output + "\n")
	}
	builder.WriteString("Provide a recovery suggestion or alternative next step.")
	return builder.String()
}

func buildNextStepsGoal(goal string, suggestion assist.Suggestion) string {
	builder := strings.Builder{}
	if goal != "" {
		builder.WriteString("Original goal: " + goal + "\n")
	}
	if suggestion.Summary != "" {
		builder.WriteString("Last action summary: " + suggestion.Summary + "\n")
	}
	cmdLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Command}, suggestion.Args...), " "))
	if cmdLine != "" {
		builder.WriteString("Last command: " + cmdLine + "\n")
	}
	builder.WriteString("Suggest 1-3 concise next steps or a clarifying question.")
	return builder.String()
}

func normalizeWhoisTarget(target string) (string, bool) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	lower = strings.TrimPrefix(lower, "http://")
	lower = strings.TrimPrefix(lower, "https://")
	lower = strings.SplitN(lower, "/", 2)[0]
	lower = strings.TrimSuffix(lower, ".")
	if strings.HasPrefix(lower, "www.") {
		lower = strings.TrimPrefix(lower, "www.")
	}
	if lower == "" {
		return "", false
	}
	return lower, true
}

func normalizeShellScriptArgs(command string, args []string) []string {
	if len(args) < 2 {
		return args
	}
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd != "bash" && cmd != "sh" && cmd != "zsh" {
		return args
	}
	flag := strings.TrimSpace(args[0])
	if flag != "-c" && flag != "-lc" {
		return args
	}
	script := strings.TrimSpace(strings.Join(args[1:], " "))
	if len(script) >= 2 {
		if (strings.HasPrefix(script, "'") && strings.HasSuffix(script, "'")) ||
			(strings.HasPrefix(script, "\"") && strings.HasSuffix(script, "\"")) {
			script = strings.TrimSpace(script[1 : len(script)-1])
		}
	}
	return []string{flag, script}
}

func extractShellScript(command string, args []string) (string, bool) {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd != "bash" && cmd != "sh" && cmd != "zsh" {
		return "", false
	}
	if len(args) < 2 {
		return "", false
	}
	flag := strings.TrimSpace(args[0])
	if flag != "-c" && flag != "-lc" {
		return "", false
	}
	return strings.TrimSpace(strings.Join(args[1:], " ")), true
}

func looksLikeFragileShellPipeline(script string) bool {
	script = strings.ToLower(strings.TrimSpace(script))
	if script == "" {
		return false
	}
	// Only block multi-command/pipeline shells; single commands are fine.
	if !strings.Contains(script, "|") && !strings.Contains(script, "&&") && !strings.Contains(script, ";") {
		return false
	}
	// We mainly want to avoid brittle web parsing pipelines.
	hasHTTP := strings.Contains(script, "curl ") || strings.Contains(script, "wget ") || strings.Contains(script, "http://") || strings.Contains(script, "https://")
	if !hasHTTP {
		return false
	}
	hasTextFilters := strings.Contains(script, "grep ") || strings.Contains(script, "sed ") || strings.Contains(script, "awk ") || strings.Contains(script, "cut ") || strings.Contains(script, "sort ")
	if !hasTextFilters {
		return false
	}
	return true
}

func firstLines(text string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	out := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= maxLines {
			break
		}
	}
	return strings.Join(out, " / ")
}
