package guided

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

func TestLiveCLIActiveCoordinatorWebSearch(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_LIVE_CLI_SEARCH") != "1" {
		t.Skip("live provider research smoke is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	searchURL := intake.DiscoverLocalWebSearch(ctx)
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
	client := llmclient.Client{BaseURL: bridge.BaseURL, AuthTokenFile: bridge.TokenFile, Model: "gpt-daybreak-blue-latest", ReasoningEffort: "high", MaxInputBytes: llmclient.SubscriptionInputByteLimit, MaxOutputTokens: llmclient.SubscriptionMaxOutputTokens}
	inspection := &intake.Inspection{Workspace: t.TempDir(), EvidenceDir: t.TempDir(), Connected: true, WebSearchURL: searchURL, Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce}}
	reply, err := coordinatorChatWithResearch(ctx, client, behavior.Frame{}, []byte(`{"status":"running","scope":"No target testing"}`), nil,
		"Search the web for a current official PHP security release, read the source page, and explain its main point. This is background research, not a target finding.", inspection)
	if err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(inspection.EvidenceDir)
	if err != nil || len(files) < 2 || !strings.Contains(strings.ToLower(reply), "php") {
		t.Fatalf("CLI coordinator did not search and read a public source: reply=%q files=%v err=%v", reply, files, err)
	}
	t.Logf("CLI coordinator reply: %s; evidence files=%d", reply, len(files))
}
