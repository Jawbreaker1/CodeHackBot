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
	cases := []struct{ name, body string }{
		{"prose", strings.Repeat("The fictional service recorded a routine observation. The analyst retained its source and uncertainty.\n", 400)},
		{"code", strings.Repeat("func inspectRecord(input []byte) (int, error) { if len(input) == 0 { return 0, nil }; return len(input), nil }\n", 350)},
		{"log", strings.Repeat("2026-01-01T00:00:00Z fixture INFO request_id=7d3a9b2e status=200 path=/sample/detail elapsed_ms=17\n", 400)},
	}
	for _, item := range cases {
		messages := []llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: item.body}}
		callTimeout := 3 * time.Minute
		if os.Getenv("BHB_PROBE_PROVIDER") != "subscription" {
			callTimeout = 10 * time.Minute
		}
		callCtx, stop := context.WithTimeout(ctx, callTimeout)
		completion, err := client.Complete(callCtx, messages, llmclient.ChatOptions{Profile: llmclient.ProfileConversation})
		stop()
		if err != nil {
			t.Fatalf("%s probe: %v", item.name, err)
		}
		if completion.PromptTokens() <= 0 {
			t.Fatalf("%s probe returned no provider input-token count", item.name)
		}
		t.Logf("model=%s shape=%s request_bytes=%d input_tokens=%d", client.Model, item.name, completion.RequestTextBytes, completion.PromptTokens())
	}
}
