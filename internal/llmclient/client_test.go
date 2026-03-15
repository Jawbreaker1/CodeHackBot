package llmclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
