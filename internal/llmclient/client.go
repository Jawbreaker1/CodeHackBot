package llmclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
)

// Client is a minimal OpenAI-compatible chat client for the rebuild path.
type Client struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
	// Empty preserves the provider default; local reasoning models can opt in.
	ReasoningEffort string
	MaxOutputTokens int
	// MaxInputBytes bounds the combined message text before contacting a provider.
	// It is an inspectable memory limit, not an exact provider token count.
	MaxInputBytes int
	// AuthTokenFile is a local bridge credential, never a provider/API token.
	AuthTokenFile string
	// Hooks let one assessment bound and account for all coordinator/worker calls.
	// Implementations must be safe for concurrent callers.
	BeforeRequest func(context.Context) error
	OnCompletion  func(Completion, error)
}

// DefaultInputByteLimit is the conservative fallback used when a provider
// profile does not declare a larger request budget.
const DefaultInputByteLimit = 48 * 1024

// SubscriptionInputByteLimit is the application-side input budget used for
// the subscription bridge. The bridge keeps this separate from local-model
// defaults because Daybreak's verified provider profile has a much larger
// context window.
const SubscriptionInputByteLimit = 128 * 1024

// SubscriptionMaxOutputTokens is passed through the bridge to the Responses
// backend when the subscription profile is active. Provider-side limits still
// win; this is a request ceiling, not a guarantee.
const SubscriptionMaxOutputTokens = 128000

// Message is a chat message.
type Message struct {
	Role             string       `json:"role"`
	Content          string       `json:"content"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	Attachments      []Attachment `json:"-"`
}

// Attachment is a bounded local file supplied with the current user turn.
// Data is held in memory only while the request is prepared; callers persist
// the local source and references separately.
type Attachment struct {
	Filename string
	MIMEType string
	Detail   string
	Data     []byte
}

type wireMessage struct {
	Role             string `json:"role"`
	Content          any    `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type wireContentPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ImageURL *wireImageURL  `json:"image_url,omitempty"`
	File     *wireFileInput `json:"file,omitempty"`
}

type wireImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type wireFileInput struct {
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data"`
	MIMEType string `json:"mime_type,omitempty"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	if len(m.Attachments) == 0 {
		return json.Marshal(wireMessage{Role: m.Role, Content: m.Content, ReasoningContent: m.ReasoningContent})
	}
	parts := []wireContentPart{{Type: "text", Text: m.Content}}
	for _, attachment := range m.Attachments {
		encoded := "data:" + attachment.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data)
		if strings.HasPrefix(strings.ToLower(attachment.MIMEType), "image/") {
			parts = append(parts, wireContentPart{Type: "image_url", ImageURL: &wireImageURL{URL: encoded, Detail: attachment.Detail}})
			continue
		}
		parts = append(parts, wireContentPart{Type: "file", File: &wireFileInput{Filename: attachment.Filename, FileData: encoded, MIMEType: attachment.MIMEType}})
	}
	return json.Marshal(wireMessage{Role: m.Role, Content: parts, ReasoningContent: m.ReasoningContent})
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role             string          `json:"role"`
		Content          json.RawMessage `json:"content"`
		ReasoningContent string          `json:"reasoning_content,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role, m.ReasoningContent = raw.Role, raw.ReasoningContent
	m.Content, m.Attachments = "", nil
	if len(raw.Content) == 0 || string(raw.Content) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw.Content, &m.Content); err == nil {
		return nil
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL    string `json:"url"`
			Detail string `json:"detail"`
		} `json:"image_url"`
		File *struct {
			Filename string `json:"filename"`
			FileData string `json:"file_data"`
			MIMEType string `json:"mime_type"`
		} `json:"file"`
	}
	if err := json.Unmarshal(raw.Content, &parts); err != nil {
		return fmt.Errorf("decode message content: %w", err)
	}
	for _, part := range parts {
		switch part.Type {
		case "text", "input_text":
			m.Content += part.Text
		case "image_url", "input_image":
			if part.ImageURL == nil {
				continue
			}
			attachment, err := attachmentFromDataURL("image", part.ImageURL.URL, part.ImageURL.Detail)
			if err != nil {
				return err
			}
			m.Attachments = append(m.Attachments, attachment)
		case "file", "input_file":
			if part.File == nil {
				continue
			}
			attachment, err := attachmentFromDataURL(part.File.Filename, part.File.FileData, "")
			if err != nil {
				return err
			}
			attachment.Filename, attachment.MIMEType = part.File.Filename, part.File.MIMEType
			m.Attachments = append(m.Attachments, attachment)
		}
	}
	return nil
}

