//go:build live

package assessment

import (
	"context"
	"encoding/json"
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

// The coverage reviewer sees a fictional saved result and an otherwise valid
// plan. It should identify a distinct observed sensitive value left untested,
// without contacting any target or asserting that disclosure occurred.
func TestLiveCoverageReviewFindsDistinctUntracedValue(t *testing.T) {
	const ref = "/fictional/tasks/baseline/client-observation.log"
	state := State{
		Goal:  "Focused read-only review of the fictional site",
		Scope: "Only fictional https://fixture.example and its public pages; no outside-host requests or login attempts",
		Plans: []Decision{{Summary: "Inspect public client behavior", Tasks: []Task{{ID: "baseline", Goal: "Inspect public pages and client code"}}, ApprovedTaskIDs: []string{"baseline"}}},
		Results: []Result{{
			Task: Task{ID: "baseline", Goal: "Inspect public pages and client code"}, Status: "done",
			Summary:   "The public bundle reads a one-time verification value from a URL parameter, sends it to a same-origin verification endpoint, then stores the separately returned bearer access token in localStorage. The page loads an outside analytics script before all client initialization is complete. No credential transmission to the outside host was observed. A separate unauthenticated profile endpoint returned 401.",
			LeadHints: []string{"An authenticated report route appears in the bundle but needs controlled accounts to test", "A public form was not submitted within read-only scope"},
			Evidence:  []ctxpacket.ExecutionResult{{LogRefs: []string{ref}, OutputSummary: "Source excerpt records the URL verification value, returned bearer value, and outside script load order."}},
		}},
	}
	decision := Decision{Summary: "Validate stored bearer token and a profile export boundary", Tasks: []Task{
		{ID: "storage", Goal: "Independently confirm the returned access token is stored in localStorage", DoneWhen: "Storage behavior verified"},
		{ID: "export", Goal: "Check one unauthenticated export endpoint", DoneWhen: "Response recorded"},
	}, OpenLeads: []Lead{{ID: "url-value", Hypothesis: "A distinct URL verification value may remain visible during outside script loading", Evidence: []string{ref}, NextCheck: "Inspect load order"}}, Gaps: []string{"Dynamic browser execution and outside-host requests were outside this read-only review; no transmission of either value was observed."}}
	input, _ := coverageReviewInput(state, decision)
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
	for attempt := 1; attempt <= 2; attempt++ {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
		completion, err := provider.Complete(ctx, subscription.Request{
			Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high",
			Messages: []llmclient.Message{{Role: "system", Content: coverageReviewInstructions}, {Role: "user", Content: string(input)}},
		})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		raw := completion["choices"].([]any)[0].(map[string]any)["message"].(llmclient.Message).Content
		var review coverageReview
		if err := json.Unmarshal([]byte(raw), &review); err != nil {
			t.Fatalf("invalid response: %v: %s", err, raw)
		}
		t.Logf("attempt %d coverage review: %s", attempt, raw)
		found := false
		for _, omission := range review.Omissions {
			question := strings.ToLower(omission.Question + " " + omission.NextCheck)
			if strings.Contains(question, "url") && (strings.Contains(question, "script") || strings.Contains(question, "analytic")) && len(omission.Evidence) == 1 && omission.Evidence[0] == ref {
				found = true
			}
		}
		if !found {
			t.Fatalf("attempt %d missed the separate URL-value path", attempt)
		}
	}
}

// A fictional planning replay checks that a previously noted minor issue does
// not make the coordinator finish while a distinct observed surface is still
// unexamined. No target request or worker action runs in this test.
func TestLiveCoordinatorRevisitsObservedUntestedSurface(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{
		"approval_mode": "operator_selected_session_policy",
		"research_mode": "connected",
	})
	if err != nil {
		t.Fatal(err)
	}
	const inventoryRef = "/fictional/tasks/baseline/surface-inventory.log"
	const headersRef = "/fictional/tasks/baseline/headers.log"
	state := State{
		Version: 1, ID: "fictional-surface-handoff", Status: "running",
		Goal:     "Perform a focused, low-impact security review of a fictional web application and document the observed behavior, material gaps, and useful next checks.",
		Scope:    "Only fictional https://fixture.example and its directly linked public pages; low-intensity read-only requests and client-code review are allowed. No login attempts or state changes.",
		Approach: &Approach{ID: "focused", Label: "Focused", Description: "Small representative sample of directly linked public pages and client code"},
		Model:    "gpt-daybreak-blue-latest", Limits: Limits{Workers: 2, Rounds: 8, Tasks: 12, StepsPerTask: 16, ModelCalls: 120},
		ProjectHistory: &ProjectHistory{Project: "fictional", Target: "fixture.example", Sessions: []PriorSession{{
			ID: "older-review", Findings: []PriorFinding{{ID: "prior-policy", Title: "Permissive script policy", Severity: "low", Status: "candidate"}},
		}}},
		Plans: []Decision{{Summary: "Inspect a small public sample", Tasks: []Task{{ID: "baseline", Goal: "Inspect the landing page and representative public responses", DoneWhen: "Record observed behavior and material unexamined surfaces"}}}},
		Results: []Result{{
			Task:   Task{ID: "baseline", Goal: "Inspect the landing page and representative public responses", DoneWhen: "Record observed behavior and material unexamined surfaces"},
			Status: "done", Summary: "The landing and status pages responded normally. The script policy allows inline code, as in the older review; no injection point was observed. The saved landing-page inventory also records a directly linked /account page and its client bundle; neither was examined, so their security behavior is unknown.",
			LeadHints: []string{"The directly linked /account page and client bundle appear in /fictional/tasks/baseline/surface-inventory.log but were not examined. They may expose a different session or access boundary; inspect them with bounded read-only requests and client-code review."},
			Evidence: []ctxpacket.ExecutionResult{
				{LogRefs: []string{inventoryRef}, OutputSummary: "The landing page links to /account and a client bundle."},
				{LogRefs: []string{headersRef}, OutputSummary: "The landing and status pages use a permissive script policy."},
			},
		}},
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
	for _, scenario := range []struct {
		name  string
		state State
	}{
		{name: "completed handoff", state: state},
		{name: "blocked handoff", state: func() State {
			blocked := state
			blocked.Results = append([]Result(nil), state.Results...)
			blocked.Results[0].Status = "blocked"
			blocked.Results[0].LeadHints = nil
			blocked.Results[0].Summary = "The worker stopped before its overall task was complete. Its saved observations show a linked /account page and a client bundle that were not examined. A minor script policy weakness was observed on the landing page."
			return blocked
		}()},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			prompt, err := coordinatorPromptBounded(scenario.state, 131072-len(frame.PromptText()))
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 1; attempt <= 3; attempt++ {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				completion, err := provider.Complete(ctx, subscription.Request{
					Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high",
					Messages: []llmclient.Message{{Role: "system", Content: frame.PromptText()}, {Role: "user", Content: prompt}},
				})
				cancel()
				if err != nil {
					t.Fatalf("attempt %d: %v", attempt, err)
				}
				raw := completion["choices"].([]any)[0].(map[string]any)["message"].(llmclient.Message).Content
				decision, err := parseDecision(raw)
				if err != nil {
					t.Fatalf("attempt %d invalid response: %v: %s", attempt, err, raw)
				}
				if err := validateDecision(decision, scenario.state); err != nil {
					t.Fatalf("attempt %d invalid decision: %v: %s", attempt, err, raw)
				}
				t.Logf("attempt %d decision: %s", attempt, raw)
				if decision.Complete || len(decision.Tasks) == 0 {
					t.Fatalf("attempt %d finished without checking the distinct observed surface", attempt)
				}
				assigned := false
				for _, task := range decision.Tasks {
					if strings.Contains(task.Goal, "/account") || strings.Contains(task.Goal, "baseline") {
						assigned = true
					}
				}
				if !assigned {
					t.Fatalf("attempt %d did not assign review of the observed account surface or its saved evidence", attempt)
				}
			}
		})
	}
}

