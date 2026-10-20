//go:build live

package assessment

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

// This opt-in diagnostic measures provider input tokens for several harmless
// request shapes. It contacts only the configured model, never a test target.
// No prompt or model response is saved in the repository.
func TestLiveInputTokenProbe(t *testing.T) {
	if os.Getenv("BHB_TOKEN_PROBE") != "1" {
		t.Skip("set BHB_TOKEN_PROBE=1 for live model token measurements")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	client := llmclient.Client{Model: os.Getenv("BHB_PROBE_MODEL"), MaxInputBytes: 128 * 1024}
	if client.Model == "" {
		t.Fatal("BHB_PROBE_MODEL is required")
	}
	if os.Getenv("BHB_PROBE_PROVIDER") == "subscription" {
		home := os.Getenv("CODEX_HOME")
		if home == "" {
			userHome, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			home = filepath.Join(userHome, ".codex")
		}
		bridge, err := subscription.StartLocalBridge(ctx, home)
		if err != nil {
			t.Fatal(err)
		}
		defer bridge.Close()
		client.BaseURL, client.AuthTokenFile, client.ReasoningEffort = bridge.BaseURL, bridge.TokenFile, "high"
	} else {
		client.BaseURL = os.Getenv("BHB_PROBE_BASE_URL")
		client.ReasoningEffort, client.MaxOutputTokens = "low", 32768
		client.MaxInputBytes = 96 * 1024
		client.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
		if client.BaseURL == "" {
			t.Fatal("BHB_PROBE_BASE_URL is required for a local model")
		}
	}
	const system = "This is a harmless context-size measurement. Reply with OK only. Do not run tools."
	cases := []struct {
		name     string
		messages []llmclient.Message
	}{
		{"prose", []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("The fictional service recorded a routine observation. The analyst retained its source and uncertainty.\n", 400)}}},
		{"code", []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("func inspectRecord(input []byte) (int, error) { if len(input) == 0 { return 0, nil }; return len(input), nil }\n", 350)}}},
		{"log", []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("2026-01-01T00:00:00Z fixture INFO request_id=7d3a9b2e status=200 path=/sample/detail elapsed_ms=17\n", 400)}}},
		{"unicode", []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("Fiktiv granskning: åtgärd, säkerhet, spårbarhet. 日本語の記録。🔎\n", 500)}}},
		{"json", []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat(`{"fixture_id":"observation-001","status":"recorded","ports":[80,443],"verified":false}`+"\n", 450)}}},
		{"multi-turn", []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("First fictional observation.\n", 350)}, {Role: "assistant", Content: "The first observation was recorded."}, {Role: "user", Content: strings.Repeat("Second fictional observation.\n", 350)}}},
	}
	if os.Getenv("BHB_TOKEN_PROBE_LARGE") == "1" {
		client.MaxInputBytes = 1024 * 1024
		cases = append(cases, struct {
			name     string
			messages []llmclient.Message
		}{"large-log", []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("2026-01-01T00:00:00Z fixture INFO request_id=7d3a9b2e status=200 path=/sample/detail elapsed_ms=17\n", 3000)}}})
		cases = append(cases, struct {
			name     string
			messages []llmclient.Message
		}{"large-prose", []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("The fictional service recorded a routine observation. The analyst retained its source and uncertainty.\n", 7000)}}})
	}
	for _, item := range cases {
		if only := os.Getenv("BHB_TOKEN_PROBE_ONLY"); only != "" && item.name != only {
			continue
		}
		callTimeout := 3 * time.Minute
		if os.Getenv("BHB_PROBE_PROVIDER") != "subscription" {
			callTimeout = 10 * time.Minute
		}
		callCtx, stop := context.WithTimeout(ctx, callTimeout)
		completion, err := client.Complete(callCtx, item.messages, llmclient.ChatOptions{Profile: llmclient.ProfileConversation})
		stop()
		if err != nil {
			t.Fatalf("%s probe: %v", item.name, err)
		}
		if completion.PromptTokens() <= 0 {
			t.Fatalf("%s probe returned no provider input-token count", item.name)
		}
		if os.Getenv("BHB_PROBE_PROVIDER") == "subscription" {
			predicted := llmclient.DaybreakTextInputTokens(item.messages)
			if predicted < completion.PromptTokens() {
				t.Fatalf("%s undercounted provider input: preflight=%d provider=%d", item.name, predicted, completion.PromptTokens())
			}
			t.Logf("model=%s shape=%s request_bytes=%d preflight_tokens=%d provider_input_tokens=%d", client.Model, item.name, completion.RequestTextBytes, predicted, completion.PromptTokens())
		} else {
			t.Logf("model=%s shape=%s request_bytes=%d provider_input_tokens=%d", client.Model, item.name, completion.RequestTextBytes, completion.PromptTokens())
		}
	}
}
