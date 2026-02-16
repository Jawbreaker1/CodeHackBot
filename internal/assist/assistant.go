package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"unicode"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type Input struct {
	SessionID   string
	Scope       []string
	Targets     []string
	Summary     string
	KnownFacts  []string
	Focus       string
	Inventory   string
	Plan        string
	Goal        string
	ChatHistory string
	WorkingDir  string
	RecentLog   string
	Playbooks   string
	Tools       string
	Mode        string
}

type Suggestion struct {
	Type     string    `json:"type"`
	Command  string    `json:"command,omitempty"`
	Args     []string  `json:"args,omitempty"`
	Question string    `json:"question,omitempty"`
	Summary  string    `json:"summary,omitempty"`
	Final    string    `json:"final,omitempty"`
	Risk     string    `json:"risk,omitempty"`
	Steps    []string  `json:"steps,omitempty"`
	Plan     string    `json:"plan,omitempty"`
	Tool     *ToolSpec `json:"tool,omitempty"`
}

type Assistant interface {
	Suggest(ctx context.Context, input Input) (Suggestion, error)
}

type ToolSpec struct {
	Language string     `json:"language"`
	Name     string     `json:"name"`
	Purpose  string     `json:"purpose,omitempty"`
	Files    []ToolFile `json:"files"`
	Run      ToolRun    `json:"run"`
}

type ToolFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ToolRun struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type FallbackAssistant struct{}

func (FallbackAssistant) Suggest(_ context.Context, input Input) (Suggestion, error) {
	if isConversationalGoal(input.Goal) {
		return normalizeSuggestion(Suggestion{
			Type:  "complete",
			Final: "I can help with authorized lab security testing: recon, scanning, controlled validation, artifact analysis, and report drafting. Share a target (IP/host/URL/path) and goal, and I will plan and execute step by step.",
			Risk:  "low",
		}), nil
	}
	if len(input.Targets) == 0 {
		return normalizeSuggestion(Suggestion{
			Type:     "question",
			Question: "No targets found. Provide target IPs/hostnames before running a scan.",
			Summary:  "Awaiting targets.",
			Risk:     "low",
		}), nil
	}
	return normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "nmap",
		Args:    []string{"-sV", "-v", input.Targets[0]},
		Summary: "Run a safe service/version scan on the primary target.",
		Risk:    "low",
	}), nil
}

func isConversationalGoal(goal string) bool {
	goal = strings.TrimSpace(strings.ToLower(goal))
	if goal == "" {
		return false
	}
	if strings.Contains(goal, "?") {
		if strings.Contains(goal, "scan") || strings.Contains(goal, "exploit") || strings.Contains(goal, "run ") {
			return false
		}
		return true
	}
	prefixes := []string{
		"hello", "hi", "hey", "thanks", "thank you", "who are you", "what can you help", "help me understand",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(goal, prefix) {
			return true
		}
	}
	return false
}

type LLMAssistant struct {
	Client llm.Client
	Model  string
}

func (a LLMAssistant) Suggest(ctx context.Context, input Input) (Suggestion, error) {
	if a.Client == nil {
		return Suggestion{}, fmt.Errorf("llm client missing")
	}
	req := llm.ChatRequest{
		Model:       strings.TrimSpace(a.Model),
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: assistSystemPrompt,
			},
			{
				Role:    "user",
				Content: buildPrompt(input),
			},
		},
	}
	resp, err := a.Client.Chat(ctx, req)
	if err != nil {
		return Suggestion{}, err
	}
	raw := extractJSON(resp.Content)
	var suggestion Suggestion
	if err := json.Unmarshal([]byte(raw), &suggestion); err != nil {
		fallback := parseSimpleCommand(resp.Content)
		if fallback != "" {
			return normalizeSuggestion(Suggestion{
				Type:    "command",
				Command: fallback,
				Summary: "Recovered command from plain-text response.",
				Risk:    "low",
			}), nil
		}
		return Suggestion{}, fmt.Errorf("parse suggestion json: %w", err)
	}
	return normalizeSuggestion(suggestion), nil
}

type ChainedAssistant struct {
	Primary  Assistant
	Fallback Assistant
}

