package webapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestSearxNGSearchReturnsPublicCandidates(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("q") != "PHP security news" || r.URL.Query().Get("format") != "json" {
			t.Errorf("unexpected search request: %s", r.URL)
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": []map[string]string{
			{"title": "Vendor advisory", "url": "https://www.php.net/", "content": "A recent update"},
			{"title": "Private host", "url": "http://127.0.0.1/private", "content": "Skip"},
			{"title": "Tracked link", "url": "https://example.org/?token=secret", "content": "Skip"},
			{"title": "Duplicate", "url": "https://www.php.net/", "content": "Skip"},
		}})
	}))
	defer service.Close()
	hits, err := (searxNGSearch{baseURL: service.URL}).SearchWeb(context.Background(), "PHP security news")
	if err != nil || len(hits) != 1 || hits[0].URL != "https://www.php.net/" {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
}

type fakeAnalysisWebSearcher func(context.Context, string) ([]analysisWebHit, error)

func (f fakeAnalysisWebSearcher) SearchWeb(ctx context.Context, query string) ([]analysisWebHit, error) {
	return f(ctx, query)
}

func TestAnalysisWebSearchReadsSelectedPageBeforeCiting(t *testing.T) {
	modelCalls, pageReads := 0, 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		answer := `{"web_search_query":"PHP security news"}`
		if modelCalls == 2 {
			answer = `{"research_urls":["https://www.php.net/","https://not-returned.example/"]}`
		}
		if modelCalls == 3 {
			prompt := ""
			for _, message := range request.Messages {
				prompt += message.Content
			}
			if !strings.Contains(prompt, "Vendor advisory content") || strings.Contains(prompt, "not-returned.example") {
				t.Errorf("final model prompt did not use verified page content only")
			}
			answer = `{"answer":"The vendor published a security update. This assessment does not show whether the target uses the affected version.","sources":["W1","P1"]}`
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	researcher := publicPageResearcher{
		webSearch: fakeAnalysisWebSearcher(func(_ context.Context, query string) ([]analysisWebHit, error) {
			if query != "PHP security news" {
				t.Errorf("wrong query: %s", query)
			}
			return []analysisWebHit{{Title: "PHP security", URL: "https://www.php.net/", Snippet: "Candidate only"}}, nil
		}),
		fetch: func(_ context.Context, raw string) (intake.ResearchPage, error) {
			pageReads++
			if raw != "https://www.php.net/" {
				t.Errorf("unreturned URL was fetched: %s", raw)
			}
			return intake.ResearchPage{URL: raw, Status: 200, Excerpt: "Vendor advisory content", ObservedAt: time.Now().UTC()}, nil
		},
	}
	answer, _, err := askAnalysisAssistantWithResearch(context.Background(), llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, analysisView{Kind: "assessment", ID: "run-1", SessionCount: 1}, nil, analysisAssistantRequest{Question: "Why are people worried about PHP issues lately?"}, researcher)
	if err != nil || modelCalls != 3 || pageReads != 1 || len(answer.Sources) != 2 || answer.Sources[0].URL != "https://www.php.net/" {
		t.Fatalf("answer=%+v calls=%d reads=%d err=%v", answer, modelCalls, pageReads, err)
	}
}
