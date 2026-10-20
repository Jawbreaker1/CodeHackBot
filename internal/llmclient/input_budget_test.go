package llmclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInputPressureMeasuresCompleteRequestAgainstBothLimits(t *testing.T) {
	count := func(messages []Message) int {
		if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" || len(messages[1].Attachments) != 1 {
			t.Fatal("preflight counter did not receive the complete multimodal request")
		}
		return 90
	}
	client := Client{MaxInputBytes: 1000, MaxInputTokens: 100, CountInputTokens: count}
	messages := []Message{{Role: "system", Content: "policy"}, {Role: "user", Content: "inspect", Attachments: []Attachment{{MIMEType: "image/png", Data: []byte("fixture")}}}}
	pressure, err := client.InputPressure(messages)
	if err != nil || pressure != 900000 {
		t.Fatalf("token pressure = %d, %v", pressure, err)
	}
	client.MaxInputBytes = 10
	pressure, err = client.InputPressure(messages)
	if err != nil || pressure != 1_300_000 {
		t.Fatalf("byte pressure = %d, %v", pressure, err)
	}
	client.CountInputTokens = nil
	if _, err := client.InputPressure(messages); err == nil {
		t.Fatal("configured token budget silently fell back to bytes")
	}
}

func TestClientRejectsOverTokenBudgetBeforeProviderRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Model: "fixture", MaxInputBytes: 1000, MaxInputTokens: 10,
		CountInputTokens: func(messages []Message) int { return len(strings.Fields(messages[0].Content)) }}
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: strings.Repeat("word ", 11)}}); err == nil || calls != 0 {
		t.Fatalf("over-budget request reached provider: calls=%d err=%v", calls, err)
	}
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "word word"}}); err != nil || calls != 1 {
		t.Fatalf("in-budget request failed: calls=%d err=%v", calls, err)
	}
}
