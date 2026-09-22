package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestServerCreatesDraftAndServesUI(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	response, err := http.Get(httpServer.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(body), "What are we investigating?") || !strings.Contains(string(body), "Customers & sessions") || !strings.Contains(string(body), "workStatus") || !strings.Contains(string(body), "collapseWorkers") || !strings.Contains(string(body), "icon-trash") {
		t.Fatal("embedded operator console UI is missing")
	}
	css, err := http.Get(httpServer.URL + "/app.css")
	if err != nil || css.StatusCode != http.StatusOK {
		t.Fatal("operator console stylesheet is missing")
	}
	_ = css.Body.Close()
	smallMark, err := http.Get(httpServer.URL + "/logo-small.svg")
	if err != nil || smallMark.StatusCode != http.StatusOK {
		t.Fatal("compact logo is missing")
	}
	_ = smallMark.Body.Close()
	if smallMark.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatal("compact logo has an incorrect content type")
	}
	wordmark, err := http.Get(httpServer.URL + "/wordmark.svg")
	if err != nil || wordmark.StatusCode != http.StatusOK {
		t.Fatal("operator wordmark is missing")
	}
	wordmarkBody, _ := io.ReadAll(wordmark.Body)
	_ = wordmark.Body.Close()
	if !strings.Contains(string(wordmarkBody), "BirdHackBot") || !strings.Contains(string(wordmarkBody), "#e5484d") {
		t.Fatal("operator wordmark is incomplete")
	}
	analysisPage, err := http.Get(httpServer.URL + "/analysis")
	if err != nil || analysisPage.StatusCode != http.StatusOK {
		t.Fatal("analysis workspace is missing")
	}
	analysisBody, _ := io.ReadAll(analysisPage.Body)
	_ = analysisPage.Body.Close()
	if !strings.Contains(string(analysisBody), "Analysis workspace") {
		t.Fatal("analysis workspace shell is missing")
	}

	created := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "fixture-lab", Goal: "inspect the fixture", Scope: "only local synthetic commands; approve each action"})
	if created.Status != "draft" || created.ID == "" {
		t.Fatalf("draft = %#v", created)
	}
	if got := postStatus(t, httpServer.URL+"/api/v1/assessments/"+created.ID+"/start", nil); got != http.StatusConflict {
		t.Fatalf("start without model status = %d", got)
	}
}

func TestServerDeletesDraftSessions(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	server := NewServer(Config{RepoRoot: root, SessionsRoot: sessions, LLM: llmclient.Client{BaseURL: "http://127.0.0.1:1/v1", Model: "fixture"}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	intake := getJSON[intakeView](t, httpServer.URL+"/api/v1/intake")
	staleIntake := server.getIntake(intake.ID)
	intakeRoot := filepath.Join(sessions, "intake", intake.ID)
	status, body := requestJSON(t, http.MethodDelete, httpServer.URL+"/api/v1/intake/"+intake.ID, nil)
	if status != http.StatusNoContent || body != "" {
		t.Fatalf("delete intake status=%d body=%q", status, body)
	}
	if _, err := os.Stat(intakeRoot); !os.IsNotExist(err) {
		t.Fatalf("deleted intake directory still exists: %v", err)
	}
	if status, _ := requestJSON(t, http.MethodGet, httpServer.URL+"/api/v1/intake/"+intake.ID, nil); status != http.StatusNotFound {
		t.Fatalf("deleted intake GET status=%d", status)
	}
	if err := staleIntake.persist(); err == nil {
		t.Fatal("stale save recreated a deleted conversation")
	}

	assessmentDraft := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "delete-fixture", Goal: "remove this draft", Scope: "synthetic only"})
	assessmentRoot := filepath.Join(sessions, "delete-fixture", assessmentDraft.ID)
	staleRun := server.getRun(assessmentDraft.ID)
	status, body = requestJSON(t, http.MethodDelete, httpServer.URL+"/api/v1/assessments/"+assessmentDraft.ID, nil)
	if status != http.StatusNoContent || body != "" {
		t.Fatalf("delete assessment status=%d body=%q", status, body)
	}
	if _, err := os.Stat(assessmentRoot); !os.IsNotExist(err) {
		t.Fatalf("deleted assessment directory still exists: %v", err)
	}
	if status, _ := requestJSON(t, http.MethodGet, httpServer.URL+"/api/v1/assessments/"+assessmentDraft.ID, nil); status != http.StatusNotFound {
		t.Fatalf("deleted assessment GET status=%d", status)
	}
	if err := staleRun.persist(); err == nil {
		t.Fatal("stale save recreated a deleted assessment")
	}
	restarted := NewServer(server.config)
	if restarted.loadErr != nil || len(restarted.runs) != 0 || len(restarted.intakes) != 0 {
		t.Fatalf("deleted sessions reappeared after restart: %v", restarted.loadErr)
	}
}