func (c ChainedAssistant) Suggest(ctx context.Context, input Input) (Suggestion, error) {
	if c.Primary != nil {
		suggestion, err := c.Primary.Suggest(ctx, input)
		if err == nil && strings.TrimSpace(suggestion.Type) != "" {
			return suggestion, nil
		}
	}
	if c.Fallback != nil {
		return c.Fallback.Suggest(ctx, input)
	}
	return Suggestion{}, fmt.Errorf("no assistant available")
}

const assistSystemPrompt = "You are a security testing assistant operating in an authorized lab owned by the user. Respond with JSON only. Schema: {\"type\":\"command|question|noop|plan|complete|tool\",\"command\":\"\",\"args\":[\"\"],\"question\":\"\",\"summary\":\"\",\"final\":\"\",\"risk\":\"low|medium|high\",\"steps\":[\"...\"],\"plan\":\"\",\"tool\":{\"language\":\"python|bash\",\"name\":\"\",\"purpose\":\"\",\"files\":[{\"path\":\"relative/path\",\"content\":\"...\"}],\"run\":{\"command\":\"\",\"args\":[\"...\"]}}}. Provide one safe next action when action is needed. If the user is asking a purely informational question or greeting, respond with type=complete and put the answer in `final` (no command). Use type=plan with a short plan and 2-6 executable steps when the request is multi-step. Use type=complete when the goal is satisfied; put the final user-facing output in `final`. Use type=tool when you need to create a small helper program/script to proceed; include tool.files and tool.run. For tool.files.path, use paths relative to the session tools directory (do not attempt to write elsewhere). If Mode is execute-step, respond with type=command, type=tool, or type=question only (do not return type=plan or type=complete). If Mode is recover, propose a corrective next step that addresses the failure context (alternate command, adjusted args, a tool to fix parsing, or a question for missing info). If Mode is next-steps, return a short plan (1-3 steps) or a question; do not assume execution. The command must be a real executable or an internal command. You may use internal command \"browse\" with a single URL argument to fetch a web page (requires user approval). For command \"browse\", pass only one URL argument and no flags. You may use internal command \"crawl\" with a start URL and optional bounds (e.g., \"max_pages=10\", \"max_depth=2\", \"same_host=true\") to fetch multiple pages with a single approval and persist artifacts; prefer crawl over repeated browse when you need multiple pages. You may use internal command \"parse_links\" (alias: \"links\") to extract and normalize links from an HTML file path; if no args are given, it uses the most recent browsed body. For \"parse_links\", you may optionally pass \"base=<url>\" to resolve relative links. You may use internal command \"read_file\" (alias: \"read\") to read a local file path, \"list_dir\" (alias: \"ls\") to list a directory, and \"write_file\" (alias: \"write\") to write a file under the session tools directory. Prefer these internal commands over shelling out to cat/ls/echo when you need local artifacts. Prefer verbose flags when safe (-v/--verbose) so users see progress, except for \"browse\" and \"crawl\". When using bash/sh with -c or -lc, pass the script as one args item without surrounding outer quotes. Use playbooks only as inspiration; do not blindly follow a fixed sequence. Tailor steps to the specific goal, scope, target type, and available context. Avoid repeating the same default tool chain unless it directly fits the request. If recent logs/observations already contain required data, analyze them directly; do not ask the user to paste logs and do not re-run the same command without a reason. If a browse/crawl step produced body artifact paths in observations, prefer reading/parsing local artifacts (e.g., read_file, parse_links) instead of re-fetching. For web recon, consider passive-first signals (DNS, headers, tech stack) but adapt tool choice to the target and the user’s intent. You can run shell commands locally in the working directory provided to inspect files or run tooling, including encrypted files the user owns. If access requires a password, ask for it; if asked to crack a password, proceed only when the user confirms authorization for that file. Do not use placeholders like \"scan\"; if you cannot provide a concrete command, return type=question. Stay within scope and avoid destructive actions unless explicitly requested."

