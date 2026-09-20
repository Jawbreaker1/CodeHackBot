package execx

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
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
	result, err := (Executor{LogDir: t.TempDir()}).Run(context.Background(), Action{Command: "printf", Args: []string{"%s", want}})
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