func TestAssigningDraftToCustomerFolderPersistsAndIndexesIt(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	server := NewServer(Config{RepoRoot: root, SessionsRoot: sessions, LLM: llmclient.Client{BaseURL: "http://127.0.0.1:1/v1", Model: "fixture"}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	draft := getJSON[intakeView](t, httpServer.URL+"/api/v1/intake")
	assigned := postJSON[intakeView](t, httpServer.URL+"/api/v1/intake/"+draft.ID+"/customer", intakeCustomerRequest{Customer: "johans-lab"})
	if assigned.Customer != "johans-lab" {
		t.Fatalf("assigned draft = %#v", assigned)
	}
	var index struct {
		Customers []customerIndexView `json:"customers"`
		Intakes   []intakeIndexView   `json:"intakes"`
	}
	index = getJSON[struct {
		Customers []customerIndexView `json:"customers"`
		Intakes   []intakeIndexView   `json:"intakes"`
	}](t, httpServer.URL+"/api/v1/customers")
	if len(index.Intakes) != 0 || len(index.Customers) != 1 || len(index.Customers[0].Drafts) != 1 || index.Customers[0].Drafts[0].ID != draft.ID {
		t.Fatalf("customer index = %#v", index)
	}

	restarted := NewServer(server.config)
	restored := restarted.getIntake(draft.ID)
	if restarted.loadErr != nil || restored == nil || restored.customer != "johans-lab" {
		t.Fatalf("restored assigned draft = %v, %+v", restarted.loadErr, restored)
	}
}

func TestDeleteAssessmentRemovesLinkedConversationAndPreservesSibling(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root})
	first, err := server.newRun("customer", "first", "synthetic fixture")
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := server.newRun("customer", "second", "synthetic fixture")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := server.newIntake()
	if err != nil {
		t.Fatal(err)
	}
	conversation.assessmentID = first.id
	if err := conversation.persist(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.root, "report.md"), []byte("fixture evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	// Discover the pre-existing assessment_id relationship after restart.
	server = NewServer(server.config)
	if err := server.deleteRun(server.getRun(first.id)); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{first.root, conversation.root} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("session data remains at %s: %v", removed, err)
		}
	}
	server = NewServer(server.config)
	view := server.customerView("customer")
	if server.loadErr != nil || len(server.intakes) != 0 || len(view.Sessions) != 1 || view.Sessions[0].ID != sibling.id {
		t.Fatalf("unexpected remaining sessions: %+v; error: %v", view, server.loadErr)
	}
}

