package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type Input struct {
	SessionID  string
	Scope      []string
	Targets    []string
	Summary    string
	KnownFacts []string
	Inventory  string
	Plan       string
	Goal       string
}

type Suggestion struct {
	Type     string   `json:"type"`
	Command  string   `json:"command,omitempty"`
	Args     []string `json:"args,omitempty"`
	Question string   `json:"question,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Risk     string   `json:"risk,omitempty"`
}

type Assistant interface {
	Suggest(ctx context.Context, input Input) (Suggestion, error)
}

type FallbackAssistant struct{}

func (FallbackAssistant) Suggest(_ context.Context, input Input) (Suggestion, error) {
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
		Args:    []string{"-sV", input.Targets[0]},
		Summary: "Run a safe service/version scan on the primary target.",
		Risk:    "low",
	}), nil
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

const assistSystemPrompt = "You are a security testing assistant. Respond with JSON only: {\"type\":\"command|question|noop\",\"command\":\"\",\"args\":[\"\"],\"question\":\"\",\"summary\":\"\",\"risk\":\"low|medium|high\"}. Provide one safe next action. The command must be a real executable (e.g., nmap, curl, nc, ping). Put flags/targets in args. Do not use placeholders like \"scan\"; if you cannot provide a concrete command, return type=question. Stay within scope and avoid destructive actions unless explicitly requested."

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
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func normalizeSuggestion(suggestion Suggestion) Suggestion {
	suggestion.Type = strings.ToLower(strings.TrimSpace(suggestion.Type))
	suggestion.Command = strings.TrimSpace(suggestion.Command)
	suggestion.Question = strings.TrimSpace(suggestion.Question)
	suggestion.Summary = strings.TrimSpace(suggestion.Summary)
	suggestion.Risk = strings.ToLower(strings.TrimSpace(suggestion.Risk))
	if suggestion.Command != "" && len(suggestion.Args) == 0 {
		parts := strings.Fields(suggestion.Command)
		if len(parts) > 1 {
			suggestion.Command = parts[0]
			suggestion.Args = parts[1:]
		}
	}
	return suggestion
}
