package assessment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func TestStorageCancellationProducesIncompleteAssessment(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(fmt.Errorf("web session persistence failed: fixture storage error"))
	coordinator := testCoordinator("http://127.0.0.1:1/v1")
	state, err := coordinator.Run(ctx, filepath.Join(t.TempDir(), "assessment"), "inspect fixture", "synthetic only")
	if err == nil || state.Status != "incomplete" || !strings.Contains(state.Error, "fixture storage error") {
		t.Fatalf("storage cancellation was reported as successful/operator-aborted: status=%s err=%v state=%+v", state.Status, err, state)
	}
}

func TestCoordinatorResearchPhaseIsAnOperatorVisiblePlan(t *testing.T) {
	state := State{Version: 1, Goal: "investigate a scoped system", Scope: "fixture only", Limits: DefaultLimits()}
	d := Decision{Phase: "research", Summary: "Establish software identity and relevant sources", Tasks: []Task{{ID: "identify", Goal: "Identify the software and research applicable advisories", DoneWhen: "identity and sources are recorded", StrategyHints: []string{"software-research/SKILL.md"}}}}
	if err := validateDecision(d, state); err != nil {
		t.Fatalf("research plan rejected: %v", err)
	}
	if err := validateDecision(Decision{Phase: "research", Summary: "done", Complete: true}, state); err == nil {
		t.Fatal("research phase cannot finalize assessment")
	}
	if err := validateDecision(Decision{Phase: "unknown", Summary: "work", Tasks: d.Tasks}, state); err == nil {
		t.Fatal("unknown phase was accepted")
	}
	if !strings.Contains(coordinatorPrompt(state), "phase:research") {
		t.Fatal("coordinator prompt does not offer a research plan")
	}
	if !strings.Contains(coordinatorPrompt(state), "strategy_hints") {
		t.Fatal("coordinator prompt does not offer task-specific guide suggestions")
	}
	for _, hints := range [][]string{{"../outside/SKILL.md"}, {"/tmp/guide/SKILL.md"}, {"one/SKILL.md", "two/SKILL.md", "three/SKILL.md"}} {
		invalid := d
		invalid.Tasks = []Task{{ID: "identify", Goal: "research", DoneWhen: "record evidence", StrategyHints: hints}}
		if err := validateDecision(invalid, state); err == nil {
			t.Fatalf("accepted invalid strategy hints: %v", hints)
		}
	}
	state.Plans = []Decision{d}
	if got := compactCoordinatorState(state).Plans[0].Phase; got != "research" {
		t.Fatalf("research phase lost from coordinator history: %q", got)
	}
}

