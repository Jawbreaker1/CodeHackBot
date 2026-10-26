package intake

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestIntakeSearchReadsSourceBeforeAnswering(t *testing.T) {
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("q") != "PHP release security" {
			t.Errorf("unexpected search request %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
			{"title": "PHP release notes", "url": "https://www.php.net/releases/", "content": "Search lead only"},
			{"title": "Private link", "url": "http://127.0.0.1/private", "content": "Must not reach model"},
		}})
	}))
	defer search.Close()
	modelCalls, pageReads := 0, 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		packet, _ := json.Marshal(request.Messages)
		answer := `{"reply":"","proposal":null,"tool":{"name":"web_search","query":"PHP release security"}}`
		switch modelCalls {
		case 1:
			if !strings.Contains(string(packet), "web_search(query)") {
				t.Error("model was not offered web search")
			}
		case 2:
			if !strings.Contains(string(packet), "https://www.php.net/releases/") || strings.Contains(string(packet), "Must not reach model") {
				t.Error("search candidates were not filtered")
			}
			answer = `{"reply":"","proposal":null,"tool":{"name":"web_fetch","url":"https://www.php.net/releases/"}}`
		case 3:
			if !strings.Contains(string(packet), "Verified page text from vendor") {
				t.Error("model did not receive the read page")
			}
			answer = `{"reply":"The PHP project describes its release at https://www.php.net/releases/. This alone does not show whether the target uses it.","proposal":null,"tool":null}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	evidence := t.TempDir()
	inspection := &Inspection{Workspace: t.TempDir(), EvidenceDir: evidence, Connected: true, WebSearchURL: search.URL, Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce}}
	inspection.fetchPage = func(_ context.Context, url string) (pageObservation, error) {
		pageReads++
		if url != "https://www.php.net/releases/" {
			t.Errorf("unexpected page read %s", url)
		}
		return pageObservation{URL: url, Status: 200, BodyExcerpt: "Verified page text from vendor", ObservedAt: time.Now().UTC()}, nil
	}
	conversation := &Conversation{Inspection: inspection}
	turn, err := conversation.Turn(t.Context(), llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, "Why are PHP issues in the news?")
	if err != nil || modelCalls != 3 || pageReads != 1 || !strings.Contains(turn.Reply, "This alone does not show") {
		t.Fatalf("turn=%+v modelCalls=%d pageReads=%d err=%v", turn, modelCalls, pageReads, err)
	}
	files, err := os.ReadDir(evidence)
	if err != nil || len(files) != 2 {
		t.Fatalf("expected one search and one page evidence file, got %v: %v", files, err)
	}
}

func TestWebSearchUnavailableWhenOfflineOrUnconfigured(t *testing.T) {
	for _, inspection := range []Inspection{
		{Workspace: t.TempDir(), EvidenceDir: t.TempDir(), WebSearchURL: "http://127.0.0.1:8888"},
		{Workspace: t.TempDir(), EvidenceDir: t.TempDir(), Connected: true},
	} {
		result, err := inspection.Run(t.Context(), ToolCall{Name: "web_search", Query: "PHP security"})
		if err != nil || result.Status != "failed" || result.Error == "" || result.EvidenceRef != "" {
			t.Fatalf("unexpected unavailable result: %+v err=%v", result, err)
		}
	}
}
