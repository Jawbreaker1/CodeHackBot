package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type LogSnippet struct {
	Path    string
	Content string
}

type SummaryInput struct {
	SessionID       string
	Reason          string
	ExistingSummary []string
	ExistingFacts   []string
	FocusSnippet    string
	LogSnippets     []LogSnippet
	LedgerSnippet   string
	PlanSnippet     string
	ChatHistory     string
}

type SummaryOutput struct {
	Summary []string
	Facts   []string
}

type Summarizer interface {
	Summarize(ctx context.Context, input SummaryInput) (SummaryOutput, error)
}

type FallbackSummarizer struct{}

func (FallbackSummarizer) Summarize(_ context.Context, input SummaryInput) (SummaryOutput, error) {
	summary := carryForwardSummary(input.ExistingSummary, 8)
	commands := extractCommands(input.LogSnippets)
	blockers := extractBlockers(input.LogSnippets, 2)
	if len(summary) == 0 && len(commands) == 0 && len(blockers) == 0 && len(input.LogSnippets) == 0 {
		summary = []string{"Summary unavailable; evidence review in progress."}
	}

	summary = append(summary, fmt.Sprintf("Summary refreshed (%s).", fallbackReason(input.Reason)))
	if len(commands) > 0 {
		summary = append(summary, fmt.Sprintf("Recent actions: %s", strings.Join(compactCommandLabels(commands, 3), "; ")))
	}
	if len(blockers) > 0 {
		summary = append(summary, fmt.Sprintf("Recent blockers: %s", strings.Join(blockers, " | ")))
	}
	if len(input.LogSnippets) > 0 {
		summary = append(summary, fmt.Sprintf("Evidence reviewed: %d recent log snippet(s).", len(input.LogSnippets)))
	}
	facts := mergeLines(input.ExistingFacts, extractFacts(input))
	return SummaryOutput{Summary: summary, Facts: facts}, nil
}

func carryForwardSummary(existing []string, keep int) []string {
	filtered := []string{}
	for _, line := range normalizeLines(existing) {
		if isLowSignalSummaryLine(line) {
			continue
		}
		filtered = append(filtered, line)
	}
	if keep > 0 && len(filtered) > keep {
		filtered = filtered[len(filtered)-keep:]
	}
	return filtered
}

func isLowSignalSummaryLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return true
	}
	switch {
	case lower == "summary pending.",
		lower == "summary unavailable; evidence review in progress.",
		lower == "conversation context considered.",
		strings.HasPrefix(lower, "summary refreshed ("),
		strings.HasPrefix(lower, "evidence reviewed:"),
		strings.HasPrefix(lower, "recent actions:"),
		strings.HasPrefix(lower, "recent blockers:"),
		strings.HasPrefix(lower, "commands:"),
		strings.HasPrefix(lower, "recent logs:"),
		strings.HasPrefix(lower, "recent conversation included."):
		return true
	}
	// Path-heavy bullets tend to drown out actionable context.
	if strings.Contains(lower, "sessions/") && strings.Contains(lower, ".log") {
		return true
	}
	return false
}

type ChainedSummarizer struct {
	Primary  Summarizer
	Fallback Summarizer
}

func (c ChainedSummarizer) Summarize(ctx context.Context, input SummaryInput) (SummaryOutput, error) {
	if c.Primary != nil {
		output, err := c.Primary.Summarize(ctx, input)
		if err == nil && len(output.Summary) > 0 {
			return output, nil
		}
	}
	if c.Fallback != nil {
		return c.Fallback.Summarize(ctx, input)
	}
	return SummaryOutput{}, fmt.Errorf("no summarizer available")
}

type LLMSummarizer struct {
	Client      llm.Client
	Model       string
	Temperature *float32
	MaxTokens   *int
}

