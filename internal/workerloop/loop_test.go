package workerloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextinspect"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func testFoundation() session.Foundation {
	return session.Foundation{Goal: "Establish the fixture", ReportingRequirement: "Evidence and limitations"}
}

func fixtureWorker(t *testing.T, turns int, replies ...string) (Loop, ctxpacket.WorkerPacket, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls
		calls++
		if n >= len(replies) {
			t.Errorf("unexpected model request %d", n+1)
			http.Error(w, "unexpected call", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": replies[n]}, "finish_reason": "stop"}}})
	}))
	t.Cleanup(server.Close)
	frame := behavior.Frame{SystemPrompt: "Assess only the stated goal.", AgentsText: "Local synthetic fixtures only; every action requires approval.", Parameters: map[string]string{"scope": "local fixture only"}}
	packet := ctxpacket.NewInitialWorkerPacket(frame, testFoundation(), t.TempDir(), "fixture", "per_action", turns)
	loop := Loop{LLM: llmclient.Client{BaseURL: server.URL, Model: "fixture"}, Executor: execx.Executor{LogDir: t.TempDir()}, Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce}, Inspector: contextinspect.Recorder{Dir: t.TempDir()}}
	return loop, packet, &calls
}

const completeEval = `{"status":"satisfied","reason":"whole goal supported by the log","summary":"Fixture established"}`
const continueEval = `{"status":"in_progress","reason":"more observations needed","summary":""}`
const printAction = `{"type":"bash","command":"printf","args":["%s","fixture"]}`

func TestActionIntentCannotBecomeWorkerOutcome(t *testing.T) {
	action := `{"type":"bash","command":"printf","args":["%s","observed value"],"summary":"I will inspect the fixture"}`
	loop, packet, calls := fixtureWorker(t, 2, action, `{"type":"step_complete","summary":"The recorded output was observed value"}`, completeEval)
	out, err := loop.Run(context.Background(), packet, 2)
	if err != nil || *calls != 3 || out.Summary != "The recorded output was observed value" || len(out.Packet.RelevantRecentResults) != 0 {
		t.Fatalf("completion used action intent or skipped worker interpretation: summary=%q calls=%d err=%v", out.Summary, *calls, err)
	}
	loop, packet, calls = fixtureWorker(t, 1, action, completeEval)
	out, err = loop.Run(context.Background(), packet, 1)
	if err != nil || *calls != 2 || out.Summary != "Fixture established" {
		t.Fatalf("final-budget fallback used action intent: summary=%q calls=%d err=%v", out.Summary, *calls, err)
	}
}

func TestRepeatedUnsupportedCompletionWithoutNewEvidenceStops(t *testing.T) {
	claim := `{"type":"step_complete","summary":"The task is complete"}`
	loop, packet, calls := fixtureWorker(t, 5, printAction, claim, continueEval, claim, continueEval, claim)
	out, err := loop.Run(context.Background(), packet, 5)
	if err == nil || !strings.Contains(err.Error(), "without new execution evidence") || out.Packet.TaskRuntime.State != "blocked" || *calls != 6 {
		t.Fatalf("repeated unsupported completion was not stopped: state=%s calls=%d err=%v", out.Packet.TaskRuntime.State, *calls, err)
	}
}

func TestPlanOnlyLoopReturnsControlWithoutExecuting(t *testing.T) {
	plan := `{"type":"update_plan","plan":{"summary":"Review the fixture","steps":["inspect"],"active_step":"inspect"}}`
	loop, packet, calls := fixtureWorker(t, 8, plan, plan, plan, plan)
	out, err := loop.Run(context.Background(), packet, 8)
	if err == nil || !strings.Contains(err.Error(), "four worker decisions produced no execution") || *calls != 4 || out.Packet.TaskRuntime.State != "blocked" {
		t.Fatalf("plan loop was not handed back: calls=%d state=%s err=%v", *calls, out.Packet.TaskRuntime.State, err)
	}
	if out.Packet.WorkProgress.ExecutedActions != 0 || out.Packet.WorkProgress.DecisionsSinceExecution != 4 || out.Packet.WorkProgress.StartedAt.IsZero() {
		t.Fatalf("incorrect progress accounting: %+v", out.Packet.WorkProgress)
	}
}

