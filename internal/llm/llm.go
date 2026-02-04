package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type ChatResponse struct {
	Content string
}

type Client interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

type LMStudioClient struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

func NewLMStudioClient(cfg config.Config) *LMStudioClient {
	baseURL := cfg.LLM.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	model := cfg.LLM.Model
	if model == "" {
		model = cfg.Agent.Model
	}
	timeout := time.Duration(cfg.LLM.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &LMStudioClient{
		baseURL: normalizeBaseURL(baseURL),
		model:   model,
		apiKey:  cfg.LLM.APIKey,
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *LMStudioClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if c == nil {
		return ChatResponse{}, fmt.Errorf("llm client is nil")
	}
	if len(req.Messages) == 0 {
		return ChatResponse{}, fmt.Errorf("llm chat requires at least one message")
	}
	if req.Model == "" {
		req.Model = c.model
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal request: %w", err)
	}
	endpoint := c.baseURL + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(request)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatResponse{}, fmt.Errorf("llm request failed with status %s", resp.Status)
	}

	var decoded chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ChatResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("llm response missing choices")
	}
	content := decoded.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return ChatResponse{}, fmt.Errorf("llm response empty")
	}
	return ChatResponse{Content: content}, nil
}

type chatCompletionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

func normalizeBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return trimmed
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	trimmed = strings.TrimRight(trimmed, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed
	}
	return trimmed + "/v1"
}
