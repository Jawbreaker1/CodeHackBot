package interactivecli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
)

type stubRunner struct {
	outcomes []workerloop.Outcome
	errs     []error
	calls    []ctxpacket.WorkerPacket
}

func (s *stubRunner) Run(_ context.Context, packet ctxpacket.WorkerPacket, _ int) (workerloop.Outcome, error) {
	s.calls = append(s.calls, packet)
	idx := len(s.calls) - 1
	return s.outcomes[idx], s.errs[idx]
}

func TestShellRunMaintainsConversationAcrossTurns(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/AGENTS.md", "rules")
	mustWrite(t, root+"/go.mod", "module example.com/test\n")

	runner := &stubRunner{
		outcomes: []workerloop.Outcome{
			{Summary: "first done", Packet: ctxpacket.WorkerPacket{BehaviorFrame: behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"}, SessionFoundation: session.Foundation{Goal: "first goal", ReportingRequirement: "owasp"}, RecentConversation: []string{"User: first goal"}}},
			{Summary: "second done", Packet: ctxpacket.WorkerPacket{BehaviorFrame: behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"}, SessionFoundation: session.Foundation{Goal: "first goal", ReportingRequirement: "owasp"}, RecentConversation: []string{"User: first goal", "Assistant: first done", "User: next step"}}},
		},
		errs: []error{nil, nil},
	}
	var out strings.Builder
	shell := Shell{
		Reader:    strings.NewReader("first goal\nnext step\nexit\n"),
		Writer:    &out,
		Runner:    runner,
		RepoRoot:  root,
		BaseURL:   "http://127.0.0.1:1234/v1",
		Model:     "test-model",
		MaxSteps:  2,
		AllowAll:  true,
		StatePath: root + "/session.json",
	}
	if err := shell.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("runner calls = %d", len(runner.calls))
	}
	if !strings.Contains(out.String(), "first done") || !strings.Contains(out.String(), "second done") {
		t.Fatalf("output missing summaries: %q", out.String())
	}
	if got := runner.calls[1].RecentConversation; len(got) == 0 || got[len(got)-1] != "User: next step" {
		t.Fatalf("second call conversation = %#v", got)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
