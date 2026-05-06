package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

// Profile defines how provider-specific response fields may be selected.
type Profile string

const (
	// ProfileConversation returns only normal assistant content.
	ProfileConversation Profile = "conversation"
	// ProfileStructuredControl permits provider compatibility fallback for machine-readable control calls.
	ProfileStructuredControl Profile = "structured_control"
)

// ResponseSource identifies the provider field selected as the usable response text.
type ResponseSource string

const (
	ResponseSourceContent          ResponseSource = "content"
	ResponseSourceReasoningContent ResponseSource = "reasoning_content"
	ResponseSourceEmpty            ResponseSource = "empty"
)

// ChatOptions configures an LLM request.
type ChatOptions struct {
	Profile Profile
}

// Completion is the normalized provider response. Text is the selected text used
// by callers; Content and ReasoningContent preserve provider fields for diagnostics.
type Completion struct {
	Text             string
	Source           ResponseSource
	Content          string
	ReasoningContent string
	FinishReason     string
	RawResponse      string
	Usage            json.RawMessage
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage,omitempty"`
}

// Chat sends a minimal chat completion request and returns the assistant content.
func (c Client) Chat(ctx context.Context, messages []Message) (string, error) {
	completion, err := c.Complete(ctx, messages, ChatOptions{Profile: ProfileConversation})
	if err != nil {
		return "", err
	}
	if completion.Source == ResponseSourceEmpty && completion.ReasoningContent != "" {
		return "", fmt.Errorf("conversation response missing content; provider returned reasoning_content only")
	}
	return completion.Text, nil
}

// ChatStructured sends a chat completion request for machine-readable control output.
func (c Client) ChatStructured(ctx context.Context, messages []Message) (string, error) {
	completion, err := c.Complete(ctx, messages, ChatOptions{Profile: ProfileStructuredControl})
	if err != nil {
		return "", err
	}
	return completion.Text, nil
}

// Complete sends a minimal chat completion request and normalizes provider output.
func (c Client) Complete(ctx context.Context, messages []Message, opts ChatOptions) (Completion, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return Completion{}, fmt.Errorf("base url is required")
	}
	if strings.TrimSpace(c.Model) == "" {
		return Completion{}, fmt.Errorf("model is required")
	}
	if len(messages) == 0 {
		return Completion{}, fmt.Errorf("messages are required")
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
		return Completion{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Completion{}, fmt.Errorf("chat request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Completion{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Completion{}, fmt.Errorf("chat request returned status %s", resp.Status)
	}

	var decoded chatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return Completion{}, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return Completion{}, fmt.Errorf("no choices returned")
	}
	choice := decoded.Choices[0]
	content := strings.TrimSpace(choice.Message.Content)
	reasoningContent := strings.TrimSpace(choice.Message.ReasoningContent)
	text, source := selectResponseText(opts.Profile, content, reasoningContent)
	return Completion{
		Text:             text,
		Source:           source,
		Content:          content,
		ReasoningContent: reasoningContent,
		FinishReason:     strings.TrimSpace(choice.FinishReason),
		RawResponse:      strings.TrimSpace(string(respBody)),
		Usage:            decoded.Usage,
	}, nil
}

func selectResponseText(profile Profile, content, reasoningContent string) (string, ResponseSource) {
	if strings.TrimSpace(content) != "" {
		return strings.TrimSpace(content), ResponseSourceContent
	}
	if profile == ProfileStructuredControl && strings.TrimSpace(reasoningContent) != "" {
		return strings.TrimSpace(reasoningContent), ResponseSourceReasoningContent
	}
	return "", ResponseSourceEmpty
}
