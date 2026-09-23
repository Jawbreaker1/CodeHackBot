package webapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestStoppingAssessmentCancelsLiveCoordinatorChat(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
	}))
	defer model.Close()
	defer close(release)
	runCtx, cancelRun := context.WithCancelCause(context.Background())
	defer cancelRun(context.Canceled)
	server := NewServer(Config{RepoRoot: t.TempDir(), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	current, err := server.newRun("fixture", "inspect fixture", "synthetic only")
	if err != nil {
		t.Fatal(err)
	}
	current.started, current.status, current.runCtx = true, "running", runCtx
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "running"}
	result := make(chan error, 1)
	go func() { result <- server.message(context.Background(), current, "Explain the current status", nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("live chat request did not reach the provider")
	}
	cancelRun(context.Canceled)
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("live chat cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stopped assessment left live chat running")
	}
	if got := current.budget.Usage(); got.Calls != 1 || got.FailedCalls != 1 {
		t.Fatalf("canceled chat usage = %+v", got)
	}
}

func TestCoordinatorCanPresentRecordedImageInChat(t *testing.T) {
	var imagePath, unregisteredPath string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var input strings.Builder
		for _, message := range request.Messages {
			input.WriteString(message.Content)
		}
		if !strings.Contains(input.String(), imagePath) {
			t.Error("recorded image was not offered to the coordinator")
		}
		answer, _ := json.Marshal(map[string]any{"text": "Here is the recorded screenshot.", "display_artifact_refs": []string{imagePath, unregisteredPath, "/etc/passwd"}})
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": string(answer)}}}})
	}))
	defer model.Close()
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	current, err := server.newRun("fixture", "inspect screenshot", "synthetic fixture only")
	if err != nil {
		t.Fatal(err)
	}
	imagePath = filepath.Join(current.root, "tasks", "browser", "work", "result.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0700); err != nil {
		t.Fatal(err)
	}
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO4B9e8AAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, image, 0600); err != nil {
		t.Fatal(err)
	}
	unregisteredPath = filepath.Join(current.root, "unregistered.png")
	if err := os.WriteFile(unregisteredPath, image, 0600); err != nil {
		t.Fatal(err)
	}
	current.started, current.status = true, "running"
	current.state = assessment.State{Version: 1, ID: current.id, Status: "running", Goal: current.goal, Scope: current.scope}
	current.updateWorker(assessment.Event{TaskID: "browser", Kind: "execution_finished", Evidence: &assessment.EvidenceView{ArtifactRefs: []string{imagePath}}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	view := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+current.id+"/messages", messageRequest{Text: "Show me the screenshot"})
	last := view.Messages[len(view.Messages)-1]
	if last.Text != "Here is the recorded screenshot." || len(last.Images) != 1 || last.Images[0].Filename != "result.png" {
		t.Fatalf("coordinator image reply = %+v", last)
	}
	response, err := http.Get(httpServer.URL + last.Images[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("image response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	if err := atomicWriteJSON(filepath.Join(current.root, "assessment.json"), current.state); err != nil {
		t.Fatal(err)
	}
	restored := NewServer(Config{RepoRoot: root})
	if restored.loadErr != nil {
		t.Fatal(restored.loadErr)
	}
	restoredView := restored.getRun(current.id).view("")
	if len(restoredView.Messages[len(restoredView.Messages)-1].Images) != 1 {
		t.Fatal("presented image was lost when the session reopened")
	}
}

func TestCoordinatorChatSharesBudgetAcrossSnapshotAndResume(t *testing.T) {
	requests := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "Acknowledged."}}}, "usage": map[string]int{"total_tokens": 7}})
	}))
	defer model.Close()
	root := t.TempDir()
	config := Config{RepoRoot: root, LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}}
	server := NewServer(config)
	current, err := server.newRun("fixture", "inspect fixture", "synthetic only")
	if err != nil {
		t.Fatal(err)
	}
	limits := assessment.DefaultLimits()
	limits.ModelCalls = 2
	current.started, current.status = true, "running"
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "running", Limits: limits, Usage: assessment.Usage{Calls: 1}}
	if err := server.message(t.Context(), current, "First discussion turn", nil); err != nil {
		t.Fatal(err)
	}
	current.snapshot(assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "running", Limits: limits, Usage: assessment.Usage{Calls: 1}})
	if got := current.view("").Usage; got.Calls != 2 || got.ReportedTokens != 7 {
		t.Fatalf("coordinator snapshot lost chat usage: %+v", got)
	}
	if err := server.message(t.Context(), current, "Second discussion turn", nil); err == nil || !strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("unbudgeted chat request was allowed: %v", err)
	}
	if requests != 1 {
		t.Fatalf("provider received %d requests, want one", requests)
	}
	if err := atomicWriteJSON(filepath.Join(current.root, "assessment.json"), assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "running", Limits: limits, Usage: assessment.Usage{Calls: 1}}); err != nil {
		t.Fatal(err)
	}
	restored := NewServer(config)
	if restored.loadErr != nil {
		t.Fatal(restored.loadErr)
	}
	if got := restored.getRun(current.id).view("").Usage; got.Calls != 2 || got.ReportedTokens != 7 {
		t.Fatalf("resumed budget lost the chat call: %+v", got)
	}
}

