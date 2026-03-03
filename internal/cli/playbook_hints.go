package cli

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/playbook"
)

const (
	playbookHintPlanCharBudget    = 700
	playbookHintExecuteCharBudget = 420
	playbookHintRecoverCharBudget = 320
	playbookHintNextCharBudget    = 480
)

type playbookMatch struct {
	entry playbook.Entry
	score int
}

type playbookGuidance struct {
	Tools      []string
	Approach   string
	Guardrails string
}

func buildPlaybookHints(entries []playbook.Entry, goal string, recent string, mode string, maxEntries int, playbookLines int) string {
	if len(entries) == 0 || maxEntries <= 0 {
		return ""
	}
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if isPlaybookListIntent(goal) {
		return playbook.List(entries)
	}

	entryCap := minInt(maxEntries, 2)
	charBudget := adaptivePlaybookHintCharBudget(mode, playbookLines)
	minScore := 1
	switch mode {
	case "execute-step", "follow-up":
		entryCap = 1
		minScore = 2
	case "recover":
		entryCap = 1
		minScore = 2
	case "next-steps":
		entryCap = minInt(maxEntries, 1)
	}

	query := strings.ToLower(goal)
	if (mode == "execute-step" || mode == "recover" || mode == "follow-up") && strings.TrimSpace(recent) != "" {
		query += "\n" + strings.ToLower(extractRecentPlaybookSignal(recent))
	}

	matches := scorePlaybookEntries(entries, query, minScore)
	if len(matches) == 0 {
		return ""
	}
	if len(matches) > entryCap {
		matches = matches[:entryCap]
	}
	return renderCompactPlaybookHints(matches, charBudget, playbookHintDetailLevel(mode, charBudget))
}

func scorePlaybookEntries(entries []playbook.Entry, query string, minScore int) []playbookMatch {
	matches := make([]playbookMatch, 0, len(entries))
	for _, entry := range entries {
		score := scorePlaybookEntry(entry, query)
		if score < minScore {
			continue
		}
		matches = append(matches, playbookMatch{entry: entry, score: score})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].entry.Name < matches[j].entry.Name
		}
		return matches[i].score > matches[j].score
	})
	return matches
}

func scorePlaybookEntry(entry playbook.Entry, query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0
	}
	score := 0
	for _, keyword := range entry.Keywords {
		keyword = strings.TrimSpace(strings.ToLower(keyword))
		if keyword == "" {
			continue
		}
		if strings.Contains(query, keyword) {
			if len(keyword) >= 6 {
				score += 3
			} else {
				score += 2
			}
		}
	}
	name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(entry.Name, ".md")))
	if name != "" && strings.Contains(query, name) {
		score += 3
	}
	return score
}

func renderCompactPlaybookHints(matches []playbookMatch, charBudget int, detailLevel int) string {
	if len(matches) == 0 {
		return ""
	}
	if charBudget <= 0 {
		charBudget = playbookHintExecuteCharBudget
	}
	if detailLevel <= 0 {
		detailLevel = 1
	}
	parts := []string{}
	for _, item := range matches {
		entry := item.entry
		guidance := summarizePlaybookGuidance(entry, detailLevel)
		if len(guidance.Tools) == 0 && guidance.Approach == "" && guidance.Guardrails == "" {
			continue
		}
		builder := strings.Builder{}
		builder.WriteString(fmt.Sprintf("- %s (%s)\n", entry.Title, entry.Name))
		if len(guidance.Tools) > 0 {
			builder.WriteString("  - Tools: " + strings.Join(guidance.Tools, ", ") + "\n")
		}
		if guidance.Approach != "" {
			builder.WriteString("  - Approach: " + guidance.Approach + "\n")
		}
		if guidance.Guardrails != "" {
			builder.WriteString("  - Guardrails: " + guidance.Guardrails + "\n")
		}
		parts = append(parts, strings.TrimSpace(builder.String()))
	}
	if len(parts) == 0 {
		return ""
	}
	text := "Playbook hints (optional; tooling + general approach, not a fixed recipe):\n" + strings.Join(parts, "\n")
	if len(text) <= charBudget {
		return text
	}
	return strings.TrimSpace(text[:maxInt(0, charBudget-16)]) + "...(truncated)"
}

func summarizePlaybookGuidance(entry playbook.Entry, detailLevel int) playbookGuidance {
	if detailLevel < 1 {
		detailLevel = 1
	}
	if detailLevel > 3 {
		detailLevel = 3
	}
	sections := parsePlaybookSections(entry.Content)
	tools := extractPlaybookTools(entry.Content, 3+detailLevel)

	goalHints := selectNonRecipeLines(sections["goal"], 1)
	approachHints := selectNonRecipeLines(append(append([]string{}, sections["planning checklist"]...), sections["procedure"]...), 2+detailLevel)
	guardrails := selectNonRecipeLines(sections["guardrails"], 1+detailLevel)

	approach := ""
	if len(approachHints) > 0 {
		approach = strings.Join(approachHints, " -> ")
	}
	if len(goalHints) > 0 {
		if approach == "" {
			approach = goalHints[0]
		} else {
			approach = goalHints[0] + " Workflow: " + approach
		}
	}
	guardrailText := strings.Join(guardrails, "; ")

	return playbookGuidance{
		Tools:      tools,
		Approach:   clampPlaybookText(approach, 180+detailLevel*80),
		Guardrails: clampPlaybookText(guardrailText, 160+detailLevel*70),
	}
}

