package workerloop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

type recordingApprover struct{ request approval.Request }

func (a *recordingApprover) Approve(_ context.Context, request approval.Request) (approval.Decision, error) {
	a.request = request
	return approval.DecisionApproveOnce, nil
}

func TestLoopApprovesActualInvocationAndWorkingDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"content": `{"type":"action","command":"printf","args":["%s","one; printf two"],"use_shell":false}`,
		}}}})
	}))
	defer server.Close()
	approver := &recordingApprover{}
	dir := t.TempDir()
	loop := Loop{LLM: llmclient.Client{BaseURL: server.URL, Model: "test"}, Executor: execx.Executor{LogDir: t.TempDir()}, Approver: approver}
	packet := ctxpacket.NewInitialWorkerPacket(behavior.Frame{SystemPrompt: "test", AgentsText: "test"}, session.Foundation{Goal: "print literal text"}, dir, "test", "pending", 1)
	outcome, _ := loop.Run(context.Background(), packet, 1)
	result := outcome.Packet.LatestExecutionResult
	if result.OutputEvidence != "stdout: one; printf two" {
		t.Fatalf("unexpected execution: %+v", result)
	}
	if approver.request.UseShell || approver.request.Cwd != dir {
		t.Fatalf("approval=%+v", approver.request)
	}
	log, err := os.ReadFile(result.LogRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "actual_invocation: "+approver.request.Command+"\n") || !strings.Contains(string(log), "cwd: "+dir+"\n") {
		t.Fatalf("executed invocation differs from approval: %s", log)
	}
}

func TestActionContractPreservesQuotedLiteral(t *testing.T) {
	response, err := ParseResponse(`{"type":"action","command":"printf","args":["%s","hello world"],"use_shell":false}`)
	if err != nil {
		t.Fatal(err)
	}
	action, failure := prepareAction(response, t.TempDir())
	if failure != nil {
		t.Fatal(failure)
	}
	result, err := (execx.Executor{LogDir: t.TempDir()}).Run(context.Background(), action)
	if err != nil {
		t.Fatal(err)
	}
	if result.StdoutSummary != "hello world" {
		t.Fatalf("output=%q", result.StdoutSummary)
	}
}

func TestActionContractRejectsUnparsedCommandString(t *testing.T) {
	_, failure := prepareAction(Response{Command: `printf "%s" "hello world"`}, t.TempDir())
	if failure == nil {
		t.Fatal("invalid direct command must be rejected, not reinterpreted")
	}
}

func TestRelativeExecutableUsesTaskWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "local-tool"), []byte("#!/bin/sh\nprintf '%s' \"$PWD\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	action, failure := prepareAction(Response{Command: "./local-tool"}, dir)
	if failure != nil {
		t.Fatal(failure)
	}
	result, err := (execx.Executor{LogDir: t.TempDir()}).Run(context.Background(), action)
	if err != nil {
		t.Fatal(err)
	}
	if result.StdoutSummary != dir {
		t.Fatalf("cwd=%q, want %q", result.StdoutSummary, dir)
	}
}

func TestAnalysisCompletionPassesProposedAnswerToEvidenceEvaluator(t *testing.T) {
	const answer = "The observed version is 1.4; the advisory for version 1.1 does not apply."
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(req.Messages[1].Content), &payload)
		var response string
		switch calls {
		case 1:
			response = `{"type":"action","command":"printf","args":["%s","observed version 1.4; advisory affects 1.1"]}`
		case 2:
			response = `{"status":"in_progress","reason":"Need interpretation of captured facts","summary":""}`
		case 3:
			data, _ := json.Marshal(Response{Type: "step_complete", Summary: answer})
			response = string(data)
		case 4:
			if payload["proposed_answer"] != answer {
				t.Errorf("evaluator did not receive answer: %v", payload["proposed_answer"])
			}
			if !strings.Contains(payload["context_packet"].(string), "observed version 1.4; advisory affects 1.1") {
				t.Error("answer replaced execution evidence")
			}
			response = `{"status":"satisfied","reason":"Proposed analysis is supported by the observed versions","summary":"Analysis complete"}`
		default:
			t.Error("unnecessary extra worker turn")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": response}}}})
	}))
	defer server.Close()
	packet := ctxpacket.NewInitialWorkerPacket(behavior.Frame{SystemPrompt: "test", AgentsText: "test"}, session.Foundation{Goal: "Interpret the observed version and advisory"}, t.TempDir(), "test", "pending", 3)
	packet.OperatorState.ModeHint = "direct_execution"
	loop := Loop{LLM: llmclient.Client{BaseURL: server.URL, Model: "test"}, Executor: execx.Executor{LogDir: t.TempDir()}, Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce}}
	outcome, err := loop.Run(context.Background(), packet, 3)
	if err != nil || outcome.Summary != answer || outcome.Packet.TaskRuntime.State != "done" || calls != 4 {
		t.Fatalf("completion=%+v err=%v calls=%d", outcome, err, calls)
	}
}
