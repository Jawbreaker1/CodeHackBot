package guided

import (
	"context"
	"fmt"
	"io"
	"os"
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
	if got.ReasoningEffort != "high" {
		t.Fatalf("subscription reasoning = %q, want high", got.ReasoningEffort)
	}
	other, err := savePreferences(ctx, NewConsole(ctx, strings.NewReader(""), io.Discard), filepath.Join(t.TempDir(), "other.json"), preferences{Provider: "subscription", Model: "other-model"})
	if err != nil || other.ReasoningEffort != "" {
		t.Fatalf("unselected model should retain provider default: %+v, %v", other, err)
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
	if validSavedPreferences(preferences{Provider: "subscription", Model: "/settings"}) {
		t.Fatal("a CLI command must not be accepted as a subscription model id")
	}
	if !validSavedPreferences(preferences{Provider: "subscription", Model: "gpt-daybreak-blue-latest"}) {
		t.Fatal("valid subscription model was rejected")
	}
}

func TestConfiguredModelsReplaceAccidentalChatText(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "config", "model-profiles.local.json")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0700); err != nil {
		t.Fatal(err)
	}
	profiles := `{"default":"daybreak","profiles":[{"id":"daybreak","label":"Daybreak Blue","provider":"subscription","model":"gpt-daybreak-blue-latest","managed_bridge":true,"reasoning_effort":"high"},{"id":"qwen38","label":"Qwen 3.8","provider":"local","base_url":"http://127.0.0.1:1234/v1","model":"qwen/qwen3.8-27b","reasoning_effort":"low","structured_json":true,"max_input_bytes":98304}]}`
	if err := os.WriteFile(profilePath, []byte(profiles), 0600); err != nil {
		t.Fatal(err)
	}
	preferencesPath := filepath.Join(root, ".birdhackbot", "preferences.json")
	if err := os.MkdirAll(filepath.Dir(preferencesPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preferencesPath, []byte(`{"provider":"subscription","model":"Hello who are you?"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	reader, writer := io.Pipe()
	defer writer.Close()
	c := newConsole(ctx, reader, io.Discard, func(event consoleEvent) {
		if event.kind == consolePrompt {
			go func() { _, _ = fmt.Fprintln(writer, "2") }()
		}
	})
	got, err := configureProvider(ctx, c, preferencesPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "local" || got.Model != "qwen/qwen3.8-27b" || got.ReasoningEffort != "low" || !got.StructuredJSON || got.MaxInputBytes != 98304 {
		t.Fatalf("configured local model was not selected intact: %+v", got)
	}
	data, err := os.ReadFile(preferencesPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Hello who are you?") {
		t.Fatal("accidental chat text remained in saved model preferences")
	}
	reader2, writer2 := io.Pipe()
	defer writer2.Close()
	var prompts []string
	c2 := newConsole(ctx, reader2, io.Discard, func(event consoleEvent) {
		if event.kind == consolePrompt {
			prompts = append(prompts, event.text)
			go func() { _, _ = fmt.Fprintln(writer2, "1") }()
		}
	})
	next, err := configureProviderSelection(ctx, c2, preferencesPath, true)
	if err != nil || next.Provider != "subscription" || next.Model != "gpt-daybreak-blue-latest" {
		t.Fatalf("settings did not switch to the configured Daybreak profile: %+v, %v", next, err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Choose a model") {
		t.Fatalf("settings inserted an unexpected saved-model prompt: %#v", prompts)
	}
}