func (s LLMSummarizer) Summarize(ctx context.Context, input SummaryInput) (SummaryOutput, error) {
	if s.Client == nil {
		return SummaryOutput{}, fmt.Errorf("llm client missing")
	}
	model := strings.TrimSpace(s.Model)
	req := llm.ChatRequest{
		Model:       model,
		Temperature: selectFloat32(s.Temperature, 0.2),
		MaxTokens:   selectInt(s.MaxTokens, 0),
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You summarize security testing sessions. Use only evidence present in provided logs/artifacts/context. Do not invent vulnerabilities or access claims. If something is not proven, write UNVERIFIED. Respond with JSON only: {\"summary\":[\"...\"],\"facts\":[\"...\"]}. Keep bullets short.",
			},
			{
				Role:    "user",
				Content: buildPrompt(input),
			},
		},
	}
	resp, err := s.Client.Chat(ctx, req)
	if err != nil {
		return SummaryOutput{}, err
	}
	var parsed struct {
		Summary []string `json:"summary"`
		Facts   []string `json:"facts"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
		return SummaryOutput{}, fmt.Errorf("parse summary json: %w", err)
	}
	return SummaryOutput{
		Summary: normalizeLines(parsed.Summary),
		Facts:   normalizeLines(parsed.Facts),
	}, nil
}

func buildPrompt(input SummaryInput) string {
	builder := strings.Builder{}
	builder.WriteString(fmt.Sprintf("Session: %s\nReason: %s\n", input.SessionID, fallbackReason(input.Reason)))
	if len(input.ExistingSummary) > 0 {
		builder.WriteString("\nExisting summary:\n")
		for _, line := range input.ExistingSummary {
			builder.WriteString("- " + line + "\n")
		}
	}
	if len(input.ExistingFacts) > 0 {
		builder.WriteString("\nExisting facts:\n")
		for _, line := range input.ExistingFacts {
			builder.WriteString("- " + line + "\n")
		}
	}
	if input.PlanSnippet != "" {
		builder.WriteString("\nPlan snippet:\n")
		builder.WriteString(input.PlanSnippet + "\n")
	}
	if input.LedgerSnippet != "" {
		builder.WriteString("\nLedger snippet:\n")
		builder.WriteString(input.LedgerSnippet + "\n")
	}
	if input.ChatHistory != "" {
		builder.WriteString("\nRecent conversation:\n")
		builder.WriteString(input.ChatHistory + "\n")
	}
	if input.FocusSnippet != "" {
		builder.WriteString("\nTask foundation:\n")
		builder.WriteString(input.FocusSnippet + "\n")
	}
	if len(input.LogSnippets) > 0 {
		builder.WriteString("\nRecent log snippets:\n")
		for _, snippet := range input.LogSnippets {
			builder.WriteString(fmt.Sprintf("\n[log: %s]\n", snippet.Path))
			builder.WriteString(snippet.Content + "\n")
		}
	}
	return builder.String()
}

func fallbackReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return "manual"
	}
	return trimmed
}

func selectFloat32(value *float32, fallback float32) float32 {
	if value == nil {
		return fallback
	}
	return *value
}

func selectInt(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

var (
	ipv4CIDRPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}\b`)
	ipv4Pattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	urlPattern      = regexp.MustCompile(`https?://[^\s]+`)
	nmapHostPattern = regexp.MustCompile(`(?i)^nmap scan report for\s+(.+?)\s+\((\d{1,3}(?:\.\d{1,3}){3})\)$`)
)

func extractCommands(snippets []LogSnippet) []string {
	commands := []string{}
	seen := map[string]struct{}{}
	for _, snippet := range snippets {
		for _, line := range strings.Split(snippet.Content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "$ ") {
				cmd := strings.TrimSpace(strings.TrimPrefix(line, "$ "))
				if cmd == "" {
					continue
				}
				if _, ok := seen[cmd]; ok {
					continue
				}
				seen[cmd] = struct{}{}
				commands = append(commands, cmd)
			}
		}
	}
	return commands
}

func compactCommandLabels(commands []string, max int) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, cmd := range commands {
		label := compactCommandLabel(cmd)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func compactCommandLabel(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	limit := 4
	if len(fields) < limit {
		limit = len(fields)
	}
	tokens := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		token := strings.TrimSpace(fields[i])
		if token == "" {
			continue
		}
		trimmed := strings.Trim(token, "\"'`")
		if strings.Contains(trimmed, "/") {
			base := filepath.Base(trimmed)
			if base != "." && base != "/" && base != "" {
				token = strings.Replace(token, trimmed, base, 1)
			}
		}
		tokens = append(tokens, clampSummaryText(token, 40))
	}
	if len(fields) > limit {
		tokens = append(tokens, "...")
	}
	return clampSummaryText(strings.Join(tokens, " "), 96)
}

func extractBlockers(snippets []LogSnippet, max int) []string {
	blockers := []string{}
	seen := map[string]struct{}{}
	for _, snippet := range snippets {
		if shouldSkipSummaryLogPath(snippet.Path) {
			continue
		}
		for _, raw := range strings.Split(snippet.Content, "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "$ ") {
				continue
			}
			lower := strings.ToLower(line)
			if !containsAny(lower,
				"no such file or directory",
				"not found",
				"permission denied",
				"timed out",
				"failed",
				"unable",
				"error",
				"denied",
			) {
				continue
			}
			blocker := clampSummaryText(collapseWhitespace(line), 110)
			if blocker == "" {
				continue
			}
			if _, ok := seen[blocker]; ok {
				continue
			}
			seen[blocker] = struct{}{}
			blockers = append(blockers, blocker)
			if max > 0 && len(blockers) >= max {
				return blockers
			}
		}
	}
	return blockers
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func collapseWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func clampSummaryText(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 16 {
		return text[:max]
	}
	return strings.TrimSpace(text[:max-14]) + "...(truncated)"
}

func extractFacts(input SummaryInput) []string {
	facts := []string{}
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		facts = append(facts, value)
	}

	for _, snippet := range input.LogSnippets {
		for _, line := range strings.Split(snippet.Content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			match := nmapHostPattern.FindStringSubmatch(line)
			if len(match) == 3 {
				host := strings.TrimSpace(match[1])
				ip := strings.TrimSpace(match[2])
				if host != "" && ip != "" && host != ip {
					add(fmt.Sprintf("Observed host: %s (%s)", host, ip))
				}
			}
		}
		for _, match := range ipv4CIDRPattern.FindAllString(snippet.Content, -1) {
			add(fmt.Sprintf("Observed CIDR: %s", match))
		}
		for _, match := range ipv4Pattern.FindAllString(snippet.Content, -1) {
			add(fmt.Sprintf("Observed IP: %s", match))
		}
		for _, match := range urlPattern.FindAllString(snippet.Content, -1) {
			add(fmt.Sprintf("Observed URL: %s", trimPunctuation(match)))
		}
	}

	if input.PlanSnippet != "" {
		add("Plan updated")
	}
	if input.LedgerSnippet != "" {
		add("Ledger entries present")
	}
	return facts
}

func trimPunctuation(value string) string {
	return strings.TrimRight(value, ".,);]")
}
