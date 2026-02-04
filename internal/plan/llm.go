package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type LLMPlanner struct {
	Client llm.Client
	Model  string
}

func (p LLMPlanner) Plan(ctx context.Context, input Input) (string, error) {
	if p.Client == nil {
		return "", fmt.Errorf("llm client missing")
	}
	req := llm.ChatRequest{
		Model:       strings.TrimSpace(p.Model),
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: planSystemPrompt,
			},
			{
				Role:    "user",
				Content: buildPrompt(input),
			},
		},
	}
	resp, err := p.Client.Chat(ctx, req)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(resp.Content)
	content = stripCodeFences(content)
	if content == "" {
		return "", fmt.Errorf("llm plan empty")
	}
	return content, nil
}

func (p LLMPlanner) Next(ctx context.Context, input Input) ([]string, error) {
	if p.Client == nil {
		return nil, fmt.Errorf("llm client missing")
	}
	req := llm.ChatRequest{
		Model:       strings.TrimSpace(p.Model),
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: nextSystemPrompt,
			},
			{
				Role:    "user",
				Content: buildPrompt(input),
			},
		},
	}
	resp, err := p.Client.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Next []string `json:"next"`
	}
	raw := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse next json: %w", err)
	}
	steps := []string{}
	for _, item := range parsed.Next {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		steps = append(steps, item)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("llm next steps empty")
	}
	return steps, nil
}

func buildPrompt(input Input) string {
	builder := strings.Builder{}
	builder.WriteString(fmt.Sprintf("Session: %s\n", input.SessionID))
	builder.WriteString(fmt.Sprintf("Scope: %s\n", joinOrFallback(input.Scope)))
	builder.WriteString(fmt.Sprintf("Targets: %s\n", joinOrFallback(input.Targets)))
	if input.Summary != "" {
		builder.WriteString("\nSummary:\n" + input.Summary + "\n")
	}
	if len(input.KnownFacts) > 0 {
		builder.WriteString("\nKnown facts:\n")
		for _, fact := range input.KnownFacts {
			builder.WriteString("- " + fact + "\n")
		}
	}
	if input.Inventory != "" {
		builder.WriteString("\nInventory:\n" + input.Inventory + "\n")
	}
	return builder.String()
}

const planSystemPrompt = "You are a security testing planner. Return a concise markdown plan only. Use headings: \"## Plan\", \"### Scope\", \"### Targets\", \"### Phases\", \"### Risks/Notes\". Keep bullets short. Stay within authorized scope and do not suggest destructive actions unless explicitly requested."

const nextSystemPrompt = "You are a security testing assistant. Respond with JSON only: {\"next\":[\"...\"]}. Provide 3-5 short, safe next steps. If targets are missing, make the first step ask for targets. No extra text."

func stripCodeFences(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimLeft(trimmed, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
		trimmed = strings.TrimSpace(trimmed)
	}
	if strings.HasSuffix(trimmed, "```") {
		trimmed = strings.TrimSuffix(trimmed, "```")
	}
	return strings.TrimSpace(trimmed)
}

func extractJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return trimmed
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}
