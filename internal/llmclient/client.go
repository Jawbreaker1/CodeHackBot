package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal OpenAI-compatible chat client for the rebuild path.
type Client struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// Chat sends a minimal chat completion request and returns the assistant content.
func (c Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return "", fmt.Errorf("base url is required")
	}
	if strings.TrimSpace(c.Model) == "" {
		return "", fmt.Errorf("model is required")
	}
	if len(messages) == 0 {
		return "", fmt.Errorf("messages are required")
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}

	body, err := json.Marshal(chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: 0.2,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("chat request returned status %s", resp.Status)
	}

	var decoded chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}
	return strings.TrimSpace(decoded.Choices[0].Message.Content), nil
}
