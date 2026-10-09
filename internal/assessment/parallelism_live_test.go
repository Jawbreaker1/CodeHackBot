//go:build live

package assessment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

// This opt-in model evaluation checks whether task count follows independently
// useful outcomes. It does not run tools or establish vulnerability coverage.
func TestLiveCoordinatorChoosesUsefulParallelism(t *testing.T) {
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
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(home, ".codex")
	}
	provider := subscription.Provider{Auth: &subscription.CodexAuth{Home: home}}
	client := func(ctx context.Context, messages []llmclient.Message) (string, error) {
		completion, err := provider.Complete(ctx, subscription.Request{
			Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high", Messages: messages,
		})
		if err != nil {
			return "", err
		}
		return completion["choices"].([]any)[0].(map[string]any)["message"].(llmclient.Message).Content, nil
	}
	tests := []struct {
		name      string
		goal      string
		scope     string
		wantTasks int
	}{
		{
			name:      "one shared source path",
			goal:      "Assess the local client application build for material security weaknesses using read-only source review and bounded fabricated-data tests.",
			scope:     "Only " + filepath.Join(root, "testdata/client-flow-lab/build-a") + "; do not modify files, start services, or contact any network.",
			wantTasks: 1,
		},
		{
			name:      "two independent source paths",
			goal:      "Assess two separate local applications for material security weaknesses using read-only source review and bounded fabricated-data tests. They do not interact or share state.",
			scope:     "Only " + filepath.Join(root, "testdata/client-flow-lab/build-a") + " and " + filepath.Join(root, "testdata/webhook-lab") + "; do not modify files, start services, or contact any network.",
			wantTasks: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for attempt := 1; attempt <= 3; attempt++ {
				state := State{
					Version: 1, ID: "parallelism-planning-fixture", Goal: tc.goal,
					Scope: tc.scope, Status: "running", Limits: DefaultLimits(),
				}
				prompt, err := coordinatorPromptBounded(state, 131072-len(frame.PromptText()))
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				raw, err := client(ctx, []llmclient.Message{
					{Role: "system", Content: frame.PromptText()},
					{Role: "user", Content: prompt},
				})
				cancel()
				if err != nil {
					t.Fatalf("attempt %d: %v", attempt, err)
				}
				decision, err := parseDecision(raw)
				if err != nil {
					t.Fatalf("attempt %d invalid JSON: %v: %s", attempt, err, raw)
				}
				if err := validateDecision(decision, state); err != nil {
					t.Fatalf("attempt %d invalid plan: %v: %s", attempt, err, raw)
				}
				t.Logf("attempt %d: %d tasks: %s", attempt, len(decision.Tasks), raw)
				if len(decision.Tasks) != tc.wantTasks {
					t.Errorf("attempt %d: wanted %d task(s), got %d", attempt, tc.wantTasks, len(decision.Tasks))
				}
			}
		})
	}
}
