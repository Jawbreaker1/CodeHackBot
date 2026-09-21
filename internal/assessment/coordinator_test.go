package assessment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func reply(w http.ResponseWriter, value any) {
	b, _ := json.Marshal(value)
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(b)}}}, "usage": map[string]int{"total_tokens": 10}})
}

func testCoordinator(url string) Coordinator {
	return Coordinator{LLM: llmclient.Client{BaseURL: url, Model: "fixture-model"}, Frame: behavior.Frame{SystemPrompt: "Authorized lab test", AgentsText: "Use synthetic fixtures only"}, Approver: func(Task) approval.Approver { return approval.StaticApprover{Decision: approval.DecisionApproveOnce} }}
}

func TestCoordinatorPromptCompactsPriorExecutionBodies(t *testing.T) {
	large := strings.Repeat("full shell script and output ", 10000)
	state := State{
		Version: 1, ID: "compact-fixture", Goal: "review evidence", Scope: "fixture only",
		Results: []Result{
			{Task: Task{ID: "worker-a", Goal: "inspect", DoneWhen: "evidence recorded"}, Status: "done", Summary: "bounded result", Evidence: []ctxpacket.ExecutionResult{
				{ActualExec: large, OutputEvidence: large, OutputSummary: "short summary", LogRefs: []string{"/tmp/evidence.log"}},
			}},
		},
	}
	prompt := coordinatorPrompt(state)
	if len(prompt) > 20000 || strings.Contains(prompt, large) || !strings.Contains(prompt, "/tmp/evidence.log") {
		t.Fatalf("coordinator prompt was not compacted: bytes=%d", len(prompt))
	}
}

func TestFindingAdvisoryFieldsRemainStructuredAndValidated(t *testing.T) {
	state := State{Limits: DefaultLimits(), Results: []Result{{Task: Task{ID: "research"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"research.log"}}}}}}
	finding := Finding{Title: "Known issue", Status: "candidate", Severity: "high", Confidence: "medium", CVEIDs: []string{"CVE-2026-1234"}, AffectedSoftware: []string{"fixture 1.2"}, References: []string{"https://example.invalid/advisory"}, Impact: "fixture", Steps: []string{"repeat the check"}, Evidence: []string{"research.log"}, Remediation: []string{"upgrade"}}
	if err := validateDecision(Decision{Summary: "research recorded", Complete: true, Findings: []Finding{finding}}, state); err != nil {
		t.Fatalf("structured advisory finding rejected: %v", err)
	}
	finding.Severity = "urgent"
	if err := validateDecision(Decision{Summary: "research recorded", Complete: true, Findings: []Finding{finding}}, state); err == nil {
		t.Fatal("invalid finding severity was accepted")
	}
}

