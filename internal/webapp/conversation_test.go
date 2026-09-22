package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestCoordinatorChatReceivesEvidenceBeforeWorkerCompletes(t *testing.T) {
	modelInput := make(chan string, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		modelInput <- request.Messages[len(request.Messages)-1].Content
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "The worker has captured evidence and is still evaluating it."}}}})
	}))
	defer model.Close()
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	current, err := server.newRun("fixture", "verify the local fixture", "local synthetic fixture only")
	if err != nil {
		t.Fatal(err)
	}
	current.started, current.status = true, "running"
	current.state = assessment.State{Status: "running", Goal: current.goal, Scope: current.scope}
	current.updateWorker(assessment.Event{TaskID: "browser", Kind: "post_exec_eval_started", EvidenceCount: 1, Evidence: &assessment.EvidenceView{
		Command: strings.Repeat("command ", 300), ExitStatus: "0", Summary: "Actual browser interaction completed", ArtifactRefs: []string{"work/result.png"}, LogRefs: []string{"logs/run.log"},
	}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	view := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+current.id+"/messages", messageRequest{Text: "What has happened?"})
	input := <-modelInput
	for _, want := range []string{`"live_workers"`, `"phase":"post_exec_eval_started"`, `"evidence_count":1`, `"latest_evidence"`, "Actual browser interaction completed", "work/result.png", "logs/run.log"} {
		if !strings.Contains(input, want) {
			t.Errorf("model input missing %s: %s", want, input)
		}
	}
	if strings.Contains(input, strings.Repeat("command ", 100)) {
		t.Fatal("live command was not bounded")
	}
	if len(view.Results) != 0 || len(view.Messages) != 2 {
		t.Fatalf("live evidence should not manufacture a completed result: %+v", view)
	}
	if len(current.workers["browser"].Evidence[0].ArtifactURLs) != 0 {
		t.Fatal("view mutated the recorded worker evidence")
	}
}

func TestAnalysisAndRestoredChatUseLatestCoordinatorDecision(t *testing.T) {
	root := t.TempDir()
	config := Config{RepoRoot: root}
	server := NewServer(config)
	current, err := server.newRun("fixture", "verify browser interaction", "synthetic only")
	if err != nil {
		t.Fatal(err)
	}
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "completed", Plans: []assessment.Decision{
		{Summary: "Awaiting execution", Gaps: []string{"Interaction not executed yet"}},
		{Summary: "Interaction verified with PNG and trace evidence", Complete: true},
	}}
	current.status = "completed"
	if err := atomicWriteJSON(filepath.Join(current.root, "assessment.json"), current.state); err != nil {
		t.Fatal(err)
	}
	if err := current.persist(); err != nil {
		t.Fatal(err)
	}
	restored := NewServer(config)
	if restored.loadErr != nil {
		t.Fatal(restored.loadErr)
	}
	httpServer := httptest.NewServer(restored)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id
	view := getJSON[assessmentView](t, path)
	analysis := getJSON[analysisView](t, path+"/analysis")
	if view.Conclusion != current.state.Plans[1].Summary || analysis.Conclusion != view.Conclusion {
		t.Fatalf("conclusion missing after restore: chat=%q analysis=%q", view.Conclusion, analysis.Conclusion)
	}
	if len(analysis.Gaps) != 0 || len(analysis.NextActions) != 0 {
		t.Fatalf("resolved gap leaked into current analysis: %+v", analysis)
	}
	if len(restored.getRun(current.id).state.Plans[0].Gaps) != 1 {
		t.Fatal("historical gap was removed from the audit record")
	}
}
