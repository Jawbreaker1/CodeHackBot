package workerloop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

type riskReviewApprover struct{ mode approval.Mode }

func (a riskReviewApprover) ApprovalMode() approval.Mode { return a.mode }
func (a riskReviewApprover) Approve(context.Context, approval.Request) (approval.Decision, error) {
	return approval.DecisionApproveOnce, nil
}

func TestIndependentRiskReviewControlsAutomaticExecution(t *testing.T) {
	for _, test := range []struct {
		name, answer, wantRisk string
		wantApproval           bool
	}{
		{"read-only", `{"risk":"low","reason":"Reads bounded host metadata."}`, "low", false},
		{"fenced", "```json\n{\"risk\":\"low\",\"reason\":\"Reads bounded host metadata.\"}\n```", "low", false},
		{"risky", `{"risk":"dangerous","reason":"Changes the target."}`, "dangerous", true},
		{"uncertain", `{"risk":"unknown","reason":"The helper's effects are unknown."}`, "unknown", true},
		{"invalid", `not JSON`, "unknown", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Messages) != 2 || !strings.Contains(payload.Messages[1].Content, "uname -a") {
					t.Errorf("review did not receive the exact invocation: %+v, %v", payload, err)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": test.answer}}}})
			}))
			defer server.Close()
			loop := Loop{LLM: llmclient.Client{BaseURL: server.URL, Model: "fixture"}, Approver: riskReviewApprover{approval.DangerousOnly}}
			request := approval.Request{Command: "uname -a", Summary: "Inspect the host", Target: "local host", Impact: "Reads kernel metadata", Risk: "low"}
			reviewed := loop.reviewExecutionRisk(context.Background(), request)
			if calls != 1 || reviewed.Risk != test.wantRisk || approval.DangerousOnly.RequiresApproval(reviewed) != test.wantApproval || reviewed.WorkerRisk != "low" {
				t.Fatalf("review=%+v calls=%d, want risk=%s approval=%t", reviewed, calls, test.wantRisk, test.wantApproval)
			}
		})
	}
}

func TestIndependentRiskReviewSkipsAlreadyRiskyActions(t *testing.T) {
	loop := Loop{Approver: riskReviewApprover{approval.DangerousOnly}}
	request := approval.Request{Command: "risky-action", Risk: "dangerous"}
	if reviewed := loop.reviewExecutionRisk(context.Background(), request); reviewed != request || !approval.DangerousOnly.RequiresApproval(reviewed) {
		t.Fatalf("risky action changed without review: %+v", reviewed)
	}
}

type gatedRiskApprover struct {
	prompted bool
}

func (a *gatedRiskApprover) ApprovalMode() approval.Mode { return approval.DangerousOnly }
func (a *gatedRiskApprover) Approve(_ context.Context, request approval.Request) (approval.Decision, error) {
	a.prompted = approval.DangerousOnly.RequiresApproval(request)
	if a.prompted {
		return approval.DecisionDeny, nil
	}
	return approval.DecisionApproveSession, nil
}

func TestLowRiskWorkerCommandExecutesWithoutPromptAfterReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"risk":"low","reason":"Prints a bounded literal without changing files."}`}}}})
	}))
	defer server.Close()
	approver := &gatedRiskApprover{}
	dir := t.TempDir()
	loop := Loop{LLM: llmclient.Client{BaseURL: server.URL, Model: "fixture"}, Executor: execx.Executor{LogDir: t.TempDir()}, Approver: approver}
	packet := ctxpacket.NewInitialWorkerPacket(behavior.Frame{SystemPrompt: "test", AgentsText: "test"}, session.Foundation{Goal: "Inspect a harmless fixture"}, dir, "fixture", string(approval.DangerousOnly), 2)
	didRun, err := loop.execute(context.Background(), &packet, Response{Type: "bash", Command: "printf", Args: []string{"%s", "observed"}, Summary: "Print a local fixture", Target: "local fixture", Impact: "Prints a literal to the execution log", Risk: "low"})
	if err != nil || !didRun || approver.prompted || !strings.Contains(packet.LatestExecutionResult.OutputEvidence, "observed") {
		t.Fatalf("low-risk worker execution did not pass the review gate: ran=%t prompted=%t err=%v result=%+v", didRun, approver.prompted, err, packet.LatestExecutionResult)
	}
}
