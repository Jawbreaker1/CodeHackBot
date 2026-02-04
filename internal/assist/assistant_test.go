package assist

import (
	"context"
	"errors"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type fakeClient struct {
	content string
	err     error
}

func (f fakeClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{Content: f.content}, nil
}

func TestFallbackAssistantQuestion(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question, got %s", suggestion.Type)
	}
}

func TestLLMAssistantParsesJSON(t *testing.T) {
	client := fakeClient{content: `{"type":"command","command":"echo","args":["hi"]}`}
	assistant := LLMAssistant{Client: client}
	suggestion, err := assistant.Suggest(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if suggestion.Type != "command" || suggestion.Command != "echo" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
}

func TestLLMAssistantParsesFencedJSON(t *testing.T) {
	client := fakeClient{content: "```json\n{\"type\":\"question\",\"question\":\"targets?\"}\n```"}
	assistant := LLMAssistant{Client: client}
	suggestion, err := assistant.Suggest(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
}

func TestChainedAssistantFallback(t *testing.T) {
	assistant := ChainedAssistant{
		Primary:  LLMAssistant{Client: fakeClient{err: errors.New("down")}},
		Fallback: FallbackAssistant{},
	}
	suggestion, err := assistant.Suggest(context.Background(), Input{})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type == "" {
		t.Fatalf("expected suggestion")
	}
}

func TestNormalizeSuggestionSplitsCommand(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "nmap -sV 10.0.0.1",
	})
	if suggestion.Command != "nmap" {
		t.Fatalf("expected command nmap, got %s", suggestion.Command)
	}
	if len(suggestion.Args) != 2 || suggestion.Args[0] != "-sV" {
		t.Fatalf("unexpected args: %v", suggestion.Args)
	}
}
