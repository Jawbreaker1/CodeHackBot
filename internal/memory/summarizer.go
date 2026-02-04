package memory

import (
	"context"
	"encoding/json"
	"fmt"
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
	LogSnippets     []LogSnippet
	LedgerSnippet   string
	PlanSnippet     string
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
	summary := append([]string{}, input.ExistingSummary...)
	if len(summary) == 0 {
		summary = []string{"Summary unavailable; review recent logs."}
	} else {
		summary = append(summary, fmt.Sprintf("Summary refreshed (%s).", fallbackReason(input.Reason)))
	}
	if len(input.LogSnippets) > 0 {
		paths := []string{}
		for _, snippet := range input.LogSnippets {
			paths = append(paths, snippet.Path)
		}
		summary = append(summary, fmt.Sprintf("Recent logs: %s", strings.Join(paths, ", ")))
	}
	facts := append([]string{}, input.ExistingFacts...)
	return SummaryOutput{Summary: summary, Facts: facts}, nil
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
	Client llm.Client
	Model  string
}

func (s LLMSummarizer) Summarize(ctx context.Context, input SummaryInput) (SummaryOutput, error) {
	if s.Client == nil {
		return SummaryOutput{}, fmt.Errorf("llm client missing")
	}
	model := strings.TrimSpace(s.Model)
	req := llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You summarize security testing sessions. Respond with JSON only: {\"summary\":[\"...\"],\"facts\":[\"...\"]}. Keep bullets short.",
			},
			{
				Role:    "user",
				Content: buildPrompt(input),
			},
		},
		Temperature: 0.2,
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
