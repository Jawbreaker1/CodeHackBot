package cli

import (
	"os"
	"regexp"
	"strings"
	"unicode"
)

func normalizeAssistantOutput(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = ansiEscapePattern.ReplaceAllString(text, "")
	text = stripThinkBlocks(text)
	text = strings.ReplaceAll(text, "\t", "    ")
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	original := text
	hadReasoningPrefix := startsWithReasoningMarker(text)
	if looksLikeStructuredReasoningLeak(text) {
		hadReasoningPrefix = true
		if extracted := extractUserFacingSection(text); extracted != "" {
			text = extracted
		} else {
			text = ""
		}
	}
	text = stripReasoningTail(text)
	if strings.TrimSpace(text) == "" {
		if hadReasoningPrefix {
			// Never surface chain-of-thought-only output to the operator.
			text = "I could not produce a clean user-facing summary for this step."
		} else {
			// Prefer a noisy answer over an empty one; empty replies create confusing
			// delayed interactions where the next prompt appears to answer the prior one.
			text = original
		}
	}
	width := 0
	if isTerminalStdout() {
		width = terminalWidth() - 2
	}
	return wrapTextForTerminal(text, width)
}

func stripThinkBlocks(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	clean := thinkBlockPattern.ReplaceAllString(text, "")
	return strings.TrimSpace(clean)
}

func stripReasoningTail(text string) string {
	// Some models leak chain-of-thought markers at start-of-line or inline.
	// Trim everything from the first marker onward.
	markers := []string{
		"thinking process:",
		"reasoning:",
		"internal reasoning:",
		"chain of thought:",
		"analyze the request:",
		"analyze the input data:",
		"evaluate current state:",
		"drafting the response:",
		"refine based on constraints:",
		"final review against constraints:",
		"constructing output:",
	}
	lower := strings.ToLower(text)
	cut := -1
	for _, marker := range markers {
		idx := strings.Index(lower, marker)
		if idx >= 0 && (cut == -1 || idx < cut) {
			cut = idx
		}
	}
	if cut < 0 {
		return text
	}
	if cut == 0 {
		// Model started with reasoning. Try to salvage a user-facing section.
		if extracted := extractUserFacingSection(text); extracted != "" {
			return extracted
		}
		return ""
	}
	return strings.TrimSpace(text[:cut])
}

func looksLikeStructuredReasoningLeak(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "analyze the request:") ||
		strings.Contains(lower, "refine based on constraints:") ||
		strings.Contains(lower, "final review against constraints:") ||
		strings.Contains(lower, "constructing output:") {
		return true
	}
	structured := regexp.MustCompile(`(?m)^\s*\d+\.\s+\*\*(analyze|evaluate|drafting|refine|final review|constructing)\b`)
	return structured.MatchString(lower)
}

func startsWithReasoningMarker(text string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	if trimmed == "" {
		return false
	}
	markers := []string{
		"thinking process:",
		"reasoning:",
		"internal reasoning:",
		"chain of thought:",
	}
	for _, marker := range markers {
		if strings.HasPrefix(trimmed, marker) {
			return true
		}
	}
	return false
}

func extractUserFacingSection(text string) string {
	type marker struct {
		display string
		query   string
	}
	markers := []marker{
		{display: "summary:", query: "summary:"},
		{display: "findings:", query: "findings:"},
		{display: "result:", query: "result:"},
		{display: "final answer:", query: "final answer:"},
		{display: "next steps:", query: "next steps:"},
		{display: "answer:", query: "answer:"},
	}
	lower := strings.ToLower(text)
	best := -1
	bestDisplay := ""
	for _, marker := range markers {
		if idx := strings.Index(lower, marker.query); idx >= 0 && (best < 0 || idx < best) {
			best = idx
			bestDisplay = marker.display
		}
		emphasized := "*" + marker.display
		if idx := strings.Index(lower, emphasized); idx >= 0 && (best < 0 || idx < best) {
			best = idx
			bestDisplay = marker.display
		}
	}
	if best < 0 {
		return ""
	}
	out := strings.TrimSpace(text[best:])
	// Normalize markdown-emphasized heading variants.
	out = strings.TrimPrefix(out, "*")
	out = strings.TrimPrefix(out, "*")
	if bestDisplay != "" && strings.HasPrefix(strings.ToLower(out), bestDisplay) {
		return out
	}
	return strings.TrimSpace(out)
}

var ansiEscapePattern = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
var thinkBlockPattern = regexp.MustCompile(`(?is)<think>.*?</think>`)