func TestDeleteRejectsBusyConversationAndUnsafeRoot(t *testing.T) {
	server := NewServer(Config{RepoRoot: t.TempDir()})
	conversation, err := server.newIntake()
	if err != nil {
		t.Fatal(err)
	}
	conversation.busy = true
	if err := server.deleteIntake(conversation); err == nil {
		t.Fatal("deleted busy conversation")
	}
	for _, root := range []string{server.config.SessionsRoot, filepath.Dir(conversation.root), t.TempDir()} {
		if err := server.removeSessionRoot(root); err == nil {
			t.Fatalf("accepted non-session path %s", root)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(server.config.SessionsRoot, "redirect")); err != nil {
		t.Fatal(err)
	}
	if err := server.removeSessionRoot(filepath.Join(server.config.SessionsRoot, "redirect", "session")); err == nil {
		t.Fatal("accepted redirected parent")
	}
}

func TestServerRejectsDeletingRunningSession(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	server := NewServer(Config{RepoRoot: root, SessionsRoot: sessions, LLM: llmclient.Client{BaseURL: "http://127.0.0.1:1/v1", Model: "fixture"}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	created := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "running-fixture", Goal: "keep while active", Scope: "synthetic only"})
	run := server.getRun(created.ID)
	run.mu.Lock()
	run.started, run.status = true, "running"
	run.mu.Unlock()
	status, body := requestJSON(t, http.MethodDelete, httpServer.URL+"/api/v1/assessments/"+created.ID, nil)
	if status != http.StatusConflict || !strings.Contains(body, "stop the assessment") {
		t.Fatalf("running delete status=%d body=%q", status, body)
	}
}

func TestAggregateContextWindowUsesLargestCurrentWorkerRequest(t *testing.T) {
	state := assessment.State{MaxInputBytes: 1000}
	view := aggregateContextWindow(state, []workerView{
		{ID: "worker-a", ContextUsedBytes: 100, ContextLimitBytes: 1000},
		{ID: "worker-b", ContextUsedBytes: 250, ContextLimitBytes: 1000},
	})
	if view.UsedBytes != 250 || view.LimitBytes != 1000 || view.RemainingBytes != 750 || view.Percent != 25 {
		t.Fatalf("context window = %+v", view)
	}
}

func TestServerUsesModelLedIntakeAndCoordinatorChat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Authorized synthetic web fixture only.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(webModelFixture))
	defer model.Close()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "web-fixture"}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	view := getJSON[intakeView](t, httpServer.URL+"/api/v1/intake")
	if view.Status != "conversation" || !view.ModelConfigured {
		t.Fatalf("intake view = %#v", view)
	}
	view = postJSON[intakeView](t, httpServer.URL+"/api/v1/intake/"+view.ID+"/messages", intakeMessageRequest{Text: "I want to record the web fixture, only using the authorized synthetic command."})
	if view.Status != "ready" || view.Proposal == nil || len(view.Messages) != 2 {
		t.Fatalf("intake response = %#v", view)
	}
	started := postJSON[assessmentView](t, httpServer.URL+"/api/v1/intake/"+view.ID+"/start", intakeStartRequest{Customer: "intake-fixture"})
	if started.Customer != "intake-fixture" {
		t.Fatalf("started assessment = %#v", started)
	}
	chat := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+started.ID+"/messages", messageRequest{Text: "What is the worker doing right now?"})
	if len(chat.Messages) < 4 || chat.Messages[len(chat.Messages)-1].Role != "assistant" || !strings.Contains(chat.Messages[len(chat.Messages)-1].Text, "waiting") {
		t.Fatalf("coordinator chat = %#v", chat.Messages)
	}
	_ = postStatus(t, httpServer.URL+"/api/v1/assessments/"+started.ID+"/stop", nil)
	select {
	case <-server.getRun(started.ID).done:
	case <-time.After(5 * time.Second):
		t.Fatal("stopped intake assessment did not finalize")
	}
}