func TestCoordinatorChatFollowupReceivesPriorTurn(t *testing.T) {
	var second []llmclient.Message
	requests := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if requests == 2 {
			second = request.Messages
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "Acknowledged."}}}})
	}))
	defer model.Close()
	server := NewServer(Config{RepoRoot: t.TempDir(), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	current, err := server.newRun("fixture", "inspect fixture", "synthetic only")
	if err != nil {
		t.Fatal(err)
	}
	current.started, current.status = true, "running"
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "running"}
	if err := server.message(t.Context(), current, "The marker is violet-otter", nil); err != nil {
		t.Fatal(err)
	}
	if err := server.message(t.Context(), current, "What was the marker?", nil); err != nil {
		t.Fatal(err)
	}
	if len(second) != 5 || second[2].Role != "user" || !strings.Contains(second[2].Content, "violet-otter") || second[3].Role != "assistant" || second[4].Role != "user" {
		t.Fatalf("follow-up lost ordered dialogue: %+v", second)
	}
}

func TestCoordinatorChatDoesNotAnswerPendingWorkerQuestions(t *testing.T) {
	requests := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "The workers are waiting for your answers."}}}})
	}))
	defer model.Close()
	server := NewServer(Config{RepoRoot: t.TempDir(), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	current, err := server.newRun("fixture", "inspect fixture", "synthetic only")
	if err != nil {
		t.Fatal(err)
	}
	current.started, current.status = true, "running"
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "running"}
	for _, id := range []string{"question-one", "question-two"} {
		current.questions[id] = &pendingQuestion{ID: id, taskID: id, text: "Which file?", answer: make(chan string, 1)}
	}
	if err := server.message(t.Context(), current, "Coordinator, explain the worker status", nil); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(current.questions) != 2 {
		t.Fatalf("main chat did not reach the coordinator: requests=%d pending=%d", requests, len(current.questions))
	}
	for _, question := range current.questions {
		select {
		case answer := <-question.answer:
			t.Fatalf("main chat became a worker answer: %q", answer)
		default:
		}
	}
	second := current.questions["question-two"]
	if err := current.answer("question-two", "the second fixture"); err != nil {
		t.Fatal(err)
	}
	if answer := <-second.answer; answer != "the second fixture" {
		t.Fatalf("selected worker received %q", answer)
	}
	if len(current.questions) != 1 || current.questions["question-one"] == nil {
		t.Fatal("explicit reply changed another worker's question")
	}
}

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
		var input strings.Builder
		for _, message := range request.Messages {
			input.WriteString(message.Content)
		}
		modelInput <- input.String()
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
		{Summary: strings.Repeat("Technical evidence and references remain in the full report. ", 12), PlainSummary: "The browser interaction was verified.", Complete: true},
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
	if view.Conclusion != "The browser interaction was verified." || analysis.Conclusion != view.Conclusion || view.ConclusionDetail != current.state.Plans[1].Summary || analysis.ConclusionDetail != view.ConclusionDetail {
		t.Fatalf("readable conclusion or complete detail missing after restore: chat=%q analysis=%q", view.Conclusion, analysis.Conclusion)
	}
	if len(analysis.Gaps) != 0 || len(analysis.NextActions) != 0 {
		t.Fatalf("resolved gap leaked into current analysis: %+v", analysis)
	}
	if len(restored.getRun(current.id).state.Plans[0].Gaps) != 1 {
		t.Fatal("historical gap was removed from the audit record")
	}
}
