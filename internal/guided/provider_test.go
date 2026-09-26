package guided

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestSavePreferencesAppliesSubscriptionInputBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "preferences.json")

	got, err := savePreferences(ctx, NewConsole(ctx, strings.NewReader(""), io.Discard), path, preferences{Provider: "subscription", Model: "gpt-daybreak-blue-latest"})
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxInputBytes != SubscriptionInputByteLimit {
		t.Fatalf("subscription input bytes = %d, want %d", got.MaxInputBytes, SubscriptionInputByteLimit)
	}
}

func TestSavePreferencesUpdatesQwen38ContextBudget(t *testing.T) {
	for _, test := range []struct {
		name  string
		model string
		old   int
		want  int
	}{
		{"new Qwen", "qwen/qwen3.8-27b", 0, llmclient.Qwen38LabInputByteLimit},
		{"old Qwen default", "qwen/qwen3.8-27b", llmclient.DefaultInputByteLimit, llmclient.Qwen38LabInputByteLimit},
		{"custom Qwen limit", "qwen/qwen3.8-27b", 72 * 1024, 72 * 1024},
		{"other local model", "other/local-model", 0, llmclient.DefaultInputByteLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "preferences.json")
			got, err := savePreferences(ctx, NewConsole(ctx, strings.NewReader(""), io.Discard), path, preferences{Provider: "local", Model: test.model, ReasoningEffort: "low", MaxInputBytes: test.old})
			if err != nil || got.MaxInputBytes != test.want {
				t.Fatalf("input bytes = %d, want %d; err=%v", got.MaxInputBytes, test.want, err)
			}
		})
	}
}

func TestSavedSubscriptionPreferencesRejectInputAsModel(t *testing.T) {
	if validSavedPreferences(preferences{Provider: "subscription", Model: "Hello who are you?"}) {
		t.Fatal("input text must not be accepted as a subscription model id")
	}
	if !validSavedPreferences(preferences{Provider: "subscription", Model: "gpt-daybreak-blue-latest"}) {
		t.Fatal("valid subscription model was rejected")
	}
}
