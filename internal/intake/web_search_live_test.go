package intake

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

// Opt-in smoke of the real provider, local search service, and bounded page
// reader. It never contacts an assessment target.
func TestLiveIntakeWebSearch(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_LIVE_INTAKE_SEARCH") != "1" {
		t.Skip("live provider research smoke is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	searchURL := DiscoverLocalWebSearch(ctx)
	if searchURL == "" {
		t.Fatal("local SearXNG is unavailable")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := subscription.StartLocalBridge(ctx, filepath.Join(home, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	toolNames := map[string]bool{}
	inspection := &Inspection{
		Workspace: t.TempDir(), EvidenceDir: t.TempDir(), Connected: true, WebSearchURL: searchURL,
		Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce},
		Emit: func(event assessment.Event) {
			if event.Kind == "execution_started" {
				for _, name := range []string{"web_search", "web_fetch", "vulnerability_search"} {
					if strings.Contains(event.Action, `"name":"`+name+`"`) {
						toolNames[name] = true
					}
				}
			}
		},
	}
	client := llmclient.Client{BaseURL: bridge.BaseURL, AuthTokenFile: bridge.TokenFile, Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high", MaxInputBytes: llmclient.SubscriptionInputByteLimit, MaxOutputTokens: llmclient.SubscriptionMaxOutputTokens}
	conversation := &Conversation{Inspection: inspection}
	turn, err := conversation.Turn(ctx, client, "Search the web for a current official PHP security release notice, read a source page, and tell me what the source says. This is background research, not a test of any target.")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("research reply: %s; tools=%v", turn.Reply, toolNames)
	if !toolNames["web_search"] || !toolNames["web_fetch"] || turn.Reply == "" {
		t.Fatalf("coordinator did not search and read a source: tools=%v reply=%q", toolNames, turn.Reply)
	}
}
