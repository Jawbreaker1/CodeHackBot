// Package subscription provides inference without delegating an agent loop.
package subscription

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

const responsesURL = "https://chatgpt.com/backend-api/codex/responses"

type Request struct {
	Model     string              `json:"model"`
	Messages  []llmclient.Message `json:"messages"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
	// Accepted for the existing worker protocol; reasoning models use their default.
	Temperature *float64 `json:"temperature,omitempty"`
}

type APIError struct {
	Status              int
	Message, RetryAfter string
}

func (e *APIError) Error() string { return e.Message }

type Provider struct {
	Auth       CredentialSource
	httpClient *http.Client
	endpoint   string
}

func (p *Provider) Complete(ctx context.Context, input Request) (map[string]any, error) {
	instructions := []string{}
	messages := []map[string]string{}
	for _, m := range input.Messages {
		switch m.Role {
		case "system", "developer":
			instructions = append(instructions, m.Content)
		case "user", "assistant":
			messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
		default:
			return nil, &APIError{Status: 400, Message: "only system, developer, user, and assistant text messages are supported"}
		}
	}
	if strings.TrimSpace(input.Model) == "" || len(messages) == 0 {
		return nil, &APIError{Status: 400, Message: "model and conversation messages are required"}
	}
	request := map[string]any{
		"model": input.Model, "instructions": strings.Join(instructions, "\n\n"), "input": messages,
		"store": false, "stream": true, "tools": []any{}, "tool_choice": "none",
	}
	includeOutputLimit := input.MaxTokens > 0
	if input.MaxTokens > 0 {
		request["max_output_tokens"] = input.MaxTokens
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	credentials, err := p.Auth.Read()
	if err != nil {
		return nil, &APIError{Status: 401, Message: err.Error()}
	}
	client := p.httpClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	// Do not follow redirects with account credentials, including same-host ones.
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	endpoint := p.endpoint
	if endpoint == "" {
		endpoint = responsesURL
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
		req.Header.Set("ChatGPT-Account-Id", credentials.AccountID)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("originator", "birdhackbot")
		req.Header.Set("User-Agent", "birdhackbot-subscription/0.1.0")
		resp, err := safeClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			credentials, err = p.Auth.Refresh(ctx, credentials)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, &APIError{Status: 401, Message: "subscription authentication expired; refresh failed; sign in with codex login again"}
			}
			continue
		}
		// Some subscription backend revisions reject the optional output limit
		// even though they accept the same model and messages. Retry once without
		// that optional field; the backend then owns its configured output ceiling.
		if resp.StatusCode == http.StatusBadRequest && includeOutputLimit {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			delete(request, "max_output_tokens")
			body, err = json.Marshal(request)
			if err != nil {
				return nil, err
			}
			includeOutputLimit = false
			attempt--
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, upstreamError(resp.StatusCode, resp.Header.Get("Retry-After"))
		}
		return readCompletion(resp.Body, input.Model)
	}
	return nil, errors.New("subscription authentication failed")
}

func upstreamError(status int, retryAfter string) error {
	message := "subscription backend request failed"
	switch status {
	case 400:
		message = "subscription backend rejected the model or request"
	case 401:
		message = "subscription authentication rejected; sign in with codex login again"
	case 403:
		message = "subscription account does not have access to this request or model"
	case 429:
		message = "subscription usage limit reached; wait for the account limit to reset"
	default:
		if status < 400 || status > 599 {
			status = 502
		}
	}
	return &APIError{Status: status, Message: message, RetryAfter: retryAfter}
}

type outputItem struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func readCompletion(reader io.Reader, model string) (map[string]any, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, 16<<20))
	scanner.Buffer(make([]byte, 4096), 16<<20)
	var data []string
	var completedItems []outputItem
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		if line != "" || len(data) == 0 {
			continue
		}
		payload := strings.Join(data, "\n")
		data = nil
		if payload == "[DONE]" {
			break
		}
		var event struct {
			Type     string     `json:"type"`
			Item     outputItem `json:"item"`
			Response struct {
				ID     string       `json:"id"`
				Model  string       `json:"model"`
				Status string       `json:"status"`
				Output []outputItem `json:"output"`
				Usage  struct {
					Input  int `json:"input_tokens"`
					Output int `json:"output_tokens"`
					Total  int `json:"total_tokens"`
				} `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(payload), &event) != nil {
			return nil, fmt.Errorf("invalid subscription stream event")
		}
		switch event.Type {
		case "response.output_item.done":
			completedItems = append(completedItems, event.Item)
		case "error", "response.failed", "response.incomplete":
			return nil, fmt.Errorf("subscription response failed or incomplete")
		case "response.completed":
			if event.Response.Status != "completed" {
				return nil, fmt.Errorf("subscription response did not complete")
			}
			var text strings.Builder
			items := event.Response.Output
			// Codex may omit output from the terminal event after sending each
			// completed item. Never promote unfinished deltas to a completion.
			if len(items) == 0 {
				items = completedItems
			}
			for _, item := range items {
				if item.Type == "reasoning" {
					continue
				}
				if item.Type != "message" || item.Role != "assistant" {
					return nil, fmt.Errorf("unexpected non-message output from inference backend")
				}
				for _, content := range item.Content {
					if content.Type != "output_text" {
						return nil, fmt.Errorf("subscription backend returned no usable text (possibly a refusal)")
					}
					text.WriteString(content.Text)
				}
			}
			if strings.TrimSpace(text.String()) == "" {
				return nil, fmt.Errorf("subscription backend returned empty output")
			}
			if event.Response.Model != "" {
				model = event.Response.Model
			}
			return map[string]any{
				"id": event.Response.ID, "object": "chat.completion", "created": time.Now().Unix(), "model": model,
				"choices": []any{map[string]any{"index": 0, "message": llmclient.Message{Role: "assistant", Content: text.String()}, "finish_reason": "stop"}},
				"usage":   map[string]int{"prompt_tokens": event.Response.Usage.Input, "completion_tokens": event.Response.Usage.Output, "total_tokens": event.Response.Usage.Total},
			}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("subscription stream ended without a completed response")
}
