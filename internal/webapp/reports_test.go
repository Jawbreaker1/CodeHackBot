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

func TestCompletedSessionGeneratesAndRestoresRequestedReports(t *testing.T) {
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		formats := []assessment.ReportFormat{assessment.OWASPReport, assessment.PTESReport}
		format := formats[calls]
		calls++
		answer, _ := json.Marshal(map[string]string{"text": "I prepared the requested format.", "report_format": string(format)})
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": string(answer)}}}, "usage": map[string]int{"total_tokens": 12}})
	}))
	defer model.Close()
	root := t.TempDir()
	config := Config{RepoRoot: root, LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}}
	server := NewServer(config)
	current, err := server.newRun("fixture", "Assess fixture", "192.0.2.1 only")
	if err != nil {
		t.Fatal(err)
	}
	current.status = "completed"
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "completed", Plans: []assessment.Decision{{Complete: true, Summary: "A bounded test found one issue.", Gaps: []string{"Version unknown."}, Findings: []assessment.Finding{{Title: "Plaintext login", Status: "candidate", Severity: "medium", Impact: "Traffic may be observed.", Steps: []string{"Request the page."}, Evidence: []string{"fixture.log"}, Remediation: []string{"Enable TLS."}}}}}}
	if err := atomicWriteJSON(filepath.Join(current.root, "assessment.json"), current.state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current.root, "report.md"), []byte("canonical report"), 0600); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id
	for i, format := range []assessment.ReportFormat{assessment.OWASPReport, assessment.PTESReport} {
		view := postJSON[assessmentView](t, path+"/messages", messageRequest{Text: "Create the " + string(format) + " report"})
		last := view.Messages[len(view.Messages)-1]
		if len(last.Attachments) != 1 || !strings.Contains(last.Attachments[0].URL, "/reports/") || view.PostRunUsage.Calls != i+1 {
			t.Fatalf("post-run report link or usage missing: %+v", view)
		}
		response, err := http.Get(httpServer.URL + last.Attachments[0].URL)
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || !strings.Contains(string(content), "Plaintext login") || !strings.Contains(string(content), "-aligned report") {
			t.Fatalf("%s report response: %d %s", format, response.StatusCode, content)
		}
	}
	canonical, err := os.ReadFile(filepath.Join(current.root, "report.md"))
	if err != nil || string(canonical) != "canonical report" {
		t.Fatalf("canonical report changed: %q %v", canonical, err)
	}
	restored := NewServer(config)
	if restored.loadErr != nil {
		t.Fatal(restored.loadErr)
	}
	view := restored.getRun(current.id).view("")
	if view.PostRunUsage.Calls != 2 || len(view.Messages[len(view.Messages)-1].Attachments) != 1 {
		t.Fatalf("report link or post-run usage lost on restart: %+v", view)
	}
}
