package plan

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

type capturePlannerClient struct {
	content string
	reqs    []llm.ChatRequest
}

func (c *capturePlannerClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.reqs = append(c.reqs, req)
	return llm.ChatResponse{Content: c.content}, nil
}

func TestFallbackPlannerPlan(t *testing.T) {
	input := Input{
		SessionID: "session-1",
		Scope:     []string{"internal"},
		Targets:   []string{"10.0.0.0/24"},
		KnownFacts: []string{
			"Observed IP: 10.0.0.5",
		},
	}
	content, err := (FallbackPlanner{}).Plan(context.Background(), input)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if !strings.Contains(content, "Plan (Fallback)") {
		t.Fatalf("expected fallback header")
	}
	if !strings.Contains(content, "### Targets") {
		t.Fatalf("expected targets section")
	}
	if !strings.Contains(content, "10.0.0.0/24") {
		t.Fatalf("expected target in plan")
	}
}

func TestChainedPlannerFallback(t *testing.T) {
	primary := LLMPlanner{Client: fakeClient{err: errors.New("down")}}
	chain := ChainedPlanner{
		Primary:  primary,
		Fallback: FallbackPlanner{},
	}
	content, err := chain.Plan(context.Background(), Input{})
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if !strings.Contains(content, "Plan (Fallback)") {
		t.Fatalf("expected fallback plan content")
	}
}

func TestLLMPlannerNextParsesJSON(t *testing.T) {
	client := fakeClient{content: `{"next":["step one","step two"]}`}
	planner := LLMPlanner{Client: client}
	steps, err := planner.Next(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("Next error: %v", err)
	}
	if len(steps) != 2 || steps[0] != "step one" {
		t.Fatalf("unexpected steps: %v", steps)
	}
}

func TestLLMPlannerNextParsesJSONInFence(t *testing.T) {
	client := fakeClient{content: "```json\n{\"next\":[\"one\"]}\n```"}
	planner := LLMPlanner{Client: client}
	steps, err := planner.Next(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("Next error: %v", err)
	}
	if len(steps) != 1 || steps[0] != "one" {
		t.Fatalf("unexpected steps: %v", steps)
	}
}

func TestLLMPlannerUsesConfiguredOptions(t *testing.T) {
	temperature := float32(0.08)
	maxTokens := 700
	client := &capturePlannerClient{content: `{"next":["one"]}`}
	planner := LLMPlanner{
		Client:      client,
		Model:       "test-model",
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	}
	if _, err := planner.Next(context.Background(), Input{SessionID: "s"}); err != nil {
		t.Fatalf("Next error: %v", err)
	}
	if len(client.reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(client.reqs))
	}
	if client.reqs[0].Temperature != temperature || client.reqs[0].MaxTokens != maxTokens {
		t.Fatalf("unexpected llm options: %+v", client.reqs[0])
	}
}