func TestCoordinatorCarriesDepthAndExplainsRoundTransition(t *testing.T) {
	approach := &Approach{ID: "focused", Label: "Focused", Description: "Check the exposed surface", Estimate: "about 10–20 minutes"}
	state := State{Version: 1, Goal: "inspect fixture", Scope: "synthetic only", Approach: approach}
	if !strings.Contains(coordinatorPrompt(state), `"approach":{"id":"focused"`) {
		t.Fatal("selected investigation depth was not sent to planning")
	}
	update := roundUpdate(2, Decision{Review: "The first check found one reachable service but no verified weakness.", PlainSummary: "Check that service's access controls next."})
	if !strings.Contains(update, "Round 1 complete: The first check found one reachable service") || !strings.Contains(update, "Proposed next: Check that service's access controls next.") {
		t.Fatalf("round transition was not readable: %q", update)
	}
	finished := roundUpdate(2, Decision{Review: "The authorized file was read.", PlainSummary: "The file contents were confirmed.", Complete: true})
	if finished != "Round 1 complete: The authorized file was read." {
		t.Fatalf("completion update duplicated the final conclusion: %q", finished)
	}
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

func TestCoordinatorRetainsEvidenceCatalogWhileBoundingResultCards(t *testing.T) {
	state := State{Version: 1, ID: "long-run", Goal: "review evidence", Scope: "fixture only"}
	result := Result{Task: Task{ID: "worker-a", Goal: "inspect", DoneWhen: "evidence recorded"}, Status: "done", Summary: "five checks completed"}
	for i := 0; i < 5; i++ {
		result.Evidence = append(result.Evidence, ctxpacket.ExecutionResult{ActualExec: strings.Repeat("long command ", 500), OutputSummary: "check completed", LogRefs: []string{fmt.Sprintf("/tmp/check-%d.log", i)}, ArtifactRefs: []string{fmt.Sprintf("/tmp/check-%d.txt", i)}})
	}
	state.Results = []Result{result}
	prompt := coordinatorPrompt(state)
	if len(prompt) > 15000 {
		t.Fatalf("coordinator prompt grew with repeated full commands: %d bytes", len(prompt))
	}
	var payload struct {
		Assessment struct {
			Results []struct {
				OmittedEvidence int `json:"omitted_evidence"`
			} `json:"results"`
		} `json:"assessment"`
		RecordedEvidence map[string][]string `json:"recorded_evidence"`
	}
	if err := json.Unmarshal([]byte(prompt), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Assessment.Results) != 1 || payload.Assessment.Results[0].OmittedEvidence != 2 || len(payload.RecordedEvidence["worker-a"]) != 10 {
		t.Fatal("compact card lost the complete registered evidence catalog")
	}
}

func TestCoordinatorModelCatalogIndexesLogsAndDistinctArtifacts(t *testing.T) {
	log := "/tmp/tasks/inspect/logs/action.log"
	image := "/tmp/tasks/inspect/work/capture.png"
	state := State{Version: 1, ID: "artifact-fixture", Goal: "review evidence", Scope: "fixture only", Results: []Result{{
		Task: Task{ID: "inspect", Goal: "inspect"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{
			LogRefs: []string{log}, ArtifactRefs: []string{log + ".stdout", log + ".stderr", log + ".approval.json", image},
		}},
	}}}
	var payload struct {
		Assessment struct {
			Results []struct {
				Evidence []struct {
					ArtifactRefs []string `json:"artifact_refs"`
				} `json:"evidence"`
			} `json:"results"`
		} `json:"assessment"`
		RecordedEvidence map[string][]string `json:"recorded_evidence"`
	}
	if err := json.Unmarshal([]byte(coordinatorPrompt(state)), &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload.RecordedEvidence["inspect"]; len(got) != 2 || got[0] != log || got[1] != image {
		t.Fatalf("model catalog did not retain log and distinct artifact: %v", got)
	}
	if got := payload.Assessment.Results[0].Evidence[0].ArtifactRefs; len(got) != 1 || got[0] != image {
		t.Fatalf("result card did not prioritize declared artifact: %v", got)
	}
	if got := state.Results[0].Evidence[0].ArtifactRefs; len(got) != 4 {
		t.Fatal("model projection changed durable evidence")
	}
}

func TestCoordinatorOlderPlansKeepTaskIndexAndRecentDetails(t *testing.T) {
	state := State{Version: 1, Goal: "long assessment", Scope: "fixture only"}
	for i := 0; i < 4; i++ {
		state.Plans = append(state.Plans, Decision{Summary: strings.Repeat("round notes ", 100), Tasks: []Task{{ID: fmt.Sprintf("task-%d", i), Goal: strings.Repeat("specific task goal ", 40), DoneWhen: "evidence recorded"}}})
	}
	view := compactCoordinatorState(state)
	if len(view.Plans) != 4 || view.Plans[0].Tasks[0].ID != "task-0" || view.Plans[0].Tasks[0].Goal != "" || view.Plans[0].Summary == state.Plans[0].Summary {
		t.Fatalf("older plan was not projected to a short task index: %+v", view.Plans[0])
	}
	if view.Plans[3].Tasks[0].Goal == "" || view.Plans[3].Tasks[0].DoneWhen == "" || state.Plans[0].Tasks[0].Goal == "" {
		t.Fatal("recent plan detail or durable historical plan was lost")
	}
}

func TestCoordinatorRetainsObservedLeadAfterLongResultPreamble(t *testing.T) {
	lead := "Observed available local resource: /opt/lab/candidates.txt"
	state := State{Results: []Result{{Task: Task{ID: "triage"}, Status: "done", Summary: strings.Repeat("recorded observation ", 115) + lead}}}
	view := compactCoordinatorState(state)
	if !strings.Contains(view.Results[0].Summary, lead) {
		t.Fatal("coordinator lost an observed lead before planning the next step")
	}
}

func TestCoordinatorBoundedViewKeepsRecentLeadAndFindingEvidence(t *testing.T) {
	state := State{Version: 1, ID: "long-run", Goal: "investigate the scoped fixture", Scope: "fixture only"}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("task-%d", i)
		log := fmt.Sprintf("/tmp/tasks/%s/action.log", id)
		state.Results = append(state.Results, Result{Task: Task{ID: id, Goal: "inspect a distinct bounded lead", DoneWhen: "record the observed result"}, Status: "done", Summary: strings.Repeat("recorded finding and limits ", 120), Evidence: []ctxpacket.ExecutionResult{{ActualExec: strings.Repeat("long command ", 200), LogRefs: []string{log}}}})
		state.Plans = append(state.Plans, Decision{Summary: strings.Repeat("planning history ", 80), Tasks: []Task{{ID: id, Goal: "inspect a distinct bounded lead"}}})
	}
	state.Results[0].Summary = strings.Repeat("older preamble ", 220) + "Older observed lead: inspect the authenticated route"
	state.Results[len(state.Results)-1].Summary = "Latest observed lead: inspect the local source tree"
	state.Plans[0].Findings = []Finding{{Title: "previous finding", Evidence: []string{"/tmp/tasks/task-0/action.log"}}}
	state.OperatorMessages = []string{strings.Repeat("older operator discussion ", 1000), "Latest operator direction: stay local"}
	prompt, err := coordinatorPromptBounded(state, 48000)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompt) > 48000 || !strings.Contains(prompt, "Latest observed lead") || !strings.Contains(prompt, "Older observed lead") || !strings.Contains(prompt, "Latest operator direction") || !strings.Contains(prompt, "/tmp/tasks/task-0/action.log") || strings.Contains(prompt, "/tmp/tasks/task-1/action.log") {
		t.Fatalf("bounded coordinator view lost priorities or retained old logs: %d bytes", len(prompt))
	}
	if len(state.Results[0].Evidence) != 1 || len(state.OperatorMessages[0]) < 1000 {
		t.Fatal("model projection mutated durable assessment state")
	}
}