func buildPrompt(input Input) string {
	builder := strings.Builder{}
	builder.WriteString(fmt.Sprintf("Session: %s\n", input.SessionID))
	builder.WriteString(fmt.Sprintf("Scope: %s\n", joinOrFallback(input.Scope)))
	builder.WriteString(fmt.Sprintf("Targets: %s\n", joinOrFallback(input.Targets)))
	if input.Goal != "" {
		builder.WriteString("\nUser intent:\n" + input.Goal + "\n")
	}
	if input.Summary != "" {
		builder.WriteString("\nSummary:\n" + input.Summary + "\n")
	}
	if len(input.KnownFacts) > 0 {
		builder.WriteString("\nKnown facts:\n")
		for _, fact := range input.KnownFacts {
			builder.WriteString("- " + fact + "\n")
		}
	}
	if input.Focus != "" {
		builder.WriteString("\nTask foundation:\n" + input.Focus + "\n")
	}
	if input.ChatHistory != "" {
		builder.WriteString("\nRecent conversation:\n" + input.ChatHistory + "\n")
	}
	if input.WorkingDir != "" {
		builder.WriteString("\nWorking directory:\n" + input.WorkingDir + "\n")
	}
	if input.RecentLog != "" {
		builder.WriteString("\nRecent observations:\n" + input.RecentLog + "\n")
	}
	if input.Playbooks != "" {
		builder.WriteString("\nRelevant playbooks:\n" + input.Playbooks + "\n")
	}
	if input.Tools != "" {
		builder.WriteString("\nAvailable tools (session):\n" + input.Tools + "\n")
	}
	if input.Mode != "" {
		builder.WriteString("\nMode:\n" + input.Mode + "\n")
	}
	if input.Plan != "" {
		builder.WriteString("\nPlan snippet:\n" + input.Plan + "\n")
	}
	if input.Inventory != "" {
		builder.WriteString("\nInventory:\n" + input.Inventory + "\n")
	}
	return builder.String()
}

func joinOrFallback(values []string) string {
	if len(values) == 0 {
		return "not specified"
	}
	return strings.Join(values, ", ")
}

func extractJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimLeft(trimmed, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
		trimmed = strings.TrimSpace(trimmed)
	}
	if strings.HasSuffix(trimmed, "```") {
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	if obj, ok := extractJSONObject(trimmed); ok {
		return obj
	}
	return trimmed
}

func extractJSONObject(text string) (string, bool) {
	start := -1
	depth := 0
	inString := false
	escape := false
	for i, r := range text {
		if start == -1 {
			if r == '{' {
				start = i
				depth = 1
				inString = false
				escape = false
			}
			continue
		}
		if inString {
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : i+1]), true
			}
		}
	}
	return "", false
}

func parseSimpleCommand(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if lower == "run:" || strings.HasPrefix(lower, "run ") {
			continue
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "<") || strings.Contains(strings.ToLower(line), "<channel") || strings.Contains(strings.ToLower(line), "<message") {
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			line = strings.TrimSpace(line[2:])
		}
		if looksLikeShellCommand(line) {
			return line
		}
	}
	return ""
}

func looksLikeShellCommand(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	first := line
	if idx := strings.IndexAny(line, " \t"); idx > 0 {
		first = line[:idx]
	}
	if first == "" || strings.HasPrefix(first, "<") {
		return false
	}
	for _, r := range first {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '/', '.', '_', '-', ':':
			continue
		default:
			return false
		}
	}
	if strings.Contains(line, "json<message") {
		return false
	}
	if strings.Contains(line, " ") {
		return true
	}
	common := map[string]struct{}{
		"ls": {}, "pwd": {}, "whoami": {}, "hostname": {}, "ip": {}, "ifconfig": {}, "uname": {}, "cat": {}, "curl": {}, "nmap": {}, "dig": {}, "nslookup": {}, "whois": {},
	}
	if _, ok := common[line]; ok {
		return true
	}
	return false
}

