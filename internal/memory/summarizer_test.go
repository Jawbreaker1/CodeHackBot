package memory

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

func TestLLMSummarizerParsesJSON(t *testing.T) {
	client := fakeClient{content: `{"summary":["a"],"facts":["b"]}`}
	summarizer := LLMSummarizer{Client: client}
	out, err := summarizer.Summarize(context.Background(), SummaryInput{SessionID: "s1"})
	if err != nil {
		t.Fatalf("Summarize error: %v", err)
	}
	if len(out.Summary) != 1 || out.Summary[0] != "a" {
		t.Fatalf("summary mismatch: %v", out.Summary)
	}
	if len(out.Facts) != 1 || out.Facts[0] != "b" {
		t.Fatalf("facts mismatch: %v", out.Facts)
	}
}

func TestChainedSummarizerFallback(t *testing.T) {
	primary := LLMSummarizer{Client: fakeClient{err: errors.New("down")}}
	chain := ChainedSummarizer{
		Primary:  primary,
		Fallback: FallbackSummarizer{},
	}
	out, err := chain.Summarize(context.Background(), SummaryInput{})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if len(out.Summary) == 0 {
		t.Fatalf("expected fallback summary")
	}
}
