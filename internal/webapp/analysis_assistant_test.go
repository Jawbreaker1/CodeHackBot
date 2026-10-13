package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestProjectFollowupCanStartWithoutChoosingAnOldSession(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answer := `{"answer":"The saved review has an open question.","sources":["P1"],"next_step":"Check the open question against the saved evidence."}`
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	client := llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}
	project := analysisView{Kind: "customer", ID: "lab", SessionCount: 1, Sessions: []analysisSessionSummary{{ID: "old-run", Goal: "Earlier review"}}}
	reply, _, err := askAnalysisAssistant(context.Background(), client, project, nil, analysisAssistantRequest{Question: "What next?"})
	if err != nil || reply.NextStep == "" || reply.SessionID != "" {
		t.Fatalf("project follow-up should not require an old run: reply=%+v err=%v", reply, err)
	}
	assessment := analysisView{Kind: "assessment", ID: "old-run", SessionCount: 1, Goal: "Earlier review"}
	reply, _, err = askAnalysisAssistant(context.Background(), client, assessment, nil, analysisAssistantRequest{Question: "What next?"})
	if err != nil || reply.NextStep != "" {
		t.Fatalf("assessment continuation should require its run ID: reply=%+v err=%v", reply, err)
	}
}

func TestAnalysisAssistantExplainsTwoSessionsWithSavedSources(t *testing.T) {
	var secondID string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var prompt strings.Builder
		for _, message := range request.Messages {
			prompt.WriteString(message.Content)
		}
		if !strings.Contains(prompt.String(), "Earlier access check") || !strings.Contains(prompt.String(), "Later access check") || !strings.Contains(prompt.String(), "two to four short sentences") {
			t.Errorf("analysis assistant did not receive both saved runs and plain-language guidance: %s", prompt.String())
		}
		answer := `{"answer":"The first run reported an access problem. The second run raised a new question, but did not prove that the first problem was fixed.","sources":["F1","F2"],"next_step":"Review the first access problem and propose a focused retest.","session_id":"` + secondID + `"}`
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()

	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	first, err := server.newRun("shared", "First run", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.newRun("shared", "Second run", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	secondID = second.id
	first.state = assessment.State{ID: first.id, Goal: first.goal, Scope: first.scope, Status: "completed", Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "Earlier access check", Status: "candidate", Severity: "high", Impact: "A user might see someone else's report.", Remediation: []string{"Check access for every report."}}}}}}
	second.state = assessment.State{ID: second.id, Goal: second.goal, Scope: second.scope, Status: "completed", Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "Later access check", Status: "candidate", Severity: "medium", Impact: "The new result still needs confirmation."}}}}}
	first.status, second.status = "completed", "completed"
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/customers/shared/analysis/assistant"
	got := postJSON[analysisAssistantView](t, path, analysisAssistantRequest{Question: "What changed between the runs?"})
	if len(got.Messages) != 2 || len(got.Messages[1].Sources) != 2 || got.Messages[1].SessionID != second.id || got.Messages[1].NextStep == "" {
		t.Fatalf("assistant reply or handoff = %+v", got)
	}
	for _, source := range got.Messages[1].Sources {
		if !strings.Contains(source.URL, "/analysis?customer=shared&finding=") {
			t.Fatalf("finding source does not open its saved record: %+v", source)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "sessions", "shared", analysisAssistantFile)); err != nil || !strings.Contains(string(data), "What changed") {
		t.Fatalf("assistant conversation was not saved: %v", err)
	}
	restored := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	restoredHTTP := httptest.NewServer(restored)
	defer restoredHTTP.Close()
	replayed := getJSON[analysisAssistantView](t, restoredHTTP.URL+"/api/v1/customers/shared/analysis/assistant")
	if len(replayed.Messages) != 2 || replayed.Messages[1].Text != got.Messages[1].Text {
		t.Fatalf("saved conversation was not restored: %+v", replayed)
	}
}

func TestAnalysisAssistantRejectsUnlinkedAnswerAndKeepsConversationEmpty(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answer := `{"answer":"A fabricated result.","sources":["F999"]}`
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	server := NewServer(Config{RepoRoot: t.TempDir(), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	current, err := server.newRun("customer-a", "Inspect fixture", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id + "/analysis/assistant"
	if status := postStatus(t, path, analysisAssistantRequest{Question: "What did we find?"}); status != http.StatusBadGateway {
		t.Fatalf("unlinked answer status = %d", status)
	}
	got := getJSON[analysisAssistantView](t, path)
	if len(got.Messages) != 0 {
		t.Fatalf("invalid answer entered the saved conversation: %+v", got)
	}
}

func TestAnalysisAssistantSnapshotKeepsFocusWhenOtherSourcesAreOmitted(t *testing.T) {
	view := analysisView{Kind: "customer", ID: "lab", SessionCount: 1, Sessions: []analysisSessionSummary{{ID: "run-1", Goal: "Review fixture"}}}
	for i := 0; i < 80; i++ {
		view.Findings = append(view.Findings, analysisFinding{ID: fmt.Sprintf("finding-%d", i), SessionID: "run-1", Title: "Finding with long details", Impact: strings.Repeat("recorded detail ", 90)})
	}
	focus := view.Findings[len(view.Findings)-1].ID
	snapshot, refs, _, err := analysisAssistantSnapshot(view, focus, 8*1024)
	if err != nil || len(snapshot) > 8*1024 || !strings.Contains(snapshot, `"omitted_source_counts"`) || refs["F80"].ID != "F80" {
		t.Fatalf("bounded snapshot lost the selected finding: bytes=%d refs=%d err=%v", len(snapshot), len(refs), err)
	}
}

func TestAnalysisAssistantSnapshotNamesViewScopeWithinLocalModelBudget(t *testing.T) {
	for _, kind := range []string{"assessment", "customer"} {
		view := analysisView{Kind: kind, ID: "run-1", Customer: "lab", SessionCount: 1, Summary: "Saved test summary"}
		if kind == "customer" {
			view.ID = "lab"
			view.Sessions = []analysisSessionSummary{{ID: "run-1", Goal: "Review fixture"}}
		}
		snapshot, _, _, err := analysisAssistantSnapshot(view, "", llmclient.Qwen38LabInputByteLimit/2)
		if err != nil || len(snapshot) > llmclient.Qwen38LabInputByteLimit/2 {
			t.Fatalf("%s packet exceeded local-model budget: bytes=%d err=%v", kind, len(snapshot), err)
		}
		var packet struct {
			ViewKind string `json:"view_kind"`
			ViewID   string `json:"view_id"`
		}
		if err := json.Unmarshal([]byte(snapshot), &packet); err != nil || packet.ViewKind != kind || packet.ViewID != view.ID {
			t.Fatalf("%s packet lost its view scope: %+v err=%v", kind, packet, err)
		}
	}
}
