package workerloop

import (
	"context"
	"os"
	"path/filepath"
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
)

// Opt-in smoke for the configured subscription bridge. It exercises model-led
// retrieval from a pressured worker packet without contacting a test target.
// Deterministic long-session tests remain the regression gate.
func TestLiveSavedConversationRecallSmoke(t *testing.T) {
	if os.Getenv("BHB_CONTEXT_LIVE") != "1" {
		t.Skip("set BHB_CONTEXT_LIVE=1 with a local bridge token file")
	}
	tokenFile := os.Getenv("BHB_CONTEXT_TOKEN_FILE")
	if tokenFile == "" {
		t.Fatal("BHB_CONTEXT_TOKEN_FILE is required")
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
	ref := filepath.Join(logDir, "fixture-observation")
	if err := os.WriteFile(ref+".stdout", []byte("A synthetic prior observation exists.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const marker = "LABEL-COPPER-17"
	goal := "Find the exact synthetic report label from the early operator message about report labels. Quote the label and its saved conversation reference. Use read-only context recall; do not run commands."
	packet := ctxpacket.NewInitialWorkerPacket(
		behavior.Frame{SystemPrompt: "You are a worker reviewing saved synthetic notes. Use only recorded context. Do not run commands.", AgentsText: "Read only the saved synthetic fixture context.", Parameters: map[string]string{"scope": "saved synthetic notes only"}},
		session.Foundation{Goal: goal, ReportingRequirement: "cite the saved message"}, root, "gpt-daybreak-blue-latest", "denied", 6,
	)
	packet.CurrentStep.DoneCondition = "Give the exact label and the saved conversation reference"
	packet.RecentConversation = append(packet.RecentConversation, "Operator answer: report label instruction: use "+marker+" for the final synthetic record.")
	for i := 0; i < 200; i++ {
		packet.RecentConversation = append(packet.RecentConversation, "User: ordinary note about section order "+strings.Repeat("neutral fixture detail ", 9))
		packet.RecentConversation = append(packet.RecentConversation, "Assistant: recorded the section order without changing the report label")
	}
	packet.LatestExecutionResult = ctxpacket.ExecutionResult{Action: "read synthetic observation", ExitStatus: "0", OutputSummary: "Synthetic prior observation recorded", LogRefs: []string{ref}}
	client := llmclient.Client{BaseURL: "http://127.0.0.1:8787/v1", Model: "gpt-daybreak-blue-latest", AuthTokenFile: tokenFile, ReasoningEffort: "high", MaxInputBytes: 48 * 1024}
	initial, err := packet.ModelView(client.InputByteLimit() - 8192)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(initial.Render(), marker) {
		t.Fatal("fixture did not hide the early operator label under context pressure")
	}
	loop := Loop{LLM: client, Executor: execx.Executor{LogDir: logDir}, Approver: approval.StaticApprover{Decision: approval.DecisionDeny}, Inspector: contextinspect.Recorder{Dir: filepath.Join(root, "context")}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := loop.Run(ctx, packet, 6)
	if saveErr := sessionstate.Save(filepath.Join(root, "session.json"), sessionstate.State{Status: out.Packet.TaskRuntime.State, Model: client.Model, MaxSteps: 6, Packet: out.Packet}); saveErr != nil {
		t.Fatal(saveErr)
	}
	if err != nil || out.Packet.TaskRuntime.State != "done" || !strings.Contains(out.Summary, marker) {
		t.Fatalf("saved operator label was not recovered: state=%s summary=%q err=%v; inspect %s", out.Packet.TaskRuntime.State, out.Summary, err, root)
	}
	if out.Packet.WorkProgress.ExecutedActions != 1 {
		t.Fatal("smoke ran a command instead of using saved context")
	}
	t.Logf("synthetic context evidence: %s", root)
}
