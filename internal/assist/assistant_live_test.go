package assist

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

func TestLLMAssistantSuggestLiveLMStudio(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_LIVE_LLM_TEST") != "1" {
		t.Skip("set BIRDHACKBOT_LIVE_LLM_TEST=1 to run live LLM integration test")
	}

	baseURL := strings.TrimSpace(os.Getenv("BIRDHACKBOT_LLM_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("BIRDHACKBOT_LLM_MODEL"))
	if baseURL == "" || model == "" {
		t.Skip("live test requires BIRDHACKBOT_LLM_BASE_URL and BIRDHACKBOT_LLM_MODEL")
	}

	cfg := config.Config{}
	cfg.LLM.BaseURL = baseURL
	cfg.LLM.Model = model
	cfg.LLM.APIKey = strings.TrimSpace(os.Getenv("BIRDHACKBOT_LLM_API_KEY"))
	cfg.LLM.TimeoutSeconds = 60
	cfg.Agent.Model = model

	assistant := LLMAssistant{
		Client: llm.NewLMStudioClient(cfg),
		Model:  model,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()

	suggestion, err := assistant.Suggest(ctx, Input{
		SessionID:  "live-llm-test",
		Scope:      []string{"local"},
		Targets:    []string{"127.0.0.1"},
		Goal:       "List files in the current directory using one safe local step.",
		WorkingDir: "/tmp",
		Mode:       "execute-step",
	})
	if err != nil {
		t.Fatalf("live Suggest failed: %v", err)
	}
	if strings.TrimSpace(suggestion.Type) == "" {
		t.Fatalf("live Suggest returned empty type")
	}
	switch suggestion.Type {
	case "command", "tool", "complete", "plan", "question", "noop":
	default:
		t.Fatalf("unexpected live suggestion type %q", suggestion.Type)
	}
}
