package webapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

func TestLiveActiveCoordinatorWebSearch(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_LIVE_COORDINATOR_SEARCH") != "1" {
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
	server := NewServer(Config{RepoRoot: t.TempDir(), WebSearchURL: searchURL, LLM: client})
	current, err := server.newRun("research-smoke", "Explain a current public PHP release", "No target testing")
	if err != nil {
		t.Fatal(err)
	}
	current.started, current.status, current.permissionMode = true, "running", approval.FullAccess
	current.state = assessment.State{Version: 1, ID: current.id, Goal: current.goal, Scope: current.scope, Status: "running", Limits: assessment.DefaultLimits()}
	if err := server.message(ctx, current, "Search the web for a current official PHP security release, read the source page, and explain its main point. This is background research, not a finding about any target.", nil); err != nil {
		t.Fatal(err)
	}
	if len(current.messages) != 2 || strings.TrimSpace(current.messages[1].Text) == "" {
		t.Fatalf("coordinator did not reply: %+v", current.messages)
	}
	evidence, err := os.ReadDir(filepath.Join(current.root, "coordinator-research"))
	if err != nil || len(evidence) < 2 {
		t.Fatalf("expected recorded search and page read; files=%v err=%v", evidence, err)
	}
	t.Logf("coordinator reply: %s; evidence files=%d", current.messages[1].Text, len(evidence))
}
