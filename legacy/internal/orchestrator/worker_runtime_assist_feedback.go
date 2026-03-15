package orchestrator

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type assistExecutionFeedback struct {
	Command             string
	Args                []string
	ExitCode            int
	LogPath             string
	RunError            string
	ResultSummary       string
	PrimaryArtifactRefs []string
	InputPathRefs       []string
	CombinedOutputTail  string
}

func captureAssistExecutionFeedback(command string, args []string, runErr error, output []byte, logPath string) assistExecutionFeedback {
	resultSummary := singleLine(summarizeCommandResult(command, args, runErr, output), 280)
	primaryRefs := inferAssistPrimaryArtifactRefs(command, args)
	inputRefs := inferAssistInputPathRefs(args, primaryRefs)
	feedback := assistExecutionFeedback{
		Command:             strings.TrimSpace(command),
		Args:                append([]string{}, args...),
		ExitCode:            commandExitCode(runErr),
		LogPath:             strings.TrimSpace(logPath),
		RunError:            strings.TrimSpace(runErrString(runErr)),
		ResultSummary:       strings.TrimSpace(resultSummary),
		PrimaryArtifactRefs: append([]string{}, primaryRefs...),
		InputPathRefs:       append([]string{}, inputRefs...),
		CombinedOutputTail:  tailOutputForPrompt(output, 14, 900),
	}
	if feedback.Command == "" {
		feedback.Command = "(unknown)"
	}
	if feedback.ResultSummary == "" {
		feedback.ResultSummary = "(no result summary)"
	}
	if strings.TrimSpace(feedback.CombinedOutputTail) == "" {
		feedback.CombinedOutputTail = "(no output)"
	}
	return feedback
}

func appendAssistExecutionFeedbackToPrompt(feedback *assistExecutionFeedback, recentLog, chatHistory string) (string, string) {
	if feedback == nil {
		return recentLog, chatHistory
	}
	section := renderAssistExecutionFeedbackSection(*feedback)
	if strings.TrimSpace(section) == "" {
		return recentLog, chatHistory
	}
	return appendPromptSection(recentLog, section), appendPromptSection(chatHistory, section)
}