func TestCoordinatorBoundedViewHandlesManyWorkerRounds(t *testing.T) {
	state := State{Version: 1, Goal: "review a long assessment", Scope: "fixture only"}
	for i := 0; i < 24; i++ {
		id := fmt.Sprintf("worker-%02d", i)
		state.Results = append(state.Results, Result{Task: Task{ID: id, Goal: "investigate a bounded lead", DoneWhen: "record outcome"}, Status: "done", Summary: strings.Repeat("distinct observation and uncertainty ", 90), Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{fmt.Sprintf("/tmp/%s.log", id)}}}})
		state.Plans = append(state.Plans, Decision{Summary: strings.Repeat("round outcome ", 50), Tasks: []Task{{ID: id}}})
	}
	view, err := coordinatorPromptBounded(state, llmclient.DefaultInputByteLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(view) > llmclient.DefaultInputByteLimit || !strings.Contains(view, "worker-00") || !strings.Contains(view, "worker-23") || !strings.Contains(view, "Distant worker conclusions") {
		t.Fatalf("long-run coordinator projection = %d bytes", len(view))
	}
}

func TestCoordinatorBoundedViewReservesSpaceForFinalCorrection(t *testing.T) {
	state := State{Version: 1, Goal: "review the scoped assessment", Scope: "one fixture", OperatorMessages: []string{"Latest operator direction: finish the report"}}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("task-%d", i)
		ref := fmt.Sprintf("/workspace/sessions/assessment/tasks/%s/logs/%s.log", id, strings.Repeat("evidence", 15))
		state.Results = append(state.Results, Result{Task: Task{ID: id, Goal: strings.Repeat("inspect target behavior ", 30), DoneWhen: "record a supported outcome"}, Status: "done", Summary: strings.Repeat("observed behavior and limitation ", 110) + id + " final lead", Evidence: []ctxpacket.ExecutionResult{{ActualExec: strings.Repeat("command ", 50), OutputSummary: strings.Repeat("observed output ", 30), LogRefs: []string{ref}}}})
		if i < 4 {
			state.Plans = append(state.Plans, Decision{Summary: strings.Repeat("prior plan and result ", 60), Tasks: []Task{{ID: id, Goal: strings.Repeat("inspect behavior ", 40)}}, Findings: []Finding{{Title: "prior candidate", Status: "candidate", Evidence: []string{ref}}}, Gaps: []string{strings.Repeat("previous limitation ", 30)}})
		}
	}
	state.Plans[len(state.Plans)-1].Findings = []Finding{{Title: "current candidate", Status: "candidate", Evidence: []string{state.Results[4].Evidence[0].LogRefs[0]}}}
	prompt, err := coordinatorPromptBounded(state, 32610)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompt) > 32610 || !strings.Contains(prompt, "task-0") || !strings.Contains(prompt, "task-4") || !strings.Contains(prompt, "current candidate") || !strings.Contains(prompt, "Latest operator direction") || !strings.Contains(prompt, "Distant worker conclusions") {
		t.Fatalf("correction view lost current state or exceeded allowance: %d bytes", len(prompt))
	}
}

