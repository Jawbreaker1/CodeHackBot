package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
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
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	Temperature    float32   `json:"temperature,omitempty"`
	MaxTokens      int       `json:"max_tokens,omitempty"`
	ResponseFormat any       `json:"response_format,omitempty"`
}

type ChatResponse struct {
	Content      string
	FinishReason string
}

type Client interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

type LMStudioClient struct {
	baseURLs []string
	model    string
	apiKey   string
	http     *http.Client
}

func NewLMStudioClient(cfg config.Config) *LMStudioClient {
	baseURLs := splitBaseURLs(cfg.LLM.BaseURL)
	if len(baseURLs) == 0 {
		baseURLs = []string{normalizeBaseURL("http://localhost:1234/v1")}
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
		baseURLs: baseURLs,
		model:    model,
		apiKey:   cfg.LLM.APIKey,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
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

	if len(c.baseURLs) == 0 {
		return ChatResponse{}, fmt.Errorf("llm base URL is not configured")
	}

	failures := make([]string, 0, len(c.baseURLs))
	for _, baseURL := range c.baseURLs {
		resp, err := c.chatAtEndpoint(ctx, baseURL+"/chat/completions", payload)
		if err == nil {
			return resp, nil
		}
		failures = append(failures, fmt.Sprintf("%s (%v)", baseURL, err))
	}
	return ChatResponse{}, fmt.Errorf("llm request failed across endpoints: %s", strings.Join(failures, " | "))
}

type chatCompletionResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
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

func splitBaseURLs(raw string) []string {
	tokens := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	out := make([]string, 0, len(tokens))
	seen := map[string]struct{}{}
	for _, token := range tokens {
		normalized := normalizeBaseURL(token)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func (c *LMStudioClient) chatAtEndpoint(ctx context.Context, endpoint string, payload []byte) (ChatResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(request)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatResponse{}, fmt.Errorf("status %s", resp.Status)
	}

	var decoded chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ChatResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("response missing choices")
	}
	content := decoded.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return ChatResponse{}, fmt.Errorf("response empty")
	}
	return ChatResponse{
		Content:      content,
		FinishReason: strings.TrimSpace(decoded.Choices[0].FinishReason),
	}, nil
}