func TestWorkerUsesSeveralDistinctContextReadsBeforeFinishing(t *testing.T) {
	loop, packet, calls := fixtureWorker(t, 7)
	ref := filepath.Join(loop.Executor.LogDir, "saved-text")
	saved := strings.Repeat("x", recallChunkBytes*2) + "FINAL_RECALLED_MARKER"
	if err := os.WriteFile(ref+".stdout", []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	packet.LatestExecutionResult = ctxpacket.ExecutionResult{Action: "read saved text", ExitStatus: "0", LogRefs: []string{ref}, OutputSummary: "saved text is available"}
	packet.WorkProgress.ExecutedActions = 1
	replies := []string{
		`{"type":"recall_context","context_query":"saved text"}`,
		fmt.Sprintf(`{"type":"recall_context","context_ref":%q,"context_offset":0}`, ref),
		fmt.Sprintf(`{"type":"recall_context","context_ref":%q,"context_offset":8192}`, ref),
		fmt.Sprintf(`{"type":"recall_context","context_ref":%q,"context_offset":16384}`, ref),
		`{"type":"step_complete","summary":"The saved text ended with FINAL_RECALLED_MARKER"}`,
		completeEval,
	}
	// Keep the existing fixture model and its request recorder, but replace its
	// scripted replies with a server for this longer deterministic sequence.
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverCalls >= len(replies) {
			t.Error("unexpected model request")
			http.Error(w, "unexpected request", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": replies[serverCalls]}, "finish_reason": "stop"}}})
		serverCalls++
	}))
	defer server.Close()
	loop.LLM.BaseURL = server.URL
	out, err := loop.Run(context.Background(), packet, 7)
	if err != nil || out.Packet.TaskRuntime.State != "done" || serverCalls != 6 || *calls != 0 {
		t.Fatalf("distinct saved reads were blocked: state=%s calls=%d err=%v", out.Packet.TaskRuntime.State, serverCalls, err)
	}
	recorder := loop.Inspector.(contextinspect.Recorder)
	request, err := os.ReadFile(filepath.Join(recorder.Dir, "step-005-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), "FINAL_RECALLED_MARKER") || !strings.Contains(string(request), "next_offset: 16405") {
		t.Fatal("the exact model request did not contain the final recovered page and its next byte offset")
	}
	if !strings.Contains(string(request), "remaining_budget: 2 steps") {
		t.Fatal("model request showed a stale turn budget")
	}
}

func TestRepeatedIdenticalContextReadStillStops(t *testing.T) {
	read := `{"type":"recall_context","context_query":"saved text"}`
	loop, packet, calls := fixtureWorker(t, 10, read, read, read, read, read)
	packet.LatestExecutionResult = ctxpacket.ExecutionResult{Action: "read saved text", ExitStatus: "0", OutputSummary: "saved text is available", LogRefs: []string{filepath.Join(loop.Executor.LogDir, "saved-text")}}
	out, err := loop.Run(context.Background(), packet, 10)
	if err == nil || !strings.Contains(err.Error(), "four worker decisions produced no execution") || out.Packet.TaskRuntime.State != "blocked" || *calls != 5 {
		t.Fatalf("repeated read bypassed the no-progress gate: state=%s calls=%d err=%v", out.Packet.TaskRuntime.State, *calls, err)
	}
}

func TestModelRequestFitsAfterRecallWithLongConversation(t *testing.T) {
	goal := "Find the exact synthetic report label from the early operator message about report labels. Quote the label and its saved conversation reference. Use read-only context recall; do not run commands."
	packet := ctxpacket.NewInitialWorkerPacket(
		behavior.Frame{SystemPrompt: "You are a worker reviewing saved synthetic notes. Use only recorded context. Do not run commands.", AgentsText: "Read only the saved synthetic fixture context.", Parameters: map[string]string{"scope": "saved synthetic notes only"}},
		session.Foundation{Goal: goal, ReportingRequirement: "cite the saved message"}, t.TempDir(), "fixture", "denied", 6,
	)
	packet.CurrentStep.DoneCondition = "Give the exact label and the saved conversation reference"
	packet.RecentConversation = append(packet.RecentConversation, "Operator answer: report label instruction: use LABEL-COPPER-17 for the final synthetic record.")
	for i := 0; i < 200; i++ {
		packet.RecentConversation = append(packet.RecentConversation, "User: ordinary note about section order "+strings.Repeat("neutral fixture detail ", 9))
		packet.RecentConversation = append(packet.RecentConversation, "Assistant: recorded the section order without changing the report label")
	}
	packet.LatestExecutionResult = ctxpacket.ExecutionResult{Action: "read synthetic observation", ExitStatus: "0", OutputSummary: "Synthetic prior observation recorded", LogRefs: []string{"/fixture/observation"}}
	packet.ContextRecall = ctxpacket.ContextRecall{Query: "early operator message exact synthetic report label report labels", Stream: "index", Matched: true, Content: strings.Repeat(`ref="conversation:1" preview="Operator answer: report label instruction: use LABEL-COPPER-17 for the final synthetic record."`+"\n", 6)}
	packet.Budget.Used = 1
	packet.RunningSummary = "A bounded excerpt from saved task context is available for the next decision; its source reference is in context_recall."
	loop := Loop{LLM: llmclient.Client{MaxInputBytes: 48 * 1024}}
	view, err := loop.modelView(&packet, buildUserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if modelInputBytes(view, buildUserPrompt) > loop.LLM.InputByteLimit() || !strings.Contains(view.Render(), "LABEL-COPPER-17") {
		t.Fatal("pressure dropped a requested recall or exceeded the actual model request budget")
	}
}

func TestProgressReviewReachesWorkerAndExecutionResetsIt(t *testing.T) {
	plan := `{"type":"update_plan","plan":{"summary":"Review the fixture","steps":["inspect"],"active_step":"inspect"}}`
	loop, packet, _ := fixtureWorker(t, 4, plan, plan, printAction, `{"type":"step_complete","summary":"Fixture observed"}`, completeEval)
	sink := &recordingProgressSink{}
	loop.Progress = sink
	out, err := loop.Run(context.Background(), packet, 4)
	if err != nil || out.Packet.TaskRuntime.State != "done" || out.Packet.WorkProgress.ExecutedActions != 1 || out.Packet.WorkProgress.DecisionsSinceExecution != 1 {
		t.Fatalf("execution did not reset no-action count: progress=%+v err=%v", out.Packet.WorkProgress, err)
	}
	seen := false
	for i, event := range sink.events {
		if event.Kind == EventDecisionStarted && event.StepIndex == 3 && strings.Contains(sink.packets[i].WorkProgress.Review, "no new execution evidence") {
			seen = true
		}
	}
	if !seen {
		t.Fatal("progress review was not visible before the third decision")
	}
}

func TestProgressReviewChecksAfterTwoActions(t *testing.T) {
	packet := ctxpacket.WorkerPacket{}
	packet.WorkProgress.ExecutedActions = 2
	if review := progressReview(packet); !strings.Contains(review, "original done condition") {
		t.Fatalf("second action did not trigger an outcome checkpoint: %q", review)
	}
	packet.WorkProgress.ExecutedActions = 3
	if review := progressReview(packet); review != "(none)" {
		t.Fatalf("checkpoint repeated without a new trigger: %q", review)
	}
}

func TestWorkerRecoversAndRevisesPlanWithoutChangingGoal(t *testing.T) {
	first := `{"type":"bash","command":"cat","args":["missing.txt"],"plan":{"summary":"Read the supplied path","steps":["read supplied file"],"active_step":"read supplied file"}}`
	second := `{"type":"bash","command":"cat","args":["actual.txt"],"plan":{"summary":"The first path was absent; use the provided alternative","steps":["read alternative file","report evidence"],"active_step":"read alternative file"}}`
	loop, p, calls := fixtureWorker(t, 3, first, second, `{"type":"step_complete","summary":"Read and reported the fixture from actual.txt"}`, completeEval)
	p.SessionFoundation.Goal = "Read the fixture from missing.txt or actual.txt and report its contents"
	p.CurrentStep.DoneCondition = "The fixture content is observed and reported"
	if err := os.WriteFile(filepath.Join(p.OperatorState.WorkingDir, "actual.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	sink := &recordingProgressSink{}
	loop.Progress = sink
	out, err := loop.Run(context.Background(), p, 3)
	if err != nil || out.Packet.TaskRuntime.State != "done" || *calls != 4 {
		t.Fatalf("out=%+v err=%v calls=%d", out, err, *calls)
	}
	if out.Packet.Budget.Used != 3 || out.Packet.CurrentStep.RemainingBudget != "0 steps" {
		t.Fatalf("budget=%+v", out.Packet.Budget)
	}
	if out.Packet.PlanState.ActiveStep != "read alternative file" || out.Packet.PlanState.WorkerGoal != p.SessionFoundation.Goal || out.Packet.CurrentStep.DoneCondition != p.CurrentStep.DoneCondition {
		t.Fatal("plan changed task contract or failed to revise")
	}
	if len(out.Packet.RelevantRecentResults) != 1 || out.Packet.RelevantRecentResults[0].ExitStatus == "0" || out.Packet.LatestExecutionResult.ExitStatus != "0" {
		t.Fatal("recovery history lost")
	}
	if p.PlanState.Mode != "" || p.Budget.Used != 0 {
		t.Fatal("caller state mutated")
	}
	if len(out.Packet.PlanHistory) != 2 || out.Packet.PlanHistory[0].AfterExecutionLog != "" || out.Packet.PlanHistory[1].AfterExecutionLog != out.Packet.RelevantRecentResults[0].LogRefs[0] {
		t.Fatal("plan revision chronology was not preserved")
	}
	if sink.packets[0].Budget.Used != 0 || sink.packets[0].TaskRuntime.State != "running" {
		t.Fatal("earlier progress snapshot mutated")
	}
}

func TestRepeatedInvocationsRemainDistinctChronologicalEvidence(t *testing.T) {
	var replies []string
	for i := 0; i < 6; i++ {
		replies = append(replies, printAction)
	}
	replies = append(replies, `{"type":"step_complete","summary":"Six distinct fixture observations were recorded"}`, completeEval)
	loop, p, _ := fixtureWorker(t, 7, replies...)
	out, err := loop.Run(context.Background(), p, 7)
	if err != nil {
		t.Fatal(err)
	}
	results := append([]ctxpacket.ExecutionResult{out.Packet.LatestExecutionResult}, out.Packet.RelevantRecentResults...)
	if len(results) != 6 {
		t.Fatalf("lost repeated observations: %d", len(results))
	}
	seen := map[string]bool{}
	for i, r := range results {
		if len(r.LogRefs) != 1 || seen[r.LogRefs[0]] {
			t.Fatalf("duplicate execution identity: %+v", r)
		}
		seen[r.LogRefs[0]] = true
		if i > 0 && r.StartedAt.After(results[i-1].StartedAt) {
			t.Fatal("history not newest first")
		}
	}
}

func TestCompletionRequiresEvidenceAndWholeGoalEvaluation(t *testing.T) {
	t.Run("claim alone", func(t *testing.T) {
		loop, p, calls := fixtureWorker(t, 1, `{"type":"step_complete","summary":"done"}`)
		out, err := loop.Run(context.Background(), p, 1)
		if err == nil || out.Packet.TaskRuntime.State != "blocked" || *calls != 1 {
			t.Fatalf("err=%v calls=%d state=%s", err, *calls, out.Packet.TaskRuntime.State)
		}
	})
	t.Run("omitted requirement", func(t *testing.T) {
		loop, p, calls := fixtureWorker(t, 2, printAction, `{"type":"step_complete","summary":"first part done","plan":{"summary":"Only part one","steps":["part one"],"active_step":"part one"}}`, continueEval)
		p.SessionFoundation.Goal = "Establish both first and second conditions"
		out, err := loop.Run(context.Background(), p, 2)
		if err == nil || out.Packet.TaskRuntime.State == "done" || *calls != 3 {
			t.Fatalf("false completion err=%v calls=%d", err, *calls)
		}
	})
	t.Run("nonzero negative evidence", func(t *testing.T) {
		loop, p, _ := fixtureWorker(t, 1, `{"type":"bash","command":"sh","args":["-c","printf 'expected absent'; exit 1"]}`, completeEval)
		out, err := loop.Run(context.Background(), p, 1)
		if err != nil || out.Packet.LatestExecutionResult.ExitStatus != "1" {
			t.Fatalf("valid negative evidence rejected: %v", err)
		}
	})
	t.Run("evaluator unavailable", func(t *testing.T) {
		loop, p, _ := fixtureWorker(t, 2, printAction, `{"type":"step_complete","summary":"the fixture is established"}`, "not a verdict")
		out, err := loop.Run(context.Background(), p, 3)
		if err == nil || out.Packet.TaskRuntime.State != "failed" || !strings.Contains(err.Error(), "evaluation unavailable") {
			t.Fatalf("err=%v state=%s", err, out.Packet.TaskRuntime.State)
		}
	})
}

func TestMalformedDecisionAndUnavailableToolCanBeCorrected(t *testing.T) {
	for _, bad := range []string{`{"action":{"command":"touch","args":["should-not-exist"]}}`, `{"type":"bash","command":"missing-fixture-command-xyz"}`} {
		t.Run(bad, func(t *testing.T) {
			loop, p, calls := fixtureWorker(t, 2, bad, printAction, completeEval)
			out, err := loop.Run(context.Background(), p, 2)
			if err != nil || *calls != 3 || len(out.Packet.RelevantRecentResults) != 0 {
				t.Fatalf("err=%v calls=%d evidence=%+v", err, *calls, out.Packet.RelevantRecentResults)
			}
		})
	}
}

type failProgress struct{ kind ProgressEventKind }

func (f failProgress) EmitProgress(e ProgressEvent, p ctxpacket.WorkerPacket) error {
	if e.Kind == f.kind {
		return errors.New("fixture disk failure")
	}
	return nil
}

func TestPersistenceFailureStopsBeforeExternalEffect(t *testing.T) {
	loop, p, _ := fixtureWorker(t, 2, `{"type":"bash","command":"touch","args":["should-not-exist"]}`)
	loop.Progress = failProgress{EventExecutionStarted}
	out, err := loop.Run(context.Background(), p, 2)
	if err == nil || out.Packet.TaskRuntime.State != "failed" {
		t.Fatalf("err=%v state=%s", err, out.Packet.TaskRuntime.State)
	}
	if _, err := os.Stat(filepath.Join(p.OperatorState.WorkingDir, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("action ran despite failed persistence")
	}
	if out.Packet.OperatorState.PendingExec == "" {
		t.Fatal("pending execution was lost")
	}
}

func TestApprovalDenialStopsWorkerWithPartialEvidence(t *testing.T) {
	loop, p, calls := fixtureWorker(t, 2, printAction)
	loop.Approver = approval.StaticApprover{Decision: approval.DecisionDeny}
	sink := &recordingProgressSink{}
	loop.Progress = sink
	out, err := loop.Run(context.Background(), p, 2)
	if err == nil || out.Packet.TaskRuntime.State != "blocked" || out.Packet.LatestExecutionResult.Action != "" || *calls != 1 || !strings.Contains(out.Summary, "did not execute") {
		t.Fatalf("denied execution state=%s summary=%q err=%v calls=%d", out.Packet.TaskRuntime.State, out.Summary, err, *calls)
	}
	denied := out.Packet.DeniedExecution
	if denied == nil || denied.Command == "" || denied.AuditRef == "" {
		t.Fatalf("denied action was not preserved in the worker packet: %+v", denied)
	}
	if _, err := os.Stat(denied.AuditRef); err != nil {
		t.Fatalf("denied action has no approval record: %v", err)
	}
	denialSeen := false
	for _, event := range sink.events {
		denialSeen = denialSeen || event.Kind == EventExecutionDenied
	}
	if !denialSeen {
		t.Fatal("denied action was not visible in progress")
	}
	loop, p, calls = fixtureWorker(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err = loop.Run(ctx, p, 2)
	if !errors.Is(err, context.Canceled) || out.Packet.TaskRuntime.State != "aborted" || *calls != 0 {
		t.Fatalf("canceled err=%v state=%s", err, out.Packet.TaskRuntime.State)
	}
}

func TestQuestionAnswerAndResumeUseOriginalBudget(t *testing.T) {
	loop, p, calls := fixtureWorker(t, 2, `{"type":"ask_user","question":"Which text?"}`, printAction, completeEval)
	const answer = "first line\n  indented line\nlast line"
	loop.AskUser = func(context.Context, string) (string, error) { return answer, nil }
	out, err := loop.Run(context.Background(), p, 2)
	if err != nil || *calls != 3 || !strings.Contains(strings.Join(out.Packet.RecentConversation, "\n"), answer) {
		t.Fatalf("answer lost: err=%v", err)
	}
	// An interrupted task resumes its remaining turn instead of granting maxSteps again.
	loop, p, calls = fixtureWorker(t, 2, printAction, continueEval)
	p.Budget.Used = 1
	out, err = loop.Run(context.Background(), p, 100)
	if err == nil || out.Packet.Budget.Limit != 2 || out.Packet.Budget.Used != 2 || *calls != 2 {
		t.Fatalf("resume reset budget: %+v err=%v calls=%d", out.Packet.Budget, err, *calls)
	}
	_, err = loop.Run(context.Background(), out.Packet, 100)
	if err == nil || *calls != 2 {
		t.Fatal("exhausted task contacted model again")
	}
}

func TestPendingExecutionIsNeverReplayed(t *testing.T) {
	loop, p, calls := fixtureWorker(t, 3)
	p.OperatorState.PendingExec = "touch unknown"
	out, err := loop.Run(context.Background(), p, 3)
	if err == nil || !strings.Contains(err.Error(), "unknown") || *calls != 0 || out.Packet.OperatorState.PendingExec == "" {
		t.Fatalf("err=%v calls=%d", err, *calls)
	}
}

func TestInvalidAndOversizedContextStopsBeforeModel(t *testing.T) {
	for _, kind := range []string{"policy", "budget", "size"} {
		t.Run(kind, func(t *testing.T) {
			loop, p, calls := fixtureWorker(t, 2)
			switch kind {
			case "policy":
				p.BehaviorFrame.AgentsText = ""
			case "budget":
				p.Budget.Used = -1
			case "size":
				p.SessionFoundation.Goal = strings.Repeat("goal ", 20000)
			}
			out, err := loop.Run(context.Background(), p, 2)
			if err == nil || *calls != 0 || out.Packet.TaskRuntime.State != "failed" {
				t.Fatalf("err=%v calls=%d", err, *calls)
			}
		})
	}
}

func TestProgressSnapshotsAndEvaluatorPromptPreserveTaskContract(t *testing.T) {
	loop, p, _ := fixtureWorker(t, 1, printAction, completeEval)
	sink := &recordingProgressSink{}
	loop.Progress = sink
	out, err := loop.Run(context.Background(), p, 1)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range sink.events {
		kinds = append(kinds, string(e.Kind))
		if e.Kind == EventExecutionStarted && e.Action != out.Packet.LatestExecutionResult.ActualExec {
			t.Fatal("execution progress displayed a stale invocation")
		}
	}
	for _, want := range []ProgressEventKind{EventTaskStarted, EventDecisionStarted, EventActionProposed, EventExecutionStarted, EventExecutionFinished, EventPostExecEvalStarted, EventPostExecEvalFinished, EventTaskCompleted} {
		if !strings.Contains(strings.Join(kinds, ","), string(want)) {
			t.Fatalf("missing %s in %v", want, kinds)
		}
	}
	if out.Packet.OperatorState.ContextUsedBytes <= 0 || out.Packet.OperatorState.ContextLimitBytes != loop.LLM.InputByteLimit() {
		t.Fatalf("context accounting missing from terminal packet: used=%d limit=%d", out.Packet.OperatorState.ContextUsedBytes, out.Packet.OperatorState.ContextLimitBytes)
	}
	for _, event := range sink.events {
		if event.Kind == EventDecisionStarted || event.Kind == EventPostExecEvalStarted {
			if event.ContextUsedBytes <= 0 || event.ContextLimitBytes != loop.LLM.InputByteLimit() {
				t.Fatalf("context accounting missing from %s: %+v", event.Kind, event)
			}
		}
	}
	prompt := buildGoalEvaluationPrompt(out.Packet, "claimed answer")
	for _, want := range []string{p.SessionFoundation.Goal, p.CurrentStep.DoneCondition, "claimed answer", "scope"} {
		if !strings.Contains(prompt, want) {
			t.Fatal(fmt.Sprintf("evaluation lost %s", want))
		}
	}
}