func TestCoordinatorDelegatesThenValidatesWithSharedBudgetAndEvidence(t *testing.T) {
	var mu sync.Mutex
	active, peak, arrivals := 0, 0, 0
	both := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var payload struct {
			Role       string `json:"role"`
			Assessment struct {
				Results []struct {
					Evidence []struct {
						LogRefs []string `json:"log_refs"`
					} `json:"evidence"`
				} `json:"results"`
			} `json:"assessment"`
		}
		_ = json.Unmarshal([]byte(req.Messages[1].Content), &payload)
		if payload.Role == "assessment_coordinator" {
			s := payload.Assessment
			switch len(s.Results) {
			case 0:
				reply(w, Decision{Summary: "Investigate two independent questions", Tasks: []Task{{ID: "first", Goal: "Print observation one", DoneWhen: "literal output recorded"}, {ID: "second", Goal: "Print observation two", DoneWhen: "literal output recorded"}}})
			case 2:
				reply(w, Decision{Summary: "Validate the first observation", Tasks: []Task{{ID: "validate", Goal: "Print validation evidence", DoneWhen: "literal output recorded", DependsOn: []string{"first"}}}})
			case 3:
				reply(w, Decision{Summary: "Evidence recorded", Complete: true, Findings: []Finding{{Title: "Synthetic finding", Status: "reproduced", ValidationTask: "validate", Impact: "fixture only", Steps: []string{"repeat the recorded invocation"}, Evidence: []string{s.Results[2].Evidence[0].LogRefs[0]}, Remediation: []string{"fixture only"}}}})
			default:
				t.Error("unexpected coordinator state")
			}
			return
		}
		if !strings.Contains(req.Messages[0].Content, "scope: synthetic files only") {
			t.Error("worker lost inherited scope")
		}
		if strings.Contains(req.Messages[1].Content, "Evaluate whether the original worker goal") {
			reply(w, map[string]string{"status": "satisfied", "reason": "literal execution evidence exists", "summary": "observed fixture output"})
			return
		}
		mu.Lock()
		active++
		arrivals++
		if active > peak {
			peak = active
		}
		if arrivals == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-both:
		case <-r.Context().Done():
			return
		}
		mu.Lock()
		active--
		mu.Unlock()
		reply(w, map[string]any{"type": "action", "command": "printf", "args": []string{"%s", "fixture observation"}})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	s, err := testCoordinator(server.URL).Run(ctx, root, "Collect and validate observations", "synthetic files only")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != "completed" || len(s.Results) != 3 || s.Usage.Calls != 9 || s.Usage.ReportedTokens != 90 {
		t.Fatalf("unexpected result: %+v", s)
	}
	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	if gotPeak != 2 {
		t.Fatalf("concurrency=%d, want 2", gotPeak)
	}
	workspaces := map[string]bool{}
	for _, result := range s.Results {
		if result.Status != "done" || len(result.Evidence) != 1 {
			t.Fatalf("bad task result: %+v", result)
		}
		e := result.Evidence[0]
		if workspaces[e.Cwd] {
			t.Fatal("workers share a mutable workspace")
		}
		workspaces[e.Cwd] = true
		for _, ref := range append(e.LogRefs, e.ArtifactRefs...) {
			if _, err := os.Stat(ref); err != nil {
				t.Fatal(err)
			}
		}
		data, err := os.ReadFile(filepath.Join(root, "tasks", result.Task.ID, "session.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "synthetic files only") {
			t.Fatal("persisted scope missing")
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Synthetic finding") || !strings.Contains(string(data), "operator review required") {
		t.Fatal("report lost evidence qualification")
	}
}

func TestBudgetFailureStillProducesPartialReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply(w, Decision{Summary: "one task", Tasks: []Task{{ID: "one", Goal: "print a value", DoneWhen: "evidence recorded"}}})
	}))
	defer server.Close()
	c := testCoordinator(server.URL)
	c.Limits = DefaultLimits()
	c.Limits.ModelCalls = 1
	root := t.TempDir()
	s, err := c.Run(context.Background(), root, "Observe fixture", "fixture only")
	if err == nil || s.Status != "incomplete" || s.Usage.Calls != 1 {
		t.Fatalf("budget failure: %+v, %v", s, err)
	}
	if _, err := os.Stat(filepath.Join(root, "report.md")); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorCancellationStopsAllWorkersAndWritesAbortedReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []llmclient.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.Messages[1].Content, `"role":"assessment_coordinator"`) {
			reply(w, Decision{Summary: "two long-running checks", Tasks: []Task{{ID: "one", Goal: "Wait for cancellation", DoneWhen: "wait finished"}, {ID: "two", Goal: "Wait for cancellation", DoneWhen: "wait finished"}}})
			return
		}
		reply(w, map[string]any{"type": "action", "command": "sh", "args": []string{"-c", "printf ready > ready; sleep 30"}})
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	done := make(chan State, 1)
	go func() {
		s, _ := testCoordinator(server.URL).Run(ctx, root, "Cancel two tasks", "local fixture only")
		done <- s
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, a := os.Stat(filepath.Join(root, "tasks/one/work/ready"))
		_, b := os.Stat(filepath.Join(root, "tasks/two/work/ready"))
		if a == nil && b == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("workers did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case s := <-done:
		if s.Status != "aborted" || len(s.Results) != 2 {
			t.Fatalf("state=%+v", s)
		}
		for _, r := range s.Results {
			if r.Status != "aborted" {
				t.Fatalf("worker did not stop: %+v", r)
			}
		}
		data, err := os.ReadFile(filepath.Join(root, "report.md"))
		if err != nil || !strings.Contains(string(data), "**aborted**") {
			t.Fatalf("aborted report: %s, %v", data, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("broadcast stop did not finish")
	}
}

func TestCoordinatorRejectsUnfinishedDependenciesAndInventedEvidence(t *testing.T) {
	s := State{Limits: DefaultLimits(), Results: []Result{{Task: Task{ID: "old"}, Status: "failed", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"recorded.log"}}}}}}
	for _, d := range []Decision{
		{Summary: "bad dependency", Tasks: []Task{{ID: "next", Goal: "go", DoneWhen: "done", DependsOn: []string{"old"}}}},
		{Summary: "bad path", Tasks: []Task{{ID: "../escape", Goal: "go", DoneWhen: "done"}}},
		{Summary: "invented evidence", Complete: true, Findings: []Finding{{Title: "claim", Status: "candidate", Impact: "impact", Steps: []string{"steps"}, Evidence: []string{"invented.log"}, Remediation: []string{"fix"}}}},
		{Summary: "unvalidated claim", Complete: true, Findings: []Finding{{Title: "claim", Status: "reproduced", ValidationTask: "old", Impact: "impact", Steps: []string{"steps"}, Evidence: []string{"recorded.log"}, Remediation: []string{"fix"}}}},
	} {
		if err := validateDecision(d, s); err == nil {
			t.Fatalf("accepted invalid decision: %+v", d)
		}
	}
}

