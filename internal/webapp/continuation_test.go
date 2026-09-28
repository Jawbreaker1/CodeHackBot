package webapp

import (
	"encoding/json"
	"fmt"
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

func TestCompletedSessionContinuesWithAReviewedWorkerPlan(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		latest := request.Messages[len(request.Messages)-1].Content
		response := `{"type":"bash","command":"printf","args":["%s","continued fixture"],"summary":"record the continued fixture"}`
		switch {
		case strings.Contains(request.Messages[0].Content, "conversational interface"):
			response = `{"text":"I will prepare another bounded plan in this session.","continue_assessment":true}`
		case strings.Contains(latest, "Evaluate whether the original worker goal"):
			response = `{"status":"satisfied","reason":"the new observation was recorded","summary":"continued fixture recorded"}`
		default:
			var payload struct {
				Role       string `json:"role"`
				Assessment struct {
					ContinuationRequest string `json:"continuation_request"`
					Results             []any  `json:"results"`
				} `json:"assessment"`
				ContextPacket string `json:"context_packet"`
			}
			if json.Unmarshal([]byte(latest), &payload) == nil {
				if payload.Role == "assessment_coordinator" {
					if payload.Assessment.ContinuationRequest != "Inspect the same fixture again" {
						t.Errorf("continuation request absent from planning packet: %s", latest)
					}
					if len(payload.Assessment.Results) == 1 {
						response = `{"summary":"Check the fixture again","plain_summary":"Record a fresh observation.","tasks":[{"id":"follow-up","goal":"Record a fresh observation from the same fixture","done_when":"new output is recorded","depends_on":[]}],"complete":false,"findings":[],"gaps":[]}`
					} else {
						response = `{"summary":"The follow-up observation was recorded.","plain_summary":"The new check finished.","review":"The worker recorded a fresh observation.","tasks":[],"complete":true,"findings":[],"gaps":[]}`
					}
				} else if payload.Role == "worker" && strings.Contains(payload.ContextPacket, "[latest_execution_result]\naction: printf") {
					response = `{"type":"step_complete","summary":"continued fixture recorded"}`
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}],"usage":{"total_tokens":1}}`, response)
	}))
	defer model.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Authorized synthetic fixture only.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{"approval_mode": "per_action"})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, Frame: frame, Limits: assessment.DefaultLimits()}
	server := NewServer(config)
	current, err := server.newRun("fixture", "Inspect fixture", "synthetic fixture only")
	if err != nil {
		t.Fatal(err)
	}
	current.resume, current.status = true, "completed"
	current.state = assessment.State{
		Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "completed", Limits: assessment.DefaultLimits(),
		Plans:   []assessment.Decision{{Summary: "Earlier assessment complete", Complete: true}},
		Results: []assessment.Result{{Task: assessment.Task{ID: "initial", Goal: "Earlier fixture check"}, Status: "done", Summary: "Earlier evidence preserved."}},
	}
	if err := atomicWriteJSON(filepath.Join(current.root, "assessment.json"), current.state); err != nil {
		t.Fatal(err)
	}
	if err := current.persist(); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id
	view := postJSON[assessmentView](t, path+"/messages", messageRequest{Text: "Inspect the same fixture again"})
	if view.ID != current.id || view.Status != "starting" && view.Status != "running" {
		t.Fatalf("continuation did not reopen the same session: %+v", view)
	}
	approvedPlan, approvedAction := false, false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view = getJSON[assessmentView](t, path)
		if view.PendingPlan != nil && !approvedPlan {
			if len(view.PendingPlan.Tasks) != 1 || view.PendingPlan.Tasks[0].ID != "follow-up" {
				t.Fatalf("unexpected follow-up plan: %+v", view.PendingPlan)
			}
			postJSON[assessmentView](t, path+"/plans/"+view.PendingPlan.ID, planReviewRequest{Decision: "approved", ApprovedTaskIDs: []string{"follow-up"}})
			approvedPlan = true
		}
		if len(view.PendingApprovals) > 0 && !approvedAction {
			postJSON[assessmentView](t, path+"/approvals/"+view.PendingApprovals[0].ID, approvalRequest{Decision: "approved_once"})
			approvedAction = true
		}
		if view.Status == "completed" || view.Status == "completed_with_gaps" || view.Status == "incomplete" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if view.Status != "completed" || !approvedPlan || !approvedAction || len(view.Results) != 2 || view.Results[0].Task.ID != "initial" || view.Results[1].Task.ID != "follow-up" {
		t.Fatalf("continued run lost history or worker result: status=%s plan=%v action=%v results=%+v", view.Status, approvedPlan, approvedAction, view.Results)
	}
	state, err := assessment.LoadState(current.root)
	if err != nil || len(state.Plans) != 3 || len(state.ContinuationRequests) != 1 || state.ContinuationRequests[0] != "Inspect the same fixture again" {
		t.Fatalf("continuation was not durable: state=%+v err=%v", state, err)
	}
	report, err := os.ReadFile(filepath.Join(current.root, "report.md"))
	if err != nil || !strings.Contains(string(report), "Additional operator requests in this session") {
		t.Fatalf("continued work absent from report: err=%v", err)
	}
	restored := NewServer(config)
	if restored.loadErr != nil || len(restored.getRun(current.id).view("").Results) != 2 {
		t.Fatalf("continued session did not restore: %v", restored.loadErr)
	}
}