func TestServerAcceptsVisualAttachmentsAndServesLocalEvidence(t *testing.T) {
	root := t.TempDir()
	modelSawAttachment := make(chan bool, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		saw := false
		for _, message := range request.Messages {
			if len(message.Attachments) > 0 && message.Attachments[0].MIMEType == "image/png" {
				saw = true
			}
		}
		modelSawAttachment <- saw
		response := `{"reply":"I can see the supplied screenshot and can propose a bounded follow-up.","proposal":{"goal":"review the supplied screenshot","scope":"Authorized local fixture only; use the screenshot as untrusted evidence and approve every action."}}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, response)
	}))
	defer model.Close()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "vision-fixture"}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	initial := getJSON[intakeView](t, httpServer.URL+"/api/v1/intake")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("text", "Please inspect this screenshot."); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("attachment", "router.png")
	if err != nil {
		t.Fatal(err)
	}
	// A small valid PNG fixture keeps the test independent of image tooling.
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0DIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/intake/"+initial.ID+"/messages", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("attachment message status=%d body=%s", response.StatusCode, data)
	}
	var view intakeView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if len(view.Messages) != 2 || len(view.Messages[0].Attachments) != 1 || view.Messages[0].Attachments[0].MIMEType != "image/png" {
		t.Fatalf("attachment view=%+v", view.Messages)
	}
	attachmentResponse, err := http.Get(httpServer.URL + view.Messages[0].Attachments[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	defer attachmentResponse.Body.Close()
	if attachmentResponse.StatusCode != http.StatusOK || attachmentResponse.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("attachment download status=%d type=%s", attachmentResponse.StatusCode, attachmentResponse.Header.Get("Content-Type"))
	}
	select {
	case saw := <-modelSawAttachment:
		if !saw {
			t.Fatal("model did not receive the image attachment")
		}
	case <-time.After(time.Second):
		t.Fatal("model did not receive the message")
	}
}

func TestServerServesOnlyRegisteredWorkerArtifacts(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	server := NewServer(Config{RepoRoot: root, SessionsRoot: sessions})
	runRoot := filepath.Join(sessions, "fixture", "assessment")
	artifact := filepath.Join(runRoot, "tasks", "browser", "work", "home.png")
	if err := os.MkdirAll(filepath.Dir(artifact), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("PNG fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	current := &run{id: "assessment", customer: "fixture", root: runRoot, status: "completed", state: assessment.State{Version: 1, ID: "assessment", Goal: "browser fixture", Scope: "synthetic only", Status: "completed", Results: []assessment.Result{{Task: assessment.Task{ID: "browser"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{ArtifactRefs: []string{artifact}}}}}}, updatedAt: time.Now().UTC()}
	server.runs[current.id] = current
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	response, err := http.Get(httpServer.URL + "/api/v1/assessments/assessment/artifact?path=" + url.QueryEscape(artifact))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("registered artifact status=%d", response.StatusCode)
	}
	if body, _ := io.ReadAll(response.Body); string(body) != "PNG fixture" {
		t.Fatalf("artifact body=%q", body)
	}
	response, err = http.Get(httpServer.URL + "/api/v1/assessments/assessment/artifact?path=" + url.QueryEscape(filepath.Join(runRoot, "tasks", "browser", "work", "missing.png")))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unregistered artifact status=%d", response.StatusCode)
	}
}

func TestServerApprovesModelSelectedLocalObservationBeforeProposal(t *testing.T) {
	root := t.TempDir()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		response := `{"reply":"I can inspect the workspace entries before proposing the check.","proposal":null,"tool":{"name":"list_directory","path":"."}}`
		for _, message := range request.Messages {
			if message.Role == "user" && strings.Contains(message.Content, "Tool observation") {
				response = `{"reply":"The workspace observation is complete. I can propose the bounded check now.","proposal":{"goal":"review the discovered workspace","scope":"Configured workspace entries only; no target contact or mutation."},"tool":null}`
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}],"usage":{"total_tokens":1}}`, response)
	}))
	defer model.Close()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "observation-fixture"}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	initial := getJSON[intakeView](t, httpServer.URL+"/api/v1/intake")
	resultCh := make(chan intakeView, 1)
	errCh := make(chan error, 1)
	go func() {
		status, body := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/intake/"+initial.ID+"/messages", intakeMessageRequest{Text: "List the files in the workspace so we can decide what to inspect."})
		if status < 200 || status >= 300 {
			errCh <- fmt.Errorf("message status=%d body=%s", status, body)
			return
		}
		var view intakeView
		if err := json.Unmarshal([]byte(body), &view); err != nil {
			errCh <- err
			return
		}
		resultCh <- view
	}()

	deadline := time.Now().Add(3 * time.Second)
	var pending *intakeApprovalView
	for time.Now().Before(deadline) {
		view := getJSON[intakeView](t, httpServer.URL+"/api/v1/intake/"+initial.ID)
		if view.PendingTool != nil {
			pending = view.PendingTool
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pending == nil || pending.Tool.Name != "list_directory" {
		t.Fatal("model-selected observation was not surfaced for approval")
	}
	postJSON[intakeView](t, httpServer.URL+"/api/v1/intake/"+initial.ID+"/approvals/"+pending.ID, approvalRequest{Decision: "approved_once"})
	select {
	case err := <-errCh:
		t.Fatal(err)
	case view := <-resultCh:
		if view.Status != "ready" || view.Proposal == nil || len(view.Messages) != 2 {
			t.Fatalf("final intake view=%#v", view)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("intake message did not complete after observation approval")
	}
}

func TestServerAggregatesSessionsByCustomer(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	first := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "shared-customer", Goal: "first bounded check", Scope: "synthetic fixture only"})
	second := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "shared-customer", Goal: "second bounded check", Scope: "synthetic fixture only"})
	_ = postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "other-customer", Goal: "separate check", Scope: "synthetic fixture only"})
	customer := getJSON[customerView](t, httpServer.URL+"/api/v1/customers/shared-customer")
	if customer.ID != "shared-customer" || len(customer.Sessions) != 2 || customer.Sessions[0].Customer != "shared-customer" {
		t.Fatalf("customer view = %#v", customer)
	}
	if customer.Sessions[0].ID != first.ID || customer.Sessions[1].ID != second.ID {
		t.Fatalf("customer sessions = %#v", customer.Sessions)
	}
	index := getJSON[map[string][]customerIndexView](t, httpServer.URL+"/api/v1/customers")
	if len(index["customers"]) != 2 || len(index["customers"][0].Sessions) == 0 || len(index["customers"][1].Sessions) == 0 {
		t.Fatalf("customer index = %#v", index)
	}
	status, report := requestJSON(t, http.MethodGet, httpServer.URL+"/api/v1/customers/shared-customer/report", nil)
	if status != http.StatusOK || !strings.Contains(report, first.ID) || !strings.Contains(report, second.ID) {
		t.Fatalf("customer report status=%d body=%s", status, report)
	}
}

