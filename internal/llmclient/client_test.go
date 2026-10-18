package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMessageAttachmentsUseMultimodalWireContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content []struct {
					Type     string `json:"type"`
					ImageURL *struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Messages) != 1 || len(body.Messages[0].Content) != 2 || body.Messages[0].Content[1].Type != "image_url" || !strings.HasPrefix(body.Messages[0].Content[1].ImageURL.URL, "data:image/png;base64,") {
			t.Fatalf("unexpected multimodal request: %+v", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"seen"}}]}`))
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Model: "vision", HTTPClient: server.Client()}
	got, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "inspect", Attachments: []Attachment{{Filename: "screen.png", MIMEType: "image/png", Data: bytes.Repeat([]byte{0x01}, 4)}}}})
	if err != nil || got != "seen" {
		t.Fatalf("Chat() = %q, %v", got, err)
	}
}

func TestRequestLimitAndIncompleteControlOutput(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"action\",\"command\":\"pwd\"}"},"finish_reason":"length"}]}`))
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Model: "fixture", MaxInputBytes: 32}
	if _, err := client.ChatStructured(context.Background(), []Message{{Role: "user", Content: strings.Repeat("x", 33)}}); err == nil || calls != 0 {
		t.Fatal("oversized request sent")
	}
	if _, err := client.ChatStructured(context.Background(), []Message{{Role: "user", Content: "small"}}); err == nil || calls != 1 {
		t.Fatal("truncated decision accepted")
	}
}

func TestStructuredChatRetriesOneTransientFailureAndAccountsForBothRequests(t *testing.T) {
	requests, reserved, completed := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"reply\":\"ready\"}"}}]}`))
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Model: "fixture"}
	client.BeforeRequest = func(context.Context) error { reserved++; return nil }
	client.OnCompletion = func(Completion, error) { completed++ }
	answer, err := client.ChatStructured(context.Background(), []Message{{Role: "user", Content: "decide"}})
	if err != nil || answer != `{"reply":"ready"}` || requests != 2 || reserved != 2 || completed != 2 {
		t.Fatalf("answer=%q err=%v requests=%d reserved=%d completed=%d", answer, err, requests, reserved, completed)
	}
}

func TestStructuredCompletionPreservesProviderInputTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ready"}}],"usage":{"prompt_tokens":42,"completion_tokens":3,"total_tokens":45}}`))
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Model: "fixture"}
	completion, err := client.ChatStructuredCompletion(context.Background(), []Message{{Role: "user", Content: "decide"}})
	if err != nil || completion.Text != "ready" || completion.PromptTokens() != 42 {
		t.Fatalf("completion=%+v err=%v", completion, err)
	}
	for _, raw := range []json.RawMessage{nil, []byte(`{}`), []byte(`{"prompt_tokens":-1}`), []byte(`{"prompt_tokens":"unknown"}`)} {
		if got := (Completion{Usage: raw}).PromptTokens(); got != 0 {
			t.Fatalf("unknown usage %s yielded %d tokens", raw, got)
		}
	}
}

func TestStructuredChatDoesNotRetryBadRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Model: "fixture"}
	if _, err := client.ChatStructured(context.Background(), []Message{{Role: "user", Content: "decide"}}); err == nil || requests != 1 {
		t.Fatalf("bad request retried: err=%v requests=%d", err, requests)
	}
}

func TestReasoningEffortIsExplicitAndOptional(t *testing.T) {
	for _, effort := range []string{"", "low"} {
		t.Run("effort="+effort, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				field, present := body["reasoning_effort"]
				if effort == "" && present || effort != "" && string(field) != `"low"` {
					t.Errorf("reasoning_effort = %s, configured %q", field, effort)
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
			}))
			defer server.Close()
			client := Client{BaseURL: server.URL, Model: "test", ReasoningEffort: effort}
			if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStructuredJSONAppliesOnlyToControlCalls(t *testing.T) {
	seen := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ResponseFormat struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Name   string `json:"name"`
					Schema struct {
						Type string `json:"type"`
					} `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		seen = append(seen, body.ResponseFormat.Type)
		if body.ResponseFormat.Type != "" && (body.ResponseFormat.JSONSchema.Name != "birdhackbot_control" || body.ResponseFormat.JSONSchema.Schema.Type != "object") {
			t.Errorf("incomplete JSON schema request: %+v", body.ResponseFormat)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"reply\":\"ready\"}"}}]}`))
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Model: "local", StructuredJSON: true}
	if _, err := client.ChatStructured(context.Background(), []Message{{Role: "user", Content: "decide"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "talk"}}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "json_schema" || seen[1] != "" {
		t.Fatalf("response formats = %v", seen)
	}
}

func TestClientChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"type\":\"step_complete\",\"summary\":\"done\"}"}}]}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()}
	got, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	want := `{"type":"step_complete","summary":"done"}`
	if got != want {
		t.Fatalf("Chat() = %q, want %q", got, want)
	}
}

func TestClientChatRejectsReasoningOnlyConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"hidden control text"}}]}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()}
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err == nil {
		t.Fatal("Chat() error = nil, want reasoning-only conversation error")
	}
	if !strings.Contains(err.Error(), "reasoning_content only") {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestClientChatStructuredFallsBackToReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"{\"mode\":\"direct_execution\",\"reason\":\"file inspection\"}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()}
	got, err := client.ChatStructured(context.Background(), []Message{{Role: "user", Content: "classify"}})
	if err != nil {
		t.Fatalf("ChatStructured() error = %v", err)
	}
	want := `{"mode":"direct_execution","reason":"file inspection"}`
	if got != want {
		t.Fatalf("ChatStructured() = %q, want %q", got, want)
	}
}

func TestClientCompletePrefersContentForStructuredControl(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"mode\":\"conversation\",\"reason\":\"final answer\"}","reasoning_content":"{\"mode\":\"direct_execution\",\"reason\":\"reasoning artifact\"}"},"finish_reason":"stop"}],"usage":{"total_tokens":10}}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()}
	got, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "classify"}}, ChatOptions{Profile: ProfileStructuredControl})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	want := `{"mode":"conversation","reason":"final answer"}`
	if got.Text != want {
		t.Fatalf("Completion.Text = %q, want %q", got.Text, want)
	}
	if got.Source != ResponseSourceContent {
		t.Fatalf("Completion.Source = %q, want %q", got.Source, ResponseSourceContent)
	}
	if got.ReasoningContent == "" {
		t.Fatal("Completion.ReasoningContent should preserve provider field")
	}
	if got.FinishReason != "stop" {
		t.Fatalf("Completion.FinishReason = %q", got.FinishReason)
	}
	if len(got.Usage) == 0 {
		t.Fatal("Completion.Usage should preserve usage metadata")
	}
}