func TestLoadStateReadsOnlyCompleteAssessmentSnapshots(t *testing.T) {
	root := t.TempDir()
	state := State{Version: 1, ID: "assessment-fixture", Goal: "observe a fixture", Scope: "fixture only", Status: "incomplete"}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assessment.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != state.ID || got.Scope != state.Scope {
		t.Fatalf("loaded state = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "assessment.json"), []byte(`{"version":1,"id":"assessment-fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(root); err == nil {
		t.Fatal("accepted incomplete assessment snapshot")
	}
}

func TestFindingListsFromLiveResponseRemainStructured(t *testing.T) {
	d, err := parseDecision(`{"summary":"Evidence gathered","tasks":[],"complete":true,"findings":[{"title":"Missing authentication","status":"candidate","impact":"Synthetic data readable","steps":["Request the administrative route without credentials","Inspect response"],"evidence":["recorded.log"],"remediation":["Require authentication","Retest the route"]}],"gaps":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Findings) != 1 || len(d.Findings[0].Steps) != 2 || len(d.Findings[0].Remediation) != 2 {
		t.Fatalf("lost reproduction/remediation lists: %+v", d)
	}
	state := State{Status: "completed", Plans: []Decision{d}}
	root := t.TempDir()
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "1. Request the administrative route") || !strings.Contains(string(report), "- Require authentication") {
		t.Fatalf("report lost structured steps: %s", report)
	}
}

func TestInvalidCoordinatorProposalGetsOnlyOneBoundedCorrection(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Messages        []llmclient.Message `json:"messages"`
			ReasoningEffort string              `json:"reasoning_effort"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ReasoningEffort != "low" {
			t.Error("correction must retain configured reasoning effort")
		}
		if calls == 2 && !strings.Contains(req.Messages[len(req.Messages)-1].Content, "Validation error:") {
			t.Error("repair omitted validation feedback")
		}
		reply(w, Decision{Summary: "invalid path proposal", Tasks: []Task{{ID: "../escape", Goal: "unaccepted", DoneWhen: "never"}}})
	}))
	defer server.Close()
	root := t.TempDir()
	c := testCoordinator(server.URL)
	c.LLM.ReasoningEffort = "low"
	s, err := c.Run(context.Background(), root, "Observe a fixture", "fixture only")
	if err == nil || calls != 2 || s.Usage.Calls != 2 || len(s.Results) != 0 {
		t.Fatalf("unbounded or executed rejected proposal: calls=%d state=%+v err=%v", calls, s, err)
	}
	if _, err := os.Stat(filepath.Join(root, "coordinator-01-correction-rejection.json")); err != nil {
		t.Fatal(err)
	}
}