func adaptivePlaybookHintCharBudget(mode string, playbookLines int) int {
	base := playbookHintPlanCharBudget
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "execute-step", "follow-up":
		base = playbookHintExecuteCharBudget
	case "recover":
		base = playbookHintRecoverCharBudget
	case "next-steps":
		base = playbookHintNextCharBudget
	}
	lines := playbookLines
	if lines <= 0 {
		lines = 60
	}
	if lines < 30 {
		lines = 30
	}
	if lines > 180 {
		lines = 180
	}
	return maxInt(base/2, (base*lines)/60)
}

func playbookHintDetailLevel(mode string, charBudget int) int {
	base := adaptivePlaybookHintCharBudget(mode, 60)
	switch {
	case charBudget >= base*2:
		return 3
	case charBudget >= (base*3)/2:
		return 2
	default:
		return 1
	}
}

func parsePlaybookSections(content string) map[string][]string {
	sections := map[string][]string{}
	if strings.TrimSpace(content) == "" {
		return sections
	}
	current := ""
	inCodeFence := false
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "```") {
			inCodeFence = !inCodeFence
			continue
		}
		if inCodeFence {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			current = normalizePlaybookSection(line)
			continue
		}
		if current == "" {
			continue
		}
		item := normalizePlaybookLine(line)
		if item == "" {
			continue
		}
		sections[current] = append(sections[current], item)
	}
	return sections
}

func normalizePlaybookSection(line string) string {
	lower := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
	switch {
	case strings.Contains(lower, "planning"):
		return "planning checklist"
	case strings.Contains(lower, "procedure"):
		return "procedure"
	case strings.Contains(lower, "guardrail"):
		return "guardrails"
	case strings.Contains(lower, "goal"):
		return "goal"
	default:
		return lower
	}
}

var numberedItemPattern = regexp.MustCompile(`^\d+[\.\)]\s+`)

func normalizePlaybookLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	switch {
	case strings.HasPrefix(line, "- "):
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
	case strings.HasPrefix(line, "* "):
		line = strings.TrimSpace(strings.TrimPrefix(line, "* "))
	case numberedItemPattern.MatchString(line):
		line = strings.TrimSpace(numberedItemPattern.ReplaceAllString(line, ""))
	}
	line = strings.TrimSpace(strings.TrimSuffix(line, "."))
	if line == "" {
		return ""
	}
	if len(line) > 160 {
		line = strings.TrimSpace(line[:144]) + "...(truncated)"
	}
	return line
}

func selectNonRecipeLines(lines []string, max int) []string {
	if len(lines) == 0 || max <= 0 {
		return nil
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, line := range lines {
		line = collapsePlaybookWhitespace(line)
		if line == "" || isConcreteRecipeLine(line) {
			continue
		}
		key := strings.ToLower(line)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, line)
		if len(out) >= max {
			break
		}
	}
	return out
}

func collapsePlaybookWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func isConcreteRecipeLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return true
	}
	switch {
	case strings.Contains(lower, "example:"),
		strings.Contains(lower, "examples:"),
		strings.Contains(line, "`"),
		strings.Contains(line, "|"),
		strings.Contains(line, "<"),
		strings.Contains(line, ">"),
		strings.Contains(lower, " --"),
		strings.Contains(lower, " -p"),
		strings.HasPrefix(lower, "/"):
		return true
	default:
		return false
	}
}

var (
	codeSpanPattern  = regexp.MustCompile("`([^`]+)`")
	toolTokenPattern = regexp.MustCompile(`^[a-zA-Z0-9_./:-]+$`)
)

func extractPlaybookTools(content string, max int) []string {
	if strings.TrimSpace(content) == "" || max <= 0 {
		return nil
	}
	out := []string{}
	seen := map[string]struct{}{}
	addTool := func(token string) {
		token = normalizeToolToken(token)
		if token == "" {
			return
		}
		key := strings.ToLower(token)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, token)
	}

	for _, m := range codeSpanPattern.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(m[1]))
		if len(fields) == 0 {
			continue
		}
		addTool(fields[0])
		if len(out) >= max {
			return out
		}
	}
	return out
}

func normalizeToolToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	token = strings.Trim(token, "\"'`.,;:()[]{}")
	if token == "" {
		return ""
	}
	if strings.HasPrefix(token, "/") {
		token = strings.TrimPrefix(token, "/")
	}
	if strings.Contains(token, "/") {
		parts := strings.Split(token, "/")
		token = parts[len(parts)-1]
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	lower := strings.ToLower(token)
	switch lower {
	case "example", "examples", "http", "https":
		return ""
	}
	if !toolTokenPattern.MatchString(token) {
		return ""
	}
	if len(token) > 24 {
		token = token[:24]
	}
	return token
}

func clampPlaybookText(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" || max <= 0 || len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max-14]) + "...(truncated)"
}

func isPlaybookListIntent(goal string) bool {
	lower := strings.ToLower(strings.TrimSpace(goal))
	return strings.Contains(lower, "playbook") || strings.Contains(lower, "workflow") || strings.Contains(lower, "procedure")
}

func extractRecentPlaybookSignal(recent string) string {
	lines := strings.Split(recent, "\n")
	out := []string{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(line, "[") || strings.HasPrefix(lower, "out:") || strings.HasPrefix(lower, "error:") {
			out = append(out, line)
		}
		if len(out) >= 10 {
			break
		}
	}
	return strings.Join(out, "\n")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
