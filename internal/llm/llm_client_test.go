package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestSplitBaseURLs(t *testing.T) {
	t.Parallel()

	got := splitBaseURLs("192.168.50.212:1234/v1, http://192.168.50.213:1234 ;192.168.50.212:1234/v1")
	if len(got) != 2 {
		t.Fatalf("expected 2 unique URLs, got %d (%v)", len(got), got)
	}
	if got[0] != "http://192.168.50.212:1234/v1" {
		t.Fatalf("unexpected first URL: %s", got[0])
	}
	if got[1] != "http://192.168.50.213:1234/v1" {
		t.Fatalf("unexpected second URL: %s", got[1])
	}
}

func TestLMStudioClientChatFallbackToSecondEndpoint(t *testing.T) {
	t.Parallel()

	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "ok-second-endpoint"}},
			},
		})
	}))
	defer okServer.Close()

	cfg := config.Config{}
	cfg.LLM.BaseURL = "http://127.0.0.1:1/v1, " + okServer.URL + "/v1"
	cfg.LLM.Model = "qwen/qwen3-coder-30b"
	cfg.LLM.TimeoutSeconds = 10
	client := NewLMStudioClient(cfg)

	resp, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if strings.TrimSpace(resp.Content) != "ok-second-endpoint" {
		t.Fatalf("unexpected response: %q", resp.Content)
	}
}

func TestLMStudioClientChatFallbackFromHTTP500(t *testing.T) {
	t.Parallel()

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failServer.Close()

	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "ok-after-500"}},
			},
		})
	}))
	defer okServer.Close()

	cfg := config.Config{}
	cfg.LLM.BaseURL = failServer.URL + "/v1, " + okServer.URL + "/v1"
	cfg.LLM.Model = "qwen/qwen3-coder-30b"
	cfg.LLM.TimeoutSeconds = 10
	client := NewLMStudioClient(cfg)

	resp, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if strings.TrimSpace(resp.Content) != "ok-after-500" {
		t.Fatalf("unexpected response: %q", resp.Content)
	}
}

func TestLMStudioClientChatAllEndpointsFail(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	cfg.LLM.BaseURL = "http://127.0.0.1:1/v1, http://127.0.0.1:2/v1"
	cfg.LLM.Model = "qwen/qwen3-coder-30b"
	cfg.LLM.TimeoutSeconds = 2
	client := NewLMStudioClient(cfg)

	_, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err == nil {
		t.Fatalf("expected Chat error")
	}
	if !strings.Contains(err.Error(), "across endpoints") {
		t.Fatalf("expected aggregated endpoint error, got: %v", err)
	}
}
