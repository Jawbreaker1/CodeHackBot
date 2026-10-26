package guided

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestCLIActiveCoordinatorCanSearchPublicSources(t *testing.T) {
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"title": "Vendor", "url": "https://www.php.net/releases/", "content": "Candidate only"}}})
	}))
	defer search.Close()
	modelCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		answer := `{"tool":{"name":"web_search","query":"PHP release security"}}`
		if modelCalls == 2 {
			answer = `{"text":"I found a vendor source, but its contents have not been read yet."}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	inspection := &intake.Inspection{Workspace: t.TempDir(), EvidenceDir: t.TempDir(), Connected: true, WebSearchURL: search.URL, Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce}}
	reply, err := coordinatorChatWithResearch(t.Context(), llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, behavior.Frame{}, []byte(`{"status":"running"}`), nil, "What changed in PHP?", inspection)
	if err != nil || modelCalls != 2 || !strings.Contains(reply, "not been read yet") {
		t.Fatalf("reply=%q calls=%d err=%v", reply, modelCalls, err)
	}
}

func TestCLIResearchCorrectsOneNonJSONControlReply(t *testing.T) {
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"title": "Vendor", "url": "https://www.php.net/", "content": "Candidate"}}})
	}))
	defer search.Close()
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		answers := []string{"I should search for a source first.", `{"tool":{"name":"web_search","query":"PHP release"}}`, `{"text":"I found a candidate; its contents have not been read."}`}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answers[calls-1]}}}})
	}))
	defer model.Close()
	inspection := &intake.Inspection{Workspace: t.TempDir(), EvidenceDir: t.TempDir(), Connected: true, WebSearchURL: search.URL, Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce}}
	reply, err := coordinatorChatWithResearch(t.Context(), llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, behavior.Frame{}, []byte(`{"status":"running"}`), nil, "Search for a PHP release", inspection)
	if err != nil || calls != 3 || !strings.Contains(reply, "not been read") {
		t.Fatalf("reply=%q calls=%d err=%v", reply, calls, err)
	}
}