func TestAnalysisPrioritizesFindingsAndAggregatesSessions(t *testing.T) {
	server := NewServer(Config{RepoRoot: t.TempDir()})
	first, err := server.newRun("analysis-customer", "first security check", "synthetic target")
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.newRun("analysis-customer", "second security check", "synthetic target")
	if err != nil {
		t.Fatal(err)
	}
	first.mu.Lock()
	first.status = "completed"
	first.state = assessment.State{ID: first.id, Goal: first.goal, Scope: first.scope, Status: "completed", Model: "daybreak", Plans: []assessment.Decision{{Summary: "validated", Findings: []assessment.Finding{{Title: "Critical auth bypass", Status: "reproduced", Severity: "critical", Confidence: "high", CVEIDs: []string{"CVE-2026-1234"}, AffectedSoftware: []string{"fixture 1.2"}, References: []string{"advisory.json"}, Impact: "account access", Steps: []string{"repeat"}, Evidence: []string{"validate.log"}, Remediation: []string{"patch"}}}}}}
	first.mu.Unlock()
	second.mu.Lock()
	second.status = "completed"
	second.state = assessment.State{ID: second.id, Goal: second.goal, Scope: second.scope, Status: "completed", Model: "qwen/qwen3.8-27b", Plans: []assessment.Decision{{Summary: "candidate", Gaps: []string{"authenticated coverage remains"}, Findings: []assessment.Finding{{Title: "Medium configuration issue", Status: "candidate", Severity: "medium", Confidence: "low", Impact: "configuration exposure", Steps: []string{"inspect"}, Evidence: []string{"inspect.log"}, Remediation: []string{"harden"}}}}}}
	second.mu.Unlock()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/assessments/"+first.id+"/analysis", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("assessment analysis status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var assessmentAnalysis analysisView
	if err := json.Unmarshal(recorder.Body.Bytes(), &assessmentAnalysis); err != nil {
		t.Fatal(err)
	}
	if len(assessmentAnalysis.Findings) != 1 || assessmentAnalysis.Findings[0].Priority != "critical" || assessmentAnalysis.Findings[0].CVEIDs[0] != "CVE-2026-1234" {
		t.Fatalf("assessment analysis=%+v", assessmentAnalysis)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/customers/analysis-customer/analysis", nil)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("customer analysis status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var customerAnalysisView analysisView
	if err := json.Unmarshal(recorder.Body.Bytes(), &customerAnalysisView); err != nil {
		t.Fatal(err)
	}
	if customerAnalysisView.SessionCount != 2 || len(customerAnalysisView.Sessions) != 2 || len(customerAnalysisView.Findings) != 2 || customerAnalysisView.Findings[0].Title != "Critical auth bypass" || len(customerAnalysisView.NextActions) < 2 {
		t.Fatalf("customer analysis=%+v", customerAnalysisView)
	}
}

func TestPlanReviewSelectsTasksBeforeExecution(t *testing.T) {
	server := NewServer(Config{RepoRoot: t.TempDir()})
	run, err := server.newRun("plan-customer", "plan fixture", "synthetic only")
	if err != nil {
		t.Fatal(err)
	}
	plan := assessment.Decision{Summary: "Choose bounded checks", Tasks: []assessment.Task{{ID: "inspect", Goal: "Inspect the fixture", DoneWhen: "inspection recorded"}, {ID: "validate", Goal: "Validate the fixture", DoneWhen: "validation recorded"}}}
	result := make(chan assessment.PlanReview, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		review, reviewErr := run.reviewPlanWait(ctx, plan)
		if reviewErr != nil {
			t.Errorf("plan review wait: %v", reviewErr)
			return
		}
		result <- review
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run.mu.RLock()
		pending := run.plan
		run.mu.RUnlock()
		if pending != nil {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/assessments/"+run.id+"/plans/"+pending.ID, strings.NewReader(`{"decision":"approved","approved_task_ids":["inspect"]}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("plan review status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			select {
			case review := <-result:
				if len(review.TaskIDs) != 1 || review.TaskIDs[0] != "inspect" {
					t.Fatalf("selected plan=%+v", review)
				}
				return
			case <-time.After(time.Second):
				t.Fatal("plan review callback did not resume")
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("plan review was not surfaced")
}

func TestServerRestoresIntakeTranscriptAndSessionModel(t *testing.T) {
	root := t.TempDir()
	model := httptest.NewServer(http.HandlerFunc(webModelFixture))
	defer model.Close()
	sessions := filepath.Join(root, "sessions")
	config := Config{RepoRoot: root, SessionsRoot: sessions, LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "first-model"}}
	first := NewServer(config)
	firstHTTP := httptest.NewServer(first)
	intake := getJSON[intakeView](t, firstHTTP.URL+"/api/v1/intake")
	selected := postJSON[intakeView](t, firstHTTP.URL+"/api/v1/intake/"+intake.ID+"/model", modelRequest{Model: "qwen/qwen3.8-27b"})
	if selected.Model != "qwen/qwen3.8-27b" || !selected.CanChangeModel {
		t.Fatalf("model selection = %#v", selected)
	}
	selected = postJSON[intakeView](t, firstHTTP.URL+"/api/v1/intake/"+intake.ID+"/messages", intakeMessageRequest{Text: "record the synthetic fixture"})
	if len(selected.Messages) != 2 {
		t.Fatalf("saved intake messages = %#v", selected.Messages)
	}
	firstHTTP.Close()

	second := NewServer(config)
	secondHTTP := httptest.NewServer(second)
	defer secondHTTP.Close()
	restored := getJSON[intakeView](t, secondHTTP.URL+"/api/v1/intake/"+intake.ID)
	if restored.Model != "qwen/qwen3.8-27b" || len(restored.Messages) != 2 || restored.Messages[0].Text != "record the synthetic fixture" {
		t.Fatalf("restored intake = %#v", restored)
	}
	index := getJSON[map[string][]intakeIndexView](t, secondHTTP.URL+"/api/v1/customers")
	if len(index["intakes"]) != 1 || index["intakes"][0].ID != intake.ID {
		t.Fatalf("restored intake index = %#v", index)
	}
}

func TestServerRejectsModelChangesDuringAssessment(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: "http://127.0.0.1:1/v1", Model: "initial"}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	created := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "model-fixture", Goal: "model selection", Scope: "synthetic only"})
	selected := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+created.ID+"/model", modelRequest{Model: "replacement"})
	if selected.Model != "replacement" || !selected.CanChangeModel {
		t.Fatalf("draft model selection = %#v", selected)
	}
	// Simulate the accepted/running state without making a network request to a
	// provider that this focused test does not need.
	run := server.getRun(created.ID)
	run.mu.Lock()
	run.started, run.status = true, "running"
	run.mu.Unlock()
	status, _ := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/assessments/"+created.ID+"/model", modelRequest{Model: "third"})
	if status == http.StatusOK {
		t.Fatal("model changed after assessment start")
	}
}

