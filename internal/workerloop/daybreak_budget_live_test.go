//go:build live

package workerloop

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
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

// A target-free smoke of the actual worker packet and provider request above
// the former Daybreak byte ceiling. It does not execute worker tools.
func TestLiveDaybreakWorkerTokenPacket(t *testing.T) {
	if os.Getenv("BHB_TOKEN_WORKER_PROBE") != "1" {
		t.Skip("set BHB_TOKEN_WORKER_PROBE=1 for the live token packet smoke")
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(userHome, ".codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	bridge, err := subscription.StartLocalBridge(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	client := (llmclient.Client{BaseURL: bridge.BaseURL, AuthTokenFile: bridge.TokenFile, Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high", MaxInputBytes: llmclient.SubscriptionInputByteLimit}).WithDaybreakAssessmentBudget()
	packet := ctxpacket.NewInitialWorkerPacket(
		behavior.Frame{SystemPrompt: "You are reviewing fictional saved observations only. No tools or target contact. Answer the task using the saved text.", AgentsText: "The records are synthetic; report only what they establish."},
		session.Foundation{Goal: "Identify the earliest unresolved fictional observation and its saved reference", ReportingRequirement: "cite the record"}, t.TempDir(), client.Model, "denied", 3,
	)
	packet.RelevantRecentResults = append(packet.RelevantRecentResults, ctxpacket.ExecutionResult{Action: "record-000", OutputSummary: "Earlier unresolved clue: INDIGO-17 requires a later role-boundary check; it is not a finding.", LogRefs: []string{"/fictional/record-000"}})
	for i := 1; i <= 180; i++ {
		packet.RelevantRecentResults = append(packet.RelevantRecentResults, ctxpacket.ExecutionResult{
			Action: fmt.Sprintf("record-%03d", i), OutputEvidence: strings.Repeat("routine fictional observation with no identified weakness. ", 16),
			LogRefs: []string{fmt.Sprintf("/fictional/record-%03d", i)},
		})
	}
	loop := Loop{LLM: client}
	view, err := loop.modelView(&packet, buildUserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	messages := []llmclient.Message{{Role: "system", Content: view.BehaviorFrame.PromptText()}, {Role: "user", Content: buildUserPrompt(view)}}
	if len(messages[0].Content)+len(messages[1].Content) <= llmclient.SubscriptionInputByteLimit || len(view.RelevantRecentResults) != len(packet.RelevantRecentResults) || !strings.Contains(messages[1].Content, "INDIGO-17") {
		t.Fatal("the worker view did not retain the older clue beyond the former byte ceiling")
	}
	completion, err := client.Complete(ctx, messages, llmclient.ChatOptions{Profile: llmclient.ProfileStructuredControl})
	if err != nil {
		t.Fatal(err)
	}
	predicted := client.CountInputTokens(messages)
	if completion.PromptTokens() == 0 || predicted < completion.PromptTokens() || strings.TrimSpace(completion.Text) == "" {
		t.Fatalf("worker packet was not accepted with measured usage: preflight=%d provider=%d", predicted, completion.PromptTokens())
	}
	t.Logf("worker packet: bytes=%d preflight_tokens=%d provider_input_tokens=%d", completion.RequestTextBytes, predicted, completion.PromptTokens())
}
