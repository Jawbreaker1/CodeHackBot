package webapp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestAnalysisAssistantCreatesAndRevisesProjectReportWithoutChangingResults(t *testing.T) {
	var editCalls int
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		prompt := ""
		for _, message := range request.Messages {
			prompt += message.Content
		}
		answer := `{"answer":"Created the OWASP report draft.","sources":["P1"],"report_request":{"format":"owasp-wstg","output":"markdown"}}`
		if strings.Contains(prompt, analysisReportRevisionSystem) {
			editCalls++
			if !strings.Contains(prompt, "First finding") || !strings.Contains(prompt, "Second finding") {
				t.Errorf("report editor lost saved findings or previous report: %s", prompt)
			}
			answer = `{"markdown":"# Revised OWASP report\n\n## Executive summary\n\nTwo saved finding records need review.\n\n## Findings\n\nFirst finding and Second finding remain recorded.\n","sources":["P1"]}`
		} else if strings.Contains(prompt, "Revise") {
			answer = `{"answer":"Created a revised draft.","sources":["P1"],"report_request":{"format":"owasp-wstg","output":"markdown","use_latest":true,"revise":true}}`
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()

	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	first, err := server.newRun("shared", "First assessment", "first scoped target")
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.newRun("shared", "Second assessment", "second scoped target")
	if err != nil {
		t.Fatal(err)
	}
	first.state = assessment.State{ID: first.id, Goal: first.goal, Scope: first.scope, Status: "completed", Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "First finding", Status: "candidate", Impact: "First impact", Evidence: []string{"first-evidence"}, Remediation: []string{"First correction"}}}}}}
	second.state = assessment.State{ID: second.id, Goal: second.goal, Scope: second.scope, Status: "completed", Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "Second finding", Status: "candidate", Impact: "Second impact", Evidence: []string{"second-evidence"}, Remediation: []string{"Second correction"}}}}}}
	first.status, second.status = "completed", "completed"
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/customers/shared/analysis/assistant"
	created := postJSON[analysisAssistantView](t, path, analysisAssistantRequest{Question: "Create an OWASP report in Markdown"})
	if len(created.Messages) != 2 || created.Messages[1].Report == nil {
		t.Fatalf("created report was not linked in the chat: %+v", created)
	}
	firstLink := created.Messages[1].Report
	firstDraft := readReportLink(t, httpServer.URL+firstLink.URL)
	for _, expected := range []string{first.id, second.id, "First finding", "Second finding", "First correction", "Second correction"} {
		if !strings.Contains(firstDraft, expected) {
			t.Fatalf("project report omitted %q: %s", expected, firstDraft)
		}
	}
	revised := postJSON[analysisAssistantView](t, path, analysisAssistantRequest{Question: "Revise the report with a shorter executive summary"})
	if len(revised.Messages) != 4 || revised.Messages[3].Report == nil || !revised.Messages[3].Report.Revised || editCalls != 1 {
		t.Fatalf("revision did not produce a new draft: %+v, edit calls %d", revised, editCalls)
	}
	if revised.Messages[3].Report.URL == firstLink.URL || !strings.Contains(readReportLink(t, httpServer.URL+revised.Messages[3].Report.URL), "# Revised OWASP report") {
		t.Fatalf("revision did not create a separate report version: %+v", revised.Messages[3].Report)
	}
	if again := readReportLink(t, httpServer.URL+firstLink.URL); again != firstDraft {
		t.Fatal("revising a report changed the earlier version")
	}
	if len(first.state.Plans[0].Findings) != 1 || len(second.state.Plans[0].Findings) != 1 {
		t.Fatal("report editing changed recorded findings")
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", "shared", "reports", revised.Messages[3].Report.MarkdownName)); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisAssistantUsesAssessmentTemplateForOneRun(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answer := `{"answer":"Created the PTES report draft.","sources":["P1"],"report_request":{"format":"ptes","output":"markdown"}}`
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	first, err := server.newRun("shared", "One assessment", "one target")
	if err != nil {
		t.Fatal(err)
	}
	other, err := server.newRun("shared", "Another assessment", "another target")
	if err != nil {
		t.Fatal(err)
	}
	first.state = assessment.State{ID: first.id, Goal: first.goal, Scope: first.scope, Status: "completed"}
	other.state = assessment.State{ID: other.id, Goal: other.goal, Scope: other.scope, Status: "completed"}
	first.status, other.status = "completed", "completed"
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + first.id + "/analysis/assistant"
	response := postJSON[analysisAssistantView](t, path, analysisAssistantRequest{Question: "Create a PTES report"})
	if len(response.Messages) != 2 || response.Messages[1].Report == nil {
		t.Fatalf("missing assessment report: %+v", response)
	}
	draft := readReportLink(t, httpServer.URL+response.Messages[1].Report.URL)
	if !strings.Contains(draft, "One assessment") || strings.Contains(draft, other.id) || strings.Contains(draft, "Another assessment") || !strings.Contains(strings.ToLower(draft), "ptes") {
		t.Fatalf("assessment report scope or template is wrong: %s", draft)
	}
}

func readReportLink(t *testing.T, link string) string {
	t.Helper()
	response, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("report link %s: status %d, err %v", link, response.StatusCode, err)
	}
	return string(data)
}
