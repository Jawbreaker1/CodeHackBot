package workerloop

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextinspect"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

// Opt-in real-model diagnostic: the required observation is absent from the
// bounded request but present in the worker's saved result catalog and log.
func TestLiveOffloadedResultRecall(t *testing.T) {
	if os.Getenv("BHB_CONTEXT_LIVE_OFFLOAD") != "1" {
		t.Skip("set BHB_CONTEXT_LIVE_OFFLOAD=1 for a self-contained subscription smoke")
	}
	tokenFile := os.Getenv("BHB_CONTEXT_TOKEN_FILE")
	baseURL := "http://127.0.0.1:8787/v1"
	if tokenFile == "" {
		home := os.Getenv("CODEX_HOME")
		if home == "" {
			userHome, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			home = filepath.Join(userHome, ".codex")
		}
		bridge, err := subscription.StartLocalBridge(t.Context(), home)
		if err != nil {
			t.Fatal(err)
		}
		defer bridge.Close()
		tokenFile, baseURL = bridge.TokenFile, bridge.BaseURL
	}
	root := os.Getenv("BHB_CONTEXT_RUN_DIR")
	if root == "" {
		root = t.TempDir()
	} else if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(root, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	marker := fmt.Sprintf("TRACE-%x", random)
	oldRef := filepath.Join(logDir, "old-source-status")
	if err := os.WriteFile(oldRef+".stdout", []byte("Exact verification tag: "+marker+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	goal := "Find the exact verification tag in the saved stdout of the old-source-status observation. Cite its saved log reference. Use recorded context only; do not execute any commands."
	packet := ctxpacket.NewInitialWorkerPacket(
		behavior.Frame{SystemPrompt: "You are a worker reviewing synthetic saved observations. Use only recorded context and do not run commands.", AgentsText: "Read only the saved synthetic fixture context.", Parameters: map[string]string{"scope": "saved synthetic observations only"}},
		session.Foundation{Goal: goal, ReportingRequirement: "cite the exact saved observation"}, root, "gpt-daybreak-blue-latest", "denied", 8,
	)
	packet.CurrentStep.DoneCondition = "Report the exact verification tag and the registered stdout log reference"
	for i := 0; i < 80; i++ {
		ref := filepath.Join(logDir, fmt.Sprintf("routine-%03d", i))
		packet.RelevantRecentResults = append(packet.RelevantRecentResults, ctxpacket.ExecutionResult{
			Action: fmt.Sprintf("routine saved observation %03d", i), ActualExec: fmt.Sprintf("read synthetic note %03d", i),
			ExitStatus: "0", OutputSummary: strings.Repeat("Routine synthetic observation with no verification tag. ", 8),
			OutputEvidence: strings.Repeat("ordinary recorded detail ", 18), LogRefs: []string{ref},
		})
	}
	packet.RelevantRecentResults = append(packet.RelevantRecentResults, ctxpacket.ExecutionResult{
		Action: "old-source-status", ActualExec: "read saved synthetic status", ExitStatus: "0",
		OutputSummary: "Earlier source-status observation stored in the registered stdout log; exact tag is not in this preview.",
		LogRefs:       []string{oldRef},
	})
	inputBytes := llmclient.DaybreakAssessmentInputBytes
	if configured := os.Getenv("BHB_CONTEXT_INPUT_BYTES"); configured != "" {
		value, err := strconv.Atoi(configured)
		if err != nil || value <= 0 {
			t.Fatalf("invalid BHB_CONTEXT_INPUT_BYTES %q", configured)
		}
		inputBytes = value
	}
	inputTokens := 18_000
	if configured := os.Getenv("BHB_CONTEXT_INPUT_TOKENS"); configured != "" {
		value, err := strconv.Atoi(configured)
		if err != nil || value <= 0 {
			t.Fatalf("invalid BHB_CONTEXT_INPUT_TOKENS %q", configured)
		}
		inputTokens = value
	}
	client := llmclient.Client{BaseURL: baseURL, Model: "gpt-daybreak-blue-latest", AuthTokenFile: tokenFile, ReasoningEffort: "high",
		MaxInputBytes: inputBytes, MaxInputTokens: inputTokens, CountInputTokens: llmclient.DaybreakTextInputTokens}
	loop := Loop{LLM: client, Executor: execx.Executor{LogDir: logDir}, Approver: approval.StaticApprover{Decision: approval.DecisionDeny}, Inspector: contextinspect.Recorder{Dir: filepath.Join(root, "context")}}
	initial, err := loop.modelView(&packet, buildUserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	oldCardRetained := false
	for _, result := range initial.RelevantRecentResults {
		oldCardRetained = oldCardRetained || result.Action == "old-source-status"
	}
	if initial.OffloadedResultCount == 0 || oldCardRetained || strings.Contains(initial.Render(), marker) {
		t.Fatalf("fixture did not offload the oldest result: retained=%d offloaded=%d", len(initial.RelevantRecentResults), initial.OffloadedResultCount)
	}
	if err := sessionstate.Save(filepath.Join(root, "session-initial.json"), sessionstate.State{Status: "running", Model: client.Model, MaxSteps: 8, Packet: packet}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	out, runErr := loop.Run(ctx, packet, 8)
	if err := sessionstate.Save(filepath.Join(root, "session.json"), sessionstate.State{Status: out.Packet.TaskRuntime.State, Model: client.Model, MaxSteps: 8, Packet: out.Packet, Summary: out.Summary}); err != nil {
		t.Fatal(err)
	}
	if runErr != nil || out.Packet.TaskRuntime.State != "done" || !strings.Contains(out.Summary, marker) || !strings.Contains(out.Summary, oldRef) {
		t.Fatalf("old log was not recovered exactly: state=%s summary=%q err=%v; inspect %s", out.Packet.TaskRuntime.State, out.Summary, runErr, root)
	}
	first, err := os.ReadFile(filepath.Join(root, "context", "step-001-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(first), marker) {
		t.Fatal("first model request leaked the hidden tag")
	}
	requests, err := filepath.Glob(filepath.Join(root, "context", "step-*-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	seenExactRecall := false
	for _, path := range requests[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		seenExactRecall = seenExactRecall || strings.Contains(string(data), marker)
	}
	if !seenExactRecall {
		t.Fatal("exact saved log text never reached a later model request")
	}
	t.Logf("offloaded result recalled from %d saved cards in %s", len(packet.RelevantRecentResults), root)
}