func renderAssistExecutionFeedbackSection(feedback assistExecutionFeedback) string {
	var b strings.Builder
	b.WriteString("[latest_execution_feedback]\n")
	b.WriteString("- command: " + strings.TrimSpace(feedback.Command) + "\n")
	if len(feedback.Args) > 0 {
		b.WriteString("- args: " + strings.Join(feedback.Args, " ") + "\n")
	} else {
		b.WriteString("- args: (none)\n")
	}
	b.WriteString("- exit_code: " + strconv.Itoa(feedback.ExitCode) + "\n")
	if strings.TrimSpace(feedback.ResultSummary) != "" {
		b.WriteString("- result_summary: " + strings.TrimSpace(feedback.ResultSummary) + "\n")
	}
	if len(feedback.PrimaryArtifactRefs) > 0 {
		b.WriteString("- primary_evidence_refs: " + strings.Join(feedback.PrimaryArtifactRefs, ", ") + "\n")
	}
	if strings.TrimSpace(feedback.LogPath) != "" {
		b.WriteString("- artifact_log: " + strings.TrimSpace(feedback.LogPath) + "\n")
	}
	if strings.TrimSpace(feedback.RunError) != "" {
		b.WriteString("- runtime_error: " + strings.TrimSpace(feedback.RunError) + "\n")
	}
	b.WriteString("- stderr_tail: unavailable (runtime captures combined stdout/stderr)\n")
	if strings.TrimSpace(feedback.CombinedOutputTail) != "" {
		b.WriteString("- combined_output_tail:\n")
		for _, line := range strings.Split(strings.TrimSpace(feedback.CombinedOutputTail), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			b.WriteString("  - " + trimmed + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func appendPromptSection(base, section string) string {
	base = strings.TrimSpace(base)
	section = strings.TrimSpace(section)
	if section == "" {
		return base
	}
	if base == "" {
		return section
	}
	return base + "\n" + section
}

func tailOutputForPrompt(output []byte, maxLines int, maxChars int) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if maxLines > 0 && len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	joined := strings.Join(out, "\n")
	if maxChars > 0 && len(joined) > maxChars {
		head := maxChars / 2
		tail := maxChars - head - 3
		if tail < 0 {
			tail = 0
		}
		return fmt.Sprintf("%s...%s", joined[:head], joined[len(joined)-tail:])
	}
	return joined
}

func inferAssistPrimaryArtifactRefs(command string, args []string) []string {
	base := canonicalAssistActionCommand(command)
	refs := []string{}
	if base == "read_file" || base == "list_dir" {
		if path := firstAssistPathArg(args); path != "" {
			refs = append(refs, path)
		}
		return appendUnique(nil, compactStrings(refs)...)
	}
	if isShellWrapperCommand(base, args) {
		return extractAssistShellOutputRefs(args[1])
	}

	outputFlags := map[string]struct{}{
		"-o": {}, "-on": {}, "-ox": {}, "-og": {}, "-oa": {}, "-of": {},
		"--output": {}, "--output-file": {}, "--outfile": {}, "--results-file": {},
		"--report-file": {}, "--save": {}, "--write": {},
	}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		lower := strings.ToLower(arg)
		if _, ok := outputFlags[lower]; ok {
			if i+1 < len(args) {
				if candidate := normalizeAssistPathValue(args[i+1]); looksLikeAssistPathToken(candidate) {
					refs = append(refs, candidate)
				}
			}
			continue
		}
		if !strings.HasPrefix(lower, "-") || !strings.Contains(lower, "=") {
			continue
		}
		name, value, ok := strings.Cut(lower, "=")
		if !ok {
			continue
		}
		if _, ok := outputFlags[name]; ok {
			if candidate := normalizeAssistPathValue(value); looksLikeAssistPathToken(candidate) {
				refs = append(refs, candidate)
			}
		}
	}
	return appendUnique(nil, compactStrings(refs)...)
}

func inferAssistInputPathRefs(args []string, primaryRefs []string) []string {
	if len(args) == 0 {
		return nil
	}
	if isShellWrapperArgs(args) {
		return extractAssistShellInputRefs(args[1], primaryRefs)
	}
	primarySet := map[string]struct{}{}
	for _, ref := range primaryRefs {
		if trimmed := strings.TrimSpace(ref); trimmed != "" {
			primarySet[strings.ToLower(filepath.Clean(trimmed))] = struct{}{}
			primarySet[strings.ToLower(filepath.Base(trimmed))] = struct{}{}
		}
	}
	refs := []string{}
	for _, arg := range args {
		candidate := normalizeAssistPathValue(arg)
		if !looksLikeAssistPathToken(candidate) {
			continue
		}
		cleanCandidate := strings.ToLower(filepath.Clean(candidate))
		if _, ok := primarySet[cleanCandidate]; ok {
			continue
		}
		if _, ok := primarySet[strings.ToLower(filepath.Base(candidate))]; ok {
			continue
		}
		refs = append(refs, candidate)
	}
	return appendUnique(nil, compactStrings(refs)...)
}

var (
	assistShellTeePathPattern         = regexp.MustCompile(`(?:^|[|;&]\s*|\s)tee(?:\s+-a)?\s+([^\s|;&]+)`)
	assistShellRedirectPathPattern    = regexp.MustCompile(`(?:^|[^\d])>>?\s*([^\s|;&]+)`)
	assistShellQuotedWrapperTrimChars = "\"'`()"
)

func isShellWrapperCommand(base string, args []string) bool {
	return (base == "bash" || base == "sh" || base == "zsh") && isShellWrapperArgs(args)
}

func isShellWrapperArgs(args []string) bool {
	if len(args) < 2 {
		return false
	}
	mode := strings.TrimSpace(args[0])
	return mode == "-c" || mode == "-lc"
}

func extractAssistShellOutputRefs(body string) []string {
	refs := []string{}
	for _, matches := range assistShellTeePathPattern.FindAllStringSubmatch(body, -1) {
		if len(matches) < 2 {
			continue
		}
		if path := normalizeAssistShellPath(matches[1]); looksLikeAssistPathToken(path) {
			refs = append(refs, path)
		}
	}
	for _, matches := range assistShellRedirectPathPattern.FindAllStringSubmatch(body, -1) {
		if len(matches) < 2 {
			continue
		}
		if path := normalizeAssistShellPath(matches[1]); looksLikeAssistPathToken(path) {
			refs = append(refs, path)
		}
	}
	return appendUnique(nil, compactStrings(refs)...)
}

func extractAssistShellInputRefs(body string, primaryRefs []string) []string {
	primarySet := map[string]struct{}{}
	for _, ref := range primaryRefs {
		if trimmed := strings.TrimSpace(ref); trimmed != "" {
			primarySet[strings.ToLower(filepath.Clean(trimmed))] = struct{}{}
			primarySet[strings.ToLower(filepath.Base(trimmed))] = struct{}{}
		}
	}
	refs := []string{}
	for _, token := range shellPathArgPattern.FindAllString(body, -1) {
		path := normalizeAssistShellPath(token)
		if !looksLikeAssistPathToken(path) {
			continue
		}
		if _, ok := primarySet[strings.ToLower(filepath.Clean(path))]; ok {
			continue
		}
		if _, ok := primarySet[strings.ToLower(filepath.Base(path))]; ok {
			continue
		}
		refs = append(refs, path)
	}
	return appendUnique(nil, compactStrings(refs)...)
}

func normalizeAssistShellPath(token string) string {
	token = strings.TrimSpace(strings.Trim(token, assistShellQuotedWrapperTrimChars))
	token = strings.TrimSuffix(token, ",")
	token = strings.TrimSuffix(token, ")")
	return token
}

func normalizeAssistPathValue(arg string) string {
	token := strings.TrimSpace(arg)
	if token == "" {
		return ""
	}
	if strings.HasPrefix(token, "-") && strings.Contains(token, "=") {
		_, value, ok := strings.Cut(token, "=")
		if ok {
			token = strings.TrimSpace(value)
		}
	}
	return strings.Trim(token, "\"'")
}

func looksLikeAssistPathToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, "-") {
		return false
	}
	if strings.Contains(token, "://") {
		return false
	}
	if strings.Count(token, ".") == 3 && strings.IndexFunc(token, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	}) == -1 {
		return false
	}
	return strings.Contains(token, "/") || strings.Contains(token, "\\") || filepath.Ext(token) != ""
}
