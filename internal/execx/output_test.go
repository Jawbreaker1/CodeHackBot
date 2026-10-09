package execx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestOutputIsAvailableDuringExecution(t *testing.T) {
	executor := Executor{LogDir: t.TempDir()}
	plan, err := executor.Plan(Action{Command: "printf live-evidence; sleep 5", UseShell: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Result, 1)
	go func() { result, _ := executor.RunPlanned(ctx, plan); done <- result }()
	deadline := time.Now().Add(2 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(plan.LogPath + ".stdout")
		if string(data) == "live-evidence" {
			observed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	result := <-done
	if !observed {
		t.Fatal("output unavailable while tool was running")
	}
	log, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "status: aborted") {
		t.Fatalf("aborted execution not recorded: %s", log)
	}
}

func TestLargeOutputUsesBoundedPreviewAndCompleteArtifact(t *testing.T) {
	want := strings.Repeat("evidence\n", outputPreviewBytes)
	source := filepath.Join(t.TempDir(), "large-output.txt")
	if err := os.WriteFile(source, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (Executor{LogDir: t.TempDir()}).Run(context.Background(), Action{Command: "cat", Args: []string{source}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.StdoutSummary) > outputPreviewBytes+100 || !strings.Contains(result.StdoutSummary, "preview truncated") {
		t.Fatalf("preview length=%d", len(result.StdoutSummary))
	}
	data, err := os.ReadFile(result.ArtifactRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatal("output artifact was truncated")
	}
}

func TestMediumOutputKeepsObservationBeyondFormerHeadPreview(t *testing.T) {
	const observation = "DECISIVE_MIDDLE_OBSERVATION"
	want := strings.Repeat("routine line\n", 3000) + observation + "\n"
	result, err := (Executor{LogDir: t.TempDir()}).Run(context.Background(), Action{Command: "printf", Args: []string{"%s", want}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.StdoutSummary, observation) || strings.Contains(result.StdoutSummary, "preview truncated") {
		t.Fatalf("medium output lost a later observation; preview bytes=%d", len(result.StdoutSummary))
	}
	packet := ctxpacket.NewInitialWorkerPacket(behavior.Frame{SystemPrompt: "policy"}, session.Foundation{Goal: "review source"}, t.TempDir(), "fixture", "per_action", 8)
	packet.LatestExecutionResult = ctxpacket.ExecutionResult{Action: "read source matches", OutputEvidence: result.StdoutSummary, LogRefs: []string{result.LogPath}}
	for i := 0; i < 5; i++ {
		packet.RelevantRecentResults = append(packet.RelevantRecentResults, ctxpacket.ExecutionResult{Action: "earlier review", OutputEvidence: strings.Repeat("prior observation ", 1200), LogRefs: []string{"/fixture/earlier"}})
	}
	view, err := packet.ModelView(96 * 1024)
	if err != nil || len(view.Render()) > 96*1024 || !strings.Contains(view.Render(), observation) {
		t.Fatalf("later observation did not reach the budgeted worker model packet: %v", err)
	}
	if len(packet.RelevantRecentResults[0].OutputEvidence) != len(strings.Repeat("prior observation ", 1200)) {
		t.Fatal("context projection changed durable older evidence")
	}
}
