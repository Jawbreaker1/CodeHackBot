package llmclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