func TestServerRestoresDraftAssessmentWithoutAuthoritySnapshot(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	first := NewServer(Config{RepoRoot: root, SessionsRoot: sessions, LLM: llmclient.Client{BaseURL: "http://127.0.0.1:1/v1", Model: "draft-model"}})
	firstHTTP := httptest.NewServer(first)
	draft := postJSON[assessmentView](t, firstHTTP.URL+"/api/v1/assessments", createRequest{Customer: "draft-customer", Goal: "restore this draft", Scope: "synthetic only"})
	firstHTTP.Close()

	second := NewServer(Config{RepoRoot: root, SessionsRoot: sessions, LLM: llmclient.Client{BaseURL: "http://127.0.0.1:1/v1", Model: "draft-model"}})
	secondHTTP := httptest.NewServer(second)
	defer secondHTTP.Close()
	restored := getJSON[assessmentView](t, secondHTTP.URL+"/api/v1/assessments/"+draft.ID)
	if restored.Status != "draft" || restored.Goal != draft.Goal || restored.Scope != draft.Scope {
		t.Fatalf("restored draft = %#v", restored)
	}
}

func TestServerRunsSharedCoordinatorAndApprovalThroughHTTP(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Authorized synthetic web fixture only.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{"approval_mode": "per_action"})
	if err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(webModelFixture))
	defer model.Close()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), Frame: frame, LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "web-fixture"}})
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	created := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "fixture-lab", Goal: "record the web fixture", Scope: "only the synthetic local command; approve each action"})
	start := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+created.ID+"/start", nil)
	if start.Status != "starting" && start.Status != "running" {
		t.Fatalf("start status = %q", start.Status)
	}

	deadline := time.Now().Add(5 * time.Second)
	approved := false
	planApproved := false
	var latest assessmentView
	for time.Now().Before(deadline) {
		latest = getJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+created.ID)
		if !planApproved && latest.PendingPlan != nil {
			ids := make([]string, 0, len(latest.PendingPlan.Tasks))
			for _, task := range latest.PendingPlan.Tasks {
				ids = append(ids, task.ID)
			}
			postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+created.ID+"/plans/"+latest.PendingPlan.ID, planReviewRequest{Decision: "approved", ApprovedTaskIDs: ids})
			planApproved = true
		}
		if !approved && len(latest.PendingApprovals) > 0 {
			approvalID := latest.PendingApprovals[0].ID
			postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+created.ID+"/approvals/"+approvalID, approvalRequest{Decision: "approved_once"})
			approved = true
		}
		if latest.Status == "completed" || latest.Status == "incomplete" || latest.Status == "aborted" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if latest.Status != "completed" || !approved || !planApproved || len(latest.Results) != 1 || len(latest.Workers) != 1 {
		t.Fatalf("final view = %#v (approved=%v plan_approved=%v)", latest, approved, planApproved)
	}
	if latest.Workers[0].ID != "observe" || latest.Workers[0].EvidenceCount != 1 || latest.Workers[0].Phase != "done" {
		t.Fatalf("worker view = %#v", latest.Workers[0])
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", created.Customer, created.ID, "report.md")); err != nil {
		t.Fatalf("report missing: %v", err)
	}
}