func attachmentFromDataURL(filename, value, detail string) (Attachment, error) {
	const prefix = "data:"
	if !strings.HasPrefix(value, prefix) {
		return Attachment{}, fmt.Errorf("attachment %s is not an inline data URL", filename)
	}
	meta, encoded, ok := strings.Cut(strings.TrimPrefix(value, prefix), ",")
	if !ok {
		return Attachment{}, fmt.Errorf("attachment %s has invalid data URL", filename)
	}
	mimeType := strings.TrimSuffix(strings.SplitN(meta, ";", 2)[0], ";base64")
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Attachment{}, fmt.Errorf("decode attachment %s: %w", filename, err)
	}
	return Attachment{Filename: filename, MIMEType: mimeType, Detail: detail, Data: data}, nil
}

type chatRequest struct {
	Model           string    `json:"model"`
	Messages        []Message `json:"messages"`
	Temperature     float64   `json:"temperature"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	MaxTokens       int       `json:"max_tokens,omitempty"`
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
func (c Client) Complete(ctx context.Context, messages []Message, opts ChatOptions) (completion Completion, completionErr error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return Completion{}, fmt.Errorf("base url is required")
	}
	if strings.TrimSpace(c.Model) == "" {
		return Completion{}, fmt.Errorf("model is required")
	}
	if len(messages) == 0 {
		return Completion{}, fmt.Errorf("messages are required")
	}
	inputBytes := 0
	attachmentBytes := 0
	for _, message := range messages {
		inputBytes += len(message.Content)
		for _, attachment := range message.Attachments {
			attachmentBytes += len(attachment.Data)
		}
	}
	if inputBytes > c.InputByteLimit() {
		return Completion{}, fmt.Errorf("model input is %d bytes; limit is %d; reduce context before retrying", inputBytes, c.InputByteLimit())
	}
	if attachmentBytes > 16<<20 {
		return Completion{}, fmt.Errorf("attachments are %d bytes; limit is %d; reduce attachments before retrying", attachmentBytes, 16<<20)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		timeout := 90 * time.Second
		if c.AuthTokenFile != "" {
			// The subscription adapter may spend up to three minutes waiting
			// for a model response. Do not cancel its caller first.
			timeout = 200 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}

	body, err := json.Marshal(struct {
		Model           string        `json:"model"`
		Messages        []wireMessage `json:"messages"`
		Temperature     float64       `json:"temperature"`
		ReasoningEffort string        `json:"reasoning_effort,omitempty"`
		MaxTokens       int           `json:"max_tokens,omitempty"`
	}{
		Model:           c.Model,
		Messages:        wireMessages(messages),
		Temperature:     0.2,
		ReasoningEffort: c.ReasoningEffort,
		MaxTokens:       c.MaxOutputTokens,
	})
	if err != nil {
		return Completion{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.AuthTokenFile != "" {
		endpoint, err := url.Parse(c.BaseURL)
		if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || !localauth.LoopbackHost(endpoint.Hostname()) {
			return Completion{}, fmt.Errorf("bridge authentication requires an http URL with a literal loopback IP")
		}
		token, err := localauth.Read(c.AuthTokenFile)
		if err != nil {
			return Completion{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		localClient := *httpClient
		localClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		httpClient = &localClient
	}

	if c.BeforeRequest != nil {
		if err := c.BeforeRequest(ctx); err != nil {
			return Completion{}, err
		}
	}
	if c.OnCompletion != nil {
		defer func() { c.OnCompletion(completion, completionErr) }()
	}
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
		if c.AuthTokenFile != "" {
			var failure struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if json.Unmarshal(respBody, &failure) == nil && failure.Error.Type == "subscription_bridge_error" {
				return Completion{}, fmt.Errorf("subscription bridge %s: %s (Retry-After: %s)", resp.Status, failure.Error.Message, resp.Header.Get("Retry-After"))
			}
		}
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
	if choice.FinishReason == "length" || choice.FinishReason == "content_filter" {
		return Completion{FinishReason: choice.FinishReason, Usage: decoded.Usage}, fmt.Errorf("model response incomplete (finish_reason=%s); no decision accepted", choice.FinishReason)
	}
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

func wireMessages(messages []Message) []wireMessage {
	result := make([]wireMessage, 0, len(messages))
	for _, message := range messages {
		if len(message.Attachments) == 0 {
			result = append(result, wireMessage{Role: message.Role, Content: message.Content, ReasoningContent: message.ReasoningContent})
			continue
		}
		data, _ := message.MarshalJSON()
		var wire wireMessage
		_ = json.Unmarshal(data, &wire)
		result = append(result, wire)
	}
	return result
}

func (c Client) InputByteLimit() int {
	if c.MaxInputBytes > 0 {
		return c.MaxInputBytes
	}
	return DefaultInputByteLimit
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
