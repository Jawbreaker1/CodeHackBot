package assist

import (
	"context"
	"fmt"
	"strings"

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

// SuggestionParseError captures LLM outputs that were returned successfully but
// could not be parsed into the expected JSON suggestion schema.
type SuggestionParseError struct {
	Raw string
	Err error
}

type LLMAssistant struct {
	Client            llm.Client
	Model             string
	Temperature       *float32
	MaxTokens         *int
	RepairTemperature *float32
	RepairMaxTokens   *int
	OnSuggestMeta     func(LLMSuggestMetadata)
}

type LLMSuggestMetadata struct {
	Model           string
	ParseRepairUsed bool
}

func (a LLMAssistant) Suggest(ctx context.Context, input Input) (Suggestion, error) {
	meta := LLMSuggestMetadata{
		Model: strings.TrimSpace(a.Model),
	}
	defer func() {
		if a.OnSuggestMeta != nil {
			a.OnSuggestMeta(meta)
		}
	}()

	if a.Client == nil {
		return Suggestion{}, fmt.Errorf("llm client missing")
	}
	req := llm.ChatRequest{
		Model:       strings.TrimSpace(a.Model),
		Temperature: selectFloat32(a.Temperature, 0.2),
		MaxTokens:   selectInt(a.MaxTokens, 0),
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
	suggestion, parseErr := parseSuggestionResponse(resp.Content)
	if parseErr == nil {
		return suggestion, nil
	}
	meta.ParseRepairUsed = true

	// One strict repair retry improves reliability on models that sometimes
	// answer in prose or wrap JSON in extra text.
	repairReq := llm.ChatRequest{
		Model:       strings.TrimSpace(a.Model),
		Temperature: selectFloat32(a.RepairTemperature, 0),
		MaxTokens:   selectInt(a.RepairMaxTokens, 0),
		Messages: []llm.Message{
			{
				Role: "system",
				Content: "Return a single JSON object only, no prose, no markdown fences, no extra keys. " +
					"Allowed schema keys: type,command,args,question,summary,final,risk,steps,plan,tool.",
			},
			{
				Role:    "user",
				Content: buildRepairPrompt(input, resp.Content),
			},
		},
	}
	repairResp, repairErr := a.Client.Chat(ctx, repairReq)
	if repairErr == nil {
		repaired, repairedErr := parseSuggestionResponse(repairResp.Content)
		if repairedErr == nil {
			return repaired, nil
		}
		return Suggestion{}, SuggestionParseError{
			Raw: strings.TrimSpace(resp.Content) + "\n---repair-attempt---\n" + strings.TrimSpace(repairResp.Content),
			Err: repairedErr.Err,
		}
	}
	// Prefer parse error classification here so fallback reason is accurate and
	// parse-only failures do not trigger cooldown.
	return Suggestion{}, parseErr
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
