package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestActiveCoordinatorSearchUsesRecordedObservation(t *testing.T) {
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "PHP release security" {
			t.Errorf("unexpected search query: %s", r.URL)
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": []map[string]string{{"title": "PHP release notes", "url": "https://www.php.net/releases/", "content": "Lead only"}}})
	}))
	defer search.Close()
	modelCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		answer := `{"tool":{"name":"web_search","query":"PHP release security"}}`
		if modelCalls == 2 {
			packet, _ := json.Marshal(request.Messages)
			if !strings.Contains(string(packet), "https://www.php.net/releases/") {
				t.Error("search observation did not reach coordinator")
			}
			answer = `{"text":"I found a vendor page to read, but search results alone do not establish which version this target runs."}`
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	server := NewServer(Config{RepoRoot: t.TempDir(), WebSearchURL: search.URL, LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	current, err := server.newRun("fixture", "inspect fixture", "synthetic only")
	if err != nil {
		t.Fatal(err)
	}
	current.started, current.status, current.permissionMode = true, "running", approval.FullAccess
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "running", Limits: assessment.DefaultLimits()}
	if err := server.message(t.Context(), current, "Why are PHP issues in the news?", nil); err != nil {
		t.Fatal(err)
	}
	if modelCalls != 2 || len(current.messages) != 2 || !strings.Contains(current.messages[1].Text, "search results alone") {
		t.Fatalf("unexpected coordinator conversation: calls=%d messages=%+v", modelCalls, current.messages)
	}
	files, err := os.ReadDir(filepath.Join(current.root, "coordinator-research"))
	if err != nil || len(files) != 1 {
		t.Fatalf("search evidence missing: %v %v", files, err)
	}
}
