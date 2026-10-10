package intake

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestTurnCorrectsMalformedControlOnceWithoutCommittingIt(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if calls == 2 && (len(request.Messages) != 3 || !strings.Contains(request.Messages[2].Content, "exactly one valid JSON object")) {
			t.Errorf("correction request = %+v", request.Messages)
		}
		response := `{"reply":"first"},{"reply":"second"}`
		if calls == 2 {
			response = `{"reply":"I can assess this lab.","proposal":null,"tool":null}`
		}
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": response}}}})
		_, _ = w.Write(body)
	}))
	defer server.Close()
	conversation := &Conversation{}
	turn, err := conversation.Turn(context.Background(), llmclient.Client{BaseURL: server.URL, Model: "fixture"}, "Assess the local lab")
	if err != nil || turn.Reply != "I can assess this lab." || calls != 2 {
		t.Fatalf("turn=%+v err=%v calls=%d", turn, err, calls)
	}
	if messages := conversation.Messages(); len(messages) != 2 || messages[0].Content != "Assess the local lab" || strings.Contains(messages[1].Content, "first") {
		t.Fatalf("unexpected committed messages: %+v", messages)
	}
}

func TestDecodeTurnRequiresExactProtocol(t *testing.T) {
	turn, err := DecodeTurn(`{"reply":"I coordinate authorized assessments.","proposal":null}`)
	if err != nil || turn.Reply == "" || turn.Proposal != nil {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	if _, err := DecodeTurn("I coordinate authorized assessments."); err == nil {
		t.Fatal("plain text must not be treated as a control response")
	}
	if _, err := DecodeTurn(`{"reply":"ready","proposal":{"goal":"","scope":"target"}}`); err == nil {
		t.Fatal("incomplete proposal must be rejected")
	}
	turn, err = DecodeTurn(`{"reply":"I will inspect the workspace entries first.","proposal":null,"tool":{"name":"list_directory","path":"."}}`)
	if err != nil || turn.Tool == nil || turn.Tool.Name != "list_directory" {
		t.Fatalf("tool turn=%+v err=%v", turn, err)
	}
	turn, err = DecodeTurn(`{"reply":"I will inspect fixed host metadata.","proposal":null,"tool":{"name":"host_system"}}`)
	if err != nil || turn.Tool == nil || turn.Tool.Name != "host_system" {
		t.Fatalf("host tool turn=%+v err=%v", turn, err)
	}
	turn, err = DecodeTurn(`{"reply":"","proposal":null,"tool":{"name":"web_fetch","url":"https://example.com/"}}`)
	if err != nil || turn.Tool == nil || turn.Tool.Name != "web_fetch" {
		t.Fatalf("tool call without interim prose=%+v err=%v", turn, err)
	}
	if _, err := DecodeTurn(`{"reply":"","proposal":null,"tool":null}`); err == nil {
		t.Fatal("final response without a reply must be rejected")
	}
	if _, err := DecodeTurn(`{"reply":"choose one","proposal":{"goal":"check","scope":"lab"},"tool":{"name":"local_network"}}`); err == nil {
		t.Fatal("tool and proposal must not be combined")
	}
}

func TestDecodeTurnAcceptsEstimatedInvestigationApproaches(t *testing.T) {
	raw := `{"reply":"These are preliminary estimates.","proposal":{"goal":"inspect fixture","scope":"synthetic only","approaches":[{"id":"focused","label":"Focused","description":"One relevant check","estimate":"10–20 minutes"},{"id":"balanced","label":"Balanced","description":"Check observed surfaces","estimate":"30–60 minutes"},{"id":"thorough","label":"Thorough","description":"Follow more leads","estimate":"1–2 hours"}]}}`
	turn, err := DecodeTurn(raw)
	if err != nil || turn.Proposal == nil || len(turn.Proposal.Approaches) != 3 || turn.Proposal.Approaches[1].ID != "balanced" {
		t.Fatalf("estimated choices were lost: turn=%+v err=%v", turn, err)
	}
}
