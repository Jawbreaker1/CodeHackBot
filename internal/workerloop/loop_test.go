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
const printAction = `{"type":"action","command":"printf","args":["%s","fixture"]}`

func TestActionIntentCannotBecomeWorkerOutcome(t *testing.T) {
	action := `{"type":"action","command":"printf","args":["%s","observed value"],"summary":"I will inspect the fixture"}`
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

func TestWorkerRecoversAndRevisesPlanWithoutChangingGoal(t *testing.T) {
	first := `{"type":"action","command":"cat","args":["missing.txt"],"plan":{"summary":"Read the supplied path","steps":["read supplied file"],"active_step":"read supplied file"}}`
	second := `{"type":"action","command":"cat","args":["actual.txt"],"plan":{"summary":"The first path was absent; use the provided alternative","steps":["read alternative file","report evidence"],"active_step":"read alternative file"}}`
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
		loop, p, _ := fixtureWorker(t, 1, `{"type":"action","command":"sh","args":["-c","printf 'expected absent'; exit 1"]}`, completeEval)
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
	for _, bad := range []string{`{"action":{"command":"touch","args":["should-not-exist"]}}`, `{"type":"action","command":"missing-fixture-command-xyz"}`} {
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
	loop, p, _ := fixtureWorker(t, 2, `{"type":"action","command":"touch","args":["should-not-exist"]}`)
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

func TestApprovalDenialAndCancellationStopWorker(t *testing.T) {
	loop, p, calls := fixtureWorker(t, 2, printAction)
	loop.Approver = approval.StaticApprover{Decision: approval.DecisionDeny}
	out, err := loop.Run(context.Background(), p, 2)
	if err == nil || out.Packet.LatestExecutionResult.Action != "" || *calls != 1 {
		t.Fatalf("denied execution err=%v calls=%d", err, *calls)
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