func TestWorkerHandoffPrioritizesDeclaredDependencies(t *testing.T) {
	large := strings.Repeat("prior investigation details ", 300)
	results := []Result{
		{Task: Task{ID: "independent"}, Status: "done", Summary: large, Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"/tasks/independent/log"}}}},
		{Task: Task{ID: "required"}, Status: "done", Summary: "validated prerequisite", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"/tasks/required/log"}}}},
	}
	handoff := strings.Join(workerHandoff(results, []string{"required"}, "/tasks"), "\n")
	if !strings.Contains(handoff, "/tasks/required/log") || strings.Contains(handoff, "/tasks/independent/log") || strings.Contains(handoff, large) || !strings.Contains(handoff, "independent") || !strings.Contains(handoff, "/tasks/<task-id>/") {
		t.Fatalf("worker handoff lost prerequisite or overincluded unrelated evidence: %s", handoff)
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
			Role          string `json:"role"`
			ContextPacket string `json:"context_packet"`
			Assessment    struct {
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
				reply(w, Decision{Summary: "Investigate two independent questions", Tasks: []Task{{ID: "first", Goal: "Print observation one", DoneWhen: "literal output recorded", StrategyHints: []string{"investigation/SKILL.md"}}, {ID: "second", Goal: "Print observation two", DoneWhen: "literal output recorded"}}})
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
		if strings.Contains(payload.ContextPacket, "[latest_execution_result]\naction: printf") {
			reply(w, map[string]string{"type": "step_complete", "summary": "observed fixture output"})
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
		reply(w, map[string]any{"type": "bash", "command": "printf", "args": []string{"%s", "fixture observation"}})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	s, err := testCoordinator(server.URL).Run(ctx, root, "Collect and validate observations", "synthetic files only")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != "completed" || len(s.Results) != 3 || s.Usage.Calls != 12 || s.Usage.ReportedTokens != 120 {
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
		if result.Task.ID == "first" && (!strings.Contains(string(data), "Coordinator-suggested local guides: investigation/SKILL.md") || !strings.Contains(string(data), "not loaded instructions")) {
			t.Fatal("coordinator guide suggestion was not preserved in worker context")
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

func TestRecoverableWorkerFailureReachesNextPlanningRound(t *testing.T) {
	var coordinatorCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if len(req.Messages) > 1 && strings.Contains(req.Messages[1].Content, `"role":"assessment_coordinator"`) {
			coordinatorCalls++
			if coordinatorCalls == 1 {
				reply(w, Decision{Summary: "try one bounded worker", Tasks: []Task{{ID: "first-attempt", Goal: "record a fixture observation", DoneWhen: "the observation is recorded"}}})
			} else {
				var payload struct {
					Assessment struct {
						Results []Result `json:"results"`
					} `json:"assessment"`
				}
				if err := json.Unmarshal([]byte(req.Messages[1].Content), &payload); err != nil || len(payload.Assessment.Results) != 1 || payload.Assessment.Results[0].Status != "blocked" {
					t.Errorf("next planner did not receive the blocked result: %s", req.Messages[1].Content)
				}
				reply(w, Decision{Summary: "recorded the limitation", Complete: true, Gaps: []string{"worker budget exhausted"}})
			}
			return
		}
		// An invalid worker decision is a recorded, recoverable worker outcome.
		reply(w, map[string]string{"type": "not-a-worker-decision"})
	}))
	defer server.Close()
	coordinator := testCoordinator(server.URL)
	coordinator.Limits = Limits{Workers: 1, Rounds: 2, Tasks: 2, StepsPerTask: 1, ModelCalls: 8}
	state, err := coordinator.Run(context.Background(), t.TempDir(), "record a fixture observation", "synthetic fixture only")
	if err != nil || state.Status != "incomplete" || len(state.Results) != 1 || state.Results[0].Status != "blocked" || len(state.Plans) != 2 {
		t.Fatalf("worker failure stopped adaptation: state=%+v err=%v", state, err)
	}
}

func TestOperatorSkippedTaskIsExplicitlyExcludedFromWorkerContext(t *testing.T) {
	var sawBoundary atomic.Bool
	var coordinatorCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var payload struct {
			Role          string `json:"role"`
			ContextPacket string `json:"context_packet"`
		}
		if err := json.Unmarshal([]byte(req.Messages[1].Content), &payload); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(req.Messages[1].Content, `"role":"assessment_coordinator"`) {
			coordinatorCalls++
			if coordinatorCalls == 1 {
				reply(w, Decision{Summary: "Two independent fixture checks", Tasks: []Task{
					{ID: "selected", Goal: "Inspect fixture metadata", DoneWhen: "metadata recorded"},
					{ID: "skipped", Goal: "Inspect fixture services", DoneWhen: "services recorded"},
				}})
			} else {
				reply(w, Decision{Summary: "Fixture check ended", Complete: true})
			}
			return
		}
		if strings.Contains(payload.ContextPacket, "Inspect fixture services") && strings.Contains(payload.ContextPacket, "did not select sibling task") {
			sawBoundary.Store(true)
		}
		// The invalid fixture decision ends this worker without executing a tool.
		reply(w, map[string]string{"type": "invalid-fixture-decision"})
	}))
	defer server.Close()
	coordinator := testCoordinator(server.URL)
	coordinator.Limits = Limits{Workers: 2, Rounds: 2, Tasks: 2, StepsPerTask: 1, ModelCalls: 8}
	coordinator.PlanApproval = func(context.Context, Decision) (PlanReview, error) {
		return PlanReview{TaskIDs: []string{"selected"}}, nil
	}
	state, err := coordinator.Run(context.Background(), t.TempDir(), "Inspect fixture", "synthetic fixture only")
	if err != nil || len(state.Results) != 1 || !sawBoundary.Load() {
		t.Fatalf("skipped task was not excluded from selected worker: state=%+v err=%v boundary=%v", state, err, sawBoundary.Load())
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
		reply(w, map[string]any{"type": "bash", "command": "sh", "args": []string{"-c", "printf ready > ready; sleep 30"}})
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
	state := State{Status: "completed", Plans: []Decision{
		{Phase: "research", Tasks: []Task{{ID: "identify", Goal: "Identify the service", DoneWhen: "Version recorded"}}, ApprovedTaskIDs: []string{"identify"}},
		{Phase: "assessment", Tasks: []Task{{ID: "test", Goal: "Check access", DoneWhen: "Access result recorded"}}, SkippedTaskIDs: []string{"test"}},
		d,
	}}
	root := t.TempDir()
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "1. Request the administrative route") || !strings.Contains(string(report), "- Require authentication") || !strings.Contains(string(report), "Round 1 — research") || !strings.Contains(string(report), "Round 2 — assessment") || !strings.Contains(string(report), "skipped by operator") || !strings.Contains(string(report), "## Executive summary") {
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