func wrapTextForTerminal(text string, width int) string {
	if width < 10 {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRightFunc(line, unicode.IsSpace)
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapLine(line, width)...)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func wrapLine(line string, width int) []string {
	if width < 10 {
		return []string{line}
	}
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	out := []string{}
	for len(runes) > width {
		cut := width
		for i := width; i > 0; i-- {
			if unicode.IsSpace(runes[i-1]) {
				cut = i
				break
			}
		}
		if cut == 0 {
			cut = width
		}
		segment := strings.TrimRightFunc(string(runes[:cut]), unicode.IsSpace)
		if segment != "" {
			out = append(out, segment)
		}
		runes = trimLeadingSpaceRunes(runes[cut:])
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}

func trimLeadingSpaceRunes(r []rune) []rune {
	for len(r) > 0 && unicode.IsSpace(r[0]) {
		r = r[1:]
	}
	return r
}

func isTerminalStdout() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func looksLikeChat(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	if looksLikeAction(trimmed) {
		return false
	}
	if strings.Contains(trimmed, "?") {
		return true
	}
	if hasPrefixOneOf(trimmed, "hello", "hi", "hey", "good morning", "good afternoon", "good evening") {
		return true
	}
	if hasPrefixOneOf(trimmed, "my name is", "i am", "i'm") {
		return true
	}
	if hasPrefixOneOf(trimmed, "who ", "what ", "where ", "why ", "how ", "can you", "could you", "would you", "tell me", "explain", "describe") {
		return true
	}
	return true
}

func looksLikeAction(text string) bool {
	if hasURLHint(text) {
		return true
	}
	if looksLikeFileQuery(text) {
		return true
	}
	verbs := map[string]struct{}{
		"scan": {}, "enumerate": {}, "list": {}, "show": {}, "find": {}, "run": {}, "check": {}, "exploit": {},
		"test": {}, "probe": {}, "search": {}, "ping": {}, "nmap": {}, "curl": {}, "msf": {}, "msfconsole": {},
		"netstat": {}, "ls": {}, "whoami": {}, "cat": {}, "dir": {}, "open": {}, "dump": {}, "inspect": {}, "analyze": {},
		"investigate": {}, "recon": {}, "crawl": {}, "browse": {}, "report": {}, "summarize": {},
		"extract": {}, "crack": {}, "decrypt": {}, "recover": {}, "unlock": {}, "bruteforce": {},
	}
	for _, token := range splitTokens(text) {
		if _, ok := verbs[token]; ok {
			return true
		}
	}
	return false
}

func hasPrefixOneOf(text string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func splitTokens(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= '0' && r <= '9' {
			return false
		}
		return true
	})
}

func looksLikeFileQuery(text string) bool {
	lower := strings.ToLower(text)
	if !hasFileHint(lower) {
		return false
	}
	if strings.Contains(lower, "?") {
		return true
	}
	if hasPrefixOneOf(lower, "open", "read", "show", "view", "display", "cat", "print") {
		return true
	}
	if strings.Contains(lower, "inside") || strings.Contains(lower, "contents") || strings.Contains(lower, "content") {
		return true
	}
	if strings.Contains(lower, "what is in") || strings.Contains(lower, "what's in") {
		return true
	}
	return false
}

func hasFileHint(text string) bool {
	if strings.Contains(text, "readme") {
		return true
	}
	if strings.Contains(text, "/") || strings.Contains(text, "\\") {
		return true
	}
	extensions := []string{".md", ".txt", ".log", ".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".go", ".py", ".sh", ".js", ".ts", ".zip", ".7z"}
	for _, ext := range extensions {
		if strings.Contains(text, ext) {
			return true
		}
	}
	return false
}

func hasURLHint(text string) bool {
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		return true
	}
	for _, token := range strings.Fields(text) {
		clean := strings.Trim(token, " \t\r\n\"'()[]{}<>.,;:")
		if isLikelyURLOrHostToken(clean) {
			return true
		}
	}
	return false
}

func extractFirstURL(text string) string {
	if text == "" {
		return ""
	}
	if match := findURLWithScheme(text); match != "" {
		return match
	}
	for _, token := range strings.Fields(text) {
		clean := strings.Trim(token, " \t\r\n\"'()[]{}<>.,;:")
		if clean == "" {
			continue
		}
		if strings.Contains(clean, "://") {
			return clean
		}
		if isLikelyURLOrHostToken(clean) {
			return clean
		}
	}
	return ""
}

func isLikelyURLOrHostToken(token string) bool {
	clean := strings.TrimSpace(token)
	if clean == "" {
		return false
	}
	lower := strings.ToLower(clean)
	if strings.Contains(lower, "://") {
		return true
	}
	// Do not treat local files/paths as URLs (e.g. secret.zip).
	if looksLikePathOrFilename(clean) {
		return false
	}
	if lower == "localhost" {
		return true
	}
	// Accept raw IPv4 literals.
	digitsOrDots := true
	for _, r := range lower {
		if (r >= '0' && r <= '9') || r == '.' {
			continue
		}
		digitsOrDots = false
		break
	}
	if digitsOrDots && strings.Count(lower, ".") == 3 {
		return true
	}
	if strings.HasPrefix(lower, "www.") {
		return strings.Count(lower, ".") >= 1
	}
	if !strings.Contains(lower, ".") {
		return false
	}
	parts := strings.Split(lower, ".")
	if len(parts) < 2 {
		return false
	}
	tld := parts[len(parts)-1]
	if len(tld) < 2 || len(tld) > 24 {
		return false
	}
	for _, r := range tld {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") || strings.Contains(part, "_") {
			return false
		}
	}
	return true
}

func findURLWithScheme(text string) string {
	start := strings.Index(text, "http://")
	if start == -1 {
		start = strings.Index(text, "https://")
	}
	if start == -1 {
		return ""
	}
	rest := text[start:]
	end := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			end = i
			break
		}
	}
	candidate := strings.Trim(rest[:end], "\"'()[]{}<>.,;:")
	return candidate
}

func shouldAutoBrowse(text string) bool {
	url := extractFirstURL(text)
	if url == "" {
		return false
	}
	lower := strings.ToLower(text)
	blockers := []string{"scan", "enumerate", "ffuf", "gobuster", "dirsearch", "nmap", "nikto", "nuclei", "exploit", "attack"}
	for _, word := range blockers {
		if strings.Contains(lower, word) {
			return false
		}
	}
	if strings.TrimSpace(lower) == strings.ToLower(url) || strings.TrimSpace(lower) == "www."+strings.TrimPrefix(strings.ToLower(url), "www.") {
		return true
	}
	infoHints := []string{"what", "tell me", "about", "overview", "summarize", "whois", "info", "information", "website", "site", "page"}
	for _, hint := range infoHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// Moved: UI/prompt/LLM indicator helpers live in ui.go.