func webModelFixture(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Messages []llmclient.Message `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	latest := ""
	if len(request.Messages) > 0 {
		latest = request.Messages[len(request.Messages)-1].Content
	}
	response := `{"type":"action","command":"printf","args":["%s","web fixture"],"summary":"recorded web fixture"}`
	if len(request.Messages) > 0 && strings.Contains(request.Messages[0].Content, "conversational assessment orchestrator") {
		response = `{"reply":"I can coordinate that authorized synthetic check. I have enough detail to propose one bounded observation.","proposal":{"goal":"record the web fixture","scope":"Authorized synthetic fixture only; run one printf command and approve each action."}}`
	} else if len(request.Messages) > 0 && strings.Contains(request.Messages[0].Content, "conversational interface") {
		response = `The worker is waiting for your approval before it runs the proposed action.`
	} else if strings.Contains(latest, "Evaluate whether the original worker goal") {
		response = `{"status":"satisfied","reason":"the fixture output is recorded","summary":"web fixture recorded"}`
	} else {
		var payload struct {
			Role       string           `json:"role"`
			Assessment assessment.State `json:"assessment"`
		}
		if err := json.Unmarshal([]byte(latest), &payload); err == nil && payload.Role == "assessment_coordinator" {
			if len(payload.Assessment.Results) == 0 {
				response = `{"summary":"web fixture plan","tasks":[{"id":"observe","goal":"record the web fixture","done_when":"fixture output is recorded","depends_on":[]}],"complete":false,"findings":[],"gaps":[]}`
			} else if len(payload.Assessment.Results) == 1 {
				response = `{"summary":"web fixture complete","tasks":[],"complete":true,"findings":[],"gaps":["synthetic fixture"]}`
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}],"usage":{"total_tokens":1}}`, response)
}

func postJSON[T any](t *testing.T, url string, value any) T {
	t.Helper()
	status, body := requestJSON(t, http.MethodPost, url, value)
	if status < 200 || status >= 300 {
		t.Fatalf("POST %s status = %d body = %s", url, status, body)
	}
	var decoded T
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func getJSON[T any](t *testing.T, url string) T {
	t.Helper()
	status, body := requestJSON(t, http.MethodGet, url, nil)
	if status < 200 || status >= 300 {
		t.Fatalf("GET %s status = %d body = %s", url, status, body)
	}
	var decoded T
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func postStatus(t *testing.T, url string, value any) int {
	status, _ := requestJSON(t, http.MethodPost, url, value)
	return status
}

func requestJSON(t *testing.T, method, url string, value any) (int, string) {
	t.Helper()
	var body *strings.Reader
	if value == nil {
		body = strings.NewReader("{}")
	} else {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = strings.NewReader(string(data))
	}
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(data)
}
