package memory

import (
	"context"
	"errors"
	"strings"
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

type captureSummaryClient struct {
	content string
	reqs    []llm.ChatRequest
}

func (c *captureSummaryClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.reqs = append(c.reqs, req)
	return llm.ChatResponse{Content: c.content}, nil
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

func TestFallbackSummarizerExtractsFacts(t *testing.T) {
	input := SummaryInput{
		LogSnippets: []LogSnippet{
			{
				Path:    "log1",
				Content: "$ nmap 10.0.0.1\nDiscovered http://example.local:8080\n",
			},
		},
	}
	out, err := (FallbackSummarizer{}).Summarize(context.Background(), input)
	if err != nil {
		t.Fatalf("fallback summarize error: %v", err)
	}
	foundIP := false
	foundURL := false
	for _, fact := range out.Facts {
		if strings.Contains(fact, "10.0.0.1") {
			foundIP = true
		}
		if strings.Contains(fact, "http://example.local:8080") {
			foundURL = true
		}
	}
	if !foundIP || !foundURL {
		t.Fatalf("expected facts for IP and URL, got %v", out.Facts)
	}
}

func TestLLMSummarizerUsesConfiguredOptions(t *testing.T) {
	temperature := float32(0.12)
	maxTokens := 512
	client := &captureSummaryClient{content: `{"summary":["ok"],"facts":["x"]}`}
	summarizer := LLMSummarizer{
		Client:      client,
		Model:       "test-model",
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	}
	if _, err := summarizer.Summarize(context.Background(), SummaryInput{SessionID: "s1"}); err != nil {
		t.Fatalf("Summarize error: %v", err)
	}
	if len(client.reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(client.reqs))
	}
	if client.reqs[0].Temperature != temperature || client.reqs[0].MaxTokens != maxTokens {
		t.Fatalf("unexpected llm options: %+v", client.reqs[0])
	}
}
