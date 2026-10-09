//go:build live

package assessment

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

// This opt-in smoke check tests cross-worker synthesis with fictional evidence.
// One model run is diagnostic, not acceptance evidence for pentest effectiveness.
func TestLiveCoordinatorConnectsIndependentLeads(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{"approval_mode": "operator_selected_session_policy", "research_mode": "connected"})
	if err != nil {
		t.Fatal(err)
	}
	const traceRef = "/fixture/tasks/trace/observation.log"
	const portalRef = "/fixture/tasks/portal/observation.log"
	state := State{
		Version: 1, ID: "synthetic-cross-worker-review", Goal: "Assess whether the fictional service exposes another tenant's trace data", Scope: "fictional fixture only", Model: "gpt-daybreak-blue-latest", Status: "running",
		Limits: Limits{Workers: 2, Rounds: 4, Tasks: 6, StepsPerTask: 8, ModelCalls: 40},
		Results: []Result{
			{Task: Task{ID: "trace", Goal: "Check trace content"}, Status: "done", Summary: "A trace records user-supplied diagnostic tags together with a tenant-specific account identifier. It did not prove who can read the trace. A benign synthetic marker was recorded.", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{traceRef}, OutputSummary: "Tenant identifier and synthetic marker appear in the trace."}}},
			{Task: Task{ID: "portal", Goal: "Check trace visibility"}, Status: "done", Summary: "An unauthenticated support portal lists trace previews from more than one tenant. The worker did not compare a listed preview with the other worker's synthetic marker or fetch a full trace.", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{portalRef}, OutputSummary: "Anonymous listing contained previews from two tenant identifiers."}}},
		},
		Plans: []Decision{{Summary: "Inspect trace content and the support portal independently", Tasks: []Task{{ID: "trace", Goal: "Check trace content", DoneWhen: "Record a safe marker"}, {ID: "portal", Goal: "Check trace visibility", DoneWhen: "Record who can see previews"}}}},
	}
	prompt, err := coordinatorPromptBounded(state, 131072-len(frame.PromptText()))
	if err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(home, ".codex")
	}
	provider := subscription.Provider{Auth: &subscription.CodexAuth{Home: home}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	completion, err := provider.Complete(ctx, subscription.Request{Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high", Messages: []llmclient.Message{{Role: "system", Content: frame.PromptText()}, {Role: "user", Content: prompt}}})
	if err != nil {
		t.Fatal(err)
	}
	choices := completion["choices"].([]any)
	raw := choices[0].(map[string]any)["message"].(llmclient.Message).Content
	decision, err := parseDecision(raw)
	if err != nil {
		t.Fatalf("invalid model response: %v: %s", err, raw)
	}
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("invalid coordinator decision: %v: %s", err, raw)
	}
	for _, lead := range decision.OpenLeads {
		if strings.Contains(strings.Join(lead.Evidence, " "), traceRef) && strings.Contains(strings.Join(lead.Evidence, " "), portalRef) {
			t.Logf("combined lead: %s; next check: %s", lead.Hypothesis, lead.NextCheck)
			return
		}
	}
	t.Fatal(fmt.Sprintf("coordinator did not preserve a combined cross-worker lead: %s", raw))
}
