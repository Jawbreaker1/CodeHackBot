package webapp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
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
	if err != nil || !strings.Contains(string(body), "What are we investigating?") || !strings.Contains(string(body), "Customers & sessions") {
		t.Fatal("embedded operator console UI is missing")
	}
	css, err := http.Get(httpServer.URL + "/app.css")
	if err != nil || css.StatusCode != http.StatusOK {
		t.Fatal("operator console stylesheet is missing")
	}
	_ = css.Body.Close()

	created := postJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments", createRequest{Customer: "fixture-lab", Goal: "inspect the fixture", Scope: "only local synthetic commands; approve each action"})
	if created.Status != "draft" || created.ID == "" {
		t.Fatalf("draft = %#v", created)
	}
	if got := postStatus(t, httpServer.URL+"/api/v1/assessments/"+created.ID+"/start", nil); got != http.StatusConflict {
		t.Fatalf("start without model status = %d", got)
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
	var latest assessmentView
	for time.Now().Before(deadline) {
		latest = getJSON[assessmentView](t, httpServer.URL+"/api/v1/assessments/"+created.ID)
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
	if latest.Status != "completed" || !approved || len(latest.Results) != 1 || len(latest.Workers) != 1 {
		t.Fatalf("final view = %#v (approved=%v)", latest, approved)
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
