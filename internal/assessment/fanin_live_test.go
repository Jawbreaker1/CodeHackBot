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

// One model-facing smoke of the compact ten-result handoff. It does not
// establish that ten workers can be executed reliably or that a security
// finding is valid.
func TestLiveCoordinatorReadsIndexedFanIn(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{"approval_mode": "operator_selected_session_policy", "research_mode": "connected"})
	if err != nil {
		t.Fatal(err)
	}
	state := State{Version: 1, ID: "fictional-fan-in", Goal: "Review ten independent fictional fixture observations and plan the most useful unresolved check. None is a confirmed vulnerability.", Scope: "Fictional saved records only; no target contact", Model: "gpt-daybreak-blue-latest", Status: "running", Limits: DefaultLimits()}
	plan := Decision{Summary: "Review ten distinct fixture observations"}
	sessionRoot := t.TempDir()
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("fixture-%02d", i)
		task := Task{ID: id, Goal: "Review fictional fixture " + id, DoneWhen: "Record its bounded observation"}
		plan.Tasks = append(plan.Tasks, task)
		plan.ApprovedTaskIDs = append(plan.ApprovedTaskIDs, id)
		summary := id + " returned only its expected synthetic marker; no weakness was established. "
		if i == 0 {
			summary = "Indigo cue: fixture-00 showed a synthetic marker in a preview that may be visible to another role. Visibility was not tested, so no disclosure is established. "
		}
		result := Result{Task: task, Status: "done", Summary: summary + strings.Repeat("The remaining source detail is in the saved result. ", 90)}
		for step := 0; step < 4; step++ {
			result.Evidence = append(result.Evidence, ctxpacket.ExecutionResult{LogRefs: []string{fmt.Sprintf("/fictional/tasks/%s/step-%d.log", id, step)}, OutputEvidence: strings.Repeat("Recorded synthetic observation. ", 160)})
		}
		if err := saveJSON(filepath.Join(sessionRoot, "tasks", id, "result.json"), result); err != nil {
			t.Fatal(err)
		}
		state.Results = append(state.Results, result)
	}
	state.Plans = []Decision{plan}
	prompt, err := coordinatorPromptBounded(state, 48*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "result_ref") || !strings.Contains(prompt, "Indigo cue") || strings.Contains(prompt, "Recorded synthetic observation. Recorded synthetic observation.") {
		t.Fatal("the live request did not use the compact result index")
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
	raw := completion["choices"].([]any)[0].(map[string]any)["message"].(llmclient.Message).Content
	decision, err := parseDecision(raw)
	if err != nil {
		t.Fatalf("invalid model response: %v: %s", err, raw)
	}
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("coordinator decision failed runtime validation: %v: %s", err, raw)
	}
	if strings.TrimSpace(decision.Summary) == "" || len(decision.Findings) != 0 || !strings.Contains(raw, "fixture-00") {
		t.Fatalf("coordinator lost the early unresolved cue or claimed a finding: %s", raw)
	}
	t.Logf("bounded request: %d bytes; next decision: %s", len(prompt), raw)
}

// Diagnostic model check for the bounded coordinator view. The fixture keeps
// an earlier operator boundary while shortening saved worker prose; no target
// contact or worker execution is part of this test.
func TestLiveCoordinatorKeepsEarlierOperatorDirection(t *testing.T) {
	if os.Getenv("BHB_CONTEXT_LIVE_OPERATOR") != "1" {
		t.Skip("set BHB_CONTEXT_LIVE_OPERATOR=1 for the opt-in model check")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{"approval_mode": "operator_selected_session_policy", "research_mode": "connected"})
	if err != nil {
		t.Fatal(err)
	}
	const earlierDirection = "User: Use saved observations only; do not contact the target or run new commands."
	state := State{
		Version: 1, ID: "fictional-context-continuity", Goal: "Explain what the fictional saved observations establish", Scope: "Fictional saved records only; no target contact", Model: "gpt-daybreak-blue-latest", Status: "running", Limits: DefaultLimits(),
		OperatorMessages: []string{earlierDirection, "Assistant: " + strings.Repeat("I will review the saved result. ", 35), "User: What is supported by the records and what remains uncertain?"},
		Plans:            []Decision{{Summary: "Review saved observations", Tasks: []Task{{ID: "review", Goal: "Interpret the fictional saved result"}}}},
		Results:          []Result{{Task: Task{ID: "review", Goal: "Interpret the fictional saved result"}, Status: "done", Summary: "Both fictional identities received the same denial response; this does not establish an access-control weakness. " + strings.Repeat("The complete observation is retained in the saved result. ", 160)}},
	}
	full := coordinatorPrompt(state)
	prompt, err := coordinatorPromptBounded(state, len(full)-2000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, earlierDirection) || len(prompt) >= len(full) {
		t.Fatal("model request did not preserve the earlier operator direction under pressure")
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
	raw := completion["choices"].([]any)[0].(map[string]any)["message"].(llmclient.Message).Content
	decision, err := parseDecision(raw)
	if err != nil {
		t.Fatalf("invalid model response: %v: %s", err, raw)
	}
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("invalid coordinator decision: %v: %s", err, raw)
	}
	t.Logf("bounded request: %d of %d bytes; inspect decision for operator boundary: %s", len(prompt), len(full), raw)
}
