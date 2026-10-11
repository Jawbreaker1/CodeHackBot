//go:build live

package workerloop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
	"github.com/Jawbreaker1/CodeHackBot/internal/workergoal"
)

// Replay the failure where useful observations were discarded because the
// worker's final prose omitted the separate lead_hints handoff field.
func TestLiveGoalEvaluationKeepsSubstantiveResultAndOpenLead(t *testing.T) {
	const logRef = "/fictional/tasks/public-review/logs/read-only-check.log"
	frame := behavior.Frame{SystemPrompt: "You are BirdHackBot. Evaluate security work from recorded evidence without inventing findings.", Parameters: map[string]string{"scope": "fictional local fixture"}}
	packet := ctxpacket.NewInitialWorkerPacket(frame, session.Foundation{
		Goal: "Inspect the entry page and directly linked dashboard of a fictional application with read-only requests, explain what was tested, and return up to three unresolved lead_hints with evidence and bounded next checks.",
	}, t.TempDir(), "fictional-review", "per_action", 8)
	packet.LatestExecutionResult = ctxpacket.ExecutionResult{
		Action:         "read-only public page and client-code inspection",
		ExitStatus:     "0",
		LogRefs:        []string{logRef},
		OutputEvidence: "GET / and GET /dashboard returned 200. The dashboard's public client bundle reads a verification token from the page URL and exchanges it for an access token, then stores that access token in localStorage. The dashboard HTML also loads analytics code before the client bundle. No verification token was used, no analytics request carrying a token was observed, and an unauthenticated GET /api/v1/me/export returned 401.",
	}
	answer := "The public entry and dashboard were inspected. The unauthenticated export request was denied with 401. A possible verification-token data flow remains unresolved because no token-bearing browser navigation or analytics request was observed; this needs a separate bounded validation."

	home := os.Getenv("CODEX_HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(home, ".codex")
	}
	provider := subscription.Provider{Auth: &subscription.CodexAuth{Home: home}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	completion, err := provider.Complete(ctx, subscription.Request{
		Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high",
		Messages: []llmclient.Message{{Role: "system", Content: frame.PromptText()}, {Role: "user", Content: buildGoalEvaluationPrompt(packet, answer)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := completion["choices"].([]any)[0].(map[string]any)["message"].(llmclient.Message).Content
	evaluation, err := workergoal.Parse(raw)
	if err != nil {
		t.Fatalf("invalid evaluator response: %v: %s", err, raw)
	}
	if evaluation.Status != workergoal.StatusSatisfied {
		t.Fatalf("substantive result was rejected because of handoff formatting: %s", raw)
	}
	if len(boundedLeadHints([]string(evaluation.LeadHints))) == 0 || !strings.Contains(strings.Join([]string(evaluation.LeadHints), " "), logRef) {
		t.Fatalf("unresolved observed lead was not handed back with evidence: %s", raw)
	}
}