func normalizeSuggestion(suggestion Suggestion) Suggestion {
	suggestion.Type = strings.ToLower(strings.TrimSpace(suggestion.Type))
	suggestion.Command = strings.TrimSpace(suggestion.Command)
	suggestion.Question = strings.TrimSpace(suggestion.Question)
	suggestion.Summary = strings.TrimSpace(suggestion.Summary)
	suggestion.Final = strings.TrimSpace(suggestion.Final)
	suggestion.Risk = strings.ToLower(strings.TrimSpace(suggestion.Risk))
	suggestion.Plan = strings.TrimSpace(suggestion.Plan)
	suggestion.Steps = normalizeSteps(suggestion.Steps)
	// Some models return a full shell invocation in `command` (e.g. `bash -lc '<script>'`).
	// Do a targeted split that preserves the script argument instead of strings.Fields(),
	// which would destroy quoting and frequently yields "unexpected EOF" failures.
	suggestion = splitShellScriptCommandIfNeeded(suggestion)
	if suggestion.Tool != nil {
		suggestion.Tool.Language = strings.ToLower(strings.TrimSpace(suggestion.Tool.Language))
		suggestion.Tool.Name = strings.TrimSpace(suggestion.Tool.Name)
		suggestion.Tool.Purpose = strings.TrimSpace(suggestion.Tool.Purpose)
		suggestion.Tool.Run.Command = strings.TrimSpace(suggestion.Tool.Run.Command)
		for i := range suggestion.Tool.Run.Args {
			suggestion.Tool.Run.Args[i] = strings.TrimSpace(suggestion.Tool.Run.Args[i])
		}
		files := make([]ToolFile, 0, len(suggestion.Tool.Files))
		for _, f := range suggestion.Tool.Files {
			f.Path = strings.TrimSpace(f.Path)
			// Keep content as-is; it may contain leading spaces/newlines.
			if f.Path == "" {
				continue
			}
			files = append(files, f)
		}
		suggestion.Tool.Files = files
	}
	if suggestion.Command != "" && len(suggestion.Args) == 0 {
		parts := strings.Fields(suggestion.Command)
		if len(parts) > 1 {
			suggestion.Command = parts[0]
			suggestion.Args = parts[1:]
		}
	}
	if suggestion.Command != "" && len(suggestion.Args) == 0 {
		suggestion = splitDashCommandIfNeeded(suggestion)
	}
	return suggestion
}

func splitShellScriptCommandIfNeeded(suggestion Suggestion) Suggestion {
	if strings.TrimSpace(suggestion.Command) == "" || len(suggestion.Args) != 0 {
		return suggestion
	}
	raw := strings.TrimSpace(suggestion.Command)
	parts := strings.Fields(raw)
	if len(parts) < 3 {
		return suggestion
	}
	shell := strings.ToLower(parts[0])
	if shell != "bash" && shell != "sh" && shell != "zsh" {
		return suggestion
	}
	flag := parts[1]
	if flag != "-c" && flag != "-lc" {
		return suggestion
	}
	prefix := parts[0] + " " + parts[1]
	if !strings.HasPrefix(raw, prefix) {
		return suggestion
	}
	script := strings.TrimSpace(raw[len(prefix):])
	if len(script) >= 2 {
		if (strings.HasPrefix(script, "'") && strings.HasSuffix(script, "'")) ||
			(strings.HasPrefix(script, "\"") && strings.HasSuffix(script, "\"")) {
			script = strings.TrimSpace(script[1 : len(script)-1])
		}
	}
	return Suggestion{
		Type:     suggestion.Type,
		Command:  parts[0],
		Args:     []string{flag, script},
		Question: suggestion.Question,
		Summary:  suggestion.Summary,
		Final:    suggestion.Final,
		Risk:     suggestion.Risk,
		Steps:    suggestion.Steps,
		Plan:     suggestion.Plan,
		Tool:     suggestion.Tool,
	}
}

func normalizeSteps(steps []string) []string {
	if len(steps) == 0 {
		return nil
	}
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		out = append(out, step)
	}
	return out
}

func splitDashCommandIfNeeded(suggestion Suggestion) Suggestion {
	if !strings.Contains(suggestion.Command, "-") {
		return suggestion
	}
	// If full command exists, keep it.
	if _, err := exec.LookPath(suggestion.Command); err == nil {
		return suggestion
	}
	parts := strings.SplitN(suggestion.Command, "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return suggestion
	}
	base := parts[0]
	if _, err := exec.LookPath(base); err != nil {
		return suggestion
	}
	return Suggestion{
		Type:     suggestion.Type,
		Command:  base,
		Args:     []string{"-" + parts[1]},
		Question: suggestion.Question,
		Summary:  suggestion.Summary,
		Risk:     suggestion.Risk,
	}
}