// The first worker may notice credential storage and an outside script without
// tracing the value's origin or destination. The next plan should investigate
// that missing trust-boundary link rather than equating the two observations
// with a confirmed leak or spending the round only on a minor header retest.
func TestLiveCoordinatorRequestsMissingSensitiveDataFlow(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{"approval_mode": "operator_selected_session_policy", "research_mode": "connected"})
	if err != nil {
		t.Fatal(err)
	}
	state := State{
		Version: 1, ID: "fictional-data-flow", Status: "running",
		Goal:     "Perform a focused, read-only security assessment of a fictional public web application and report evidence and material gaps.",
		Scope:    "Only fictional https://fixture.example and directly linked public pages; read-only requests and client-code review. No login attempts, state changes, or outside-host requests.",
		Approach: &Approach{ID: "focused", Label: "Focused"},
		Model:    "gpt-daybreak-blue-latest", Limits: Limits{Workers: 2, Rounds: 8, Tasks: 12, StepsPerTask: 16, ModelCalls: 120},
		Plans: []Decision{{Summary: "Inspect public pages and client code", Tasks: []Task{{ID: "baseline", Goal: "Assess the public surface and one safe access boundary", DoneWhen: "Explain tested behavior and unresolved material paths"}}}},
		Results: []Result{{
			Task:    Task{ID: "baseline", Goal: "Assess the public surface and one safe access boundary", DoneWhen: "Explain tested behavior and unresolved material paths"},
			Status:  "done",
			Summary: "The landing and dashboard pages were inspected. Public client code reads a one-time verification value from a URL query parameter, posts it to a same-host verification endpoint, then stores the returned bearer access token in localStorage and later sends it in Authorization headers. The dashboard HTML loads an outside analytics script, and a weak inline-script policy was observed. One unauthenticated account endpoint returned 401. No analytics request carrying either value was observed, and no high-impact issue was confirmed.",
			LeadHints: []string{
				"An account export route appears in /fictional/tasks/baseline/client-code.log but was not tested; check it with one credential-free GET.",
				"Authenticated report identifiers appear in /fictional/tasks/baseline/client-code.log; check object authorization later with controlled accounts.",
				"A state-changing public form appears in /fictional/tasks/baseline/dashboard.log; review it only under separate approval.",
			},
			Evidence: []ctxpacket.ExecutionResult{
				{LogRefs: []string{"/fictional/tasks/baseline/client-code.log"}, OutputSummary: "Public bundle reads a URL verification value, exchanges it, then stores the separate access token in browser storage."},
				{LogRefs: []string{"/fictional/tasks/baseline/dashboard.log"}, OutputSummary: "Dashboard loads an outside analytics script."},
				{LogRefs: []string{"/fictional/tasks/baseline/account.log"}, OutputSummary: "Credential-free account request returned 401."},
			},
		}},
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
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		completion, err := provider.Complete(ctx, subscription.Request{
			Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high",
			Messages: []llmclient.Message{{Role: "system", Content: frame.PromptText()}, {Role: "user", Content: prompt}},
		})
		cancel()
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		raw := completion["choices"].([]any)[0].(map[string]any)["message"].(llmclient.Message).Content
		decision, err := parseDecision(raw)
		if err != nil {
			t.Fatalf("attempt %d invalid response: %v: %s", attempt, err, raw)
		}
		if err := validateDecision(decision, state); err != nil {
			t.Fatalf("attempt %d invalid decision: %v: %s", attempt, err, raw)
		}
		t.Logf("attempt %d decision: %s", attempt, raw)
		if decision.Complete {
			t.Fatalf("attempt %d completed while the sensitive-data path remained untraced", attempt)
		}
		tracesFlow := false
		for _, task := range decision.Tasks {
			goal := strings.ToLower(task.Goal + " " + task.DoneWhen)
			if (strings.Contains(goal, "url") || strings.Contains(goal, "query") || strings.Contains(goal, "verification")) &&
				(strings.Contains(goal, "script") || strings.Contains(goal, "analytic") || strings.Contains(goal, "telemetry") || strings.Contains(goal, "load order")) {
				tracesFlow = true
			}
		}
		if !tracesFlow {
			t.Fatalf("attempt %d did not delegate a bounded review of the URL credential's path to outside scripts", attempt)
		}
	}
}
