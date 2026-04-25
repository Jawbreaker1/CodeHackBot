package interactivecli

import (
	"errors"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestNewUIStateStartsWithPromptAndBanner(t *testing.T) {
	state := NewUIState()
	if len(state.Stream) != 1 || state.Stream[0] != "BirdHackBot interactive session." {
		t.Fatalf("Stream = %#v", state.Stream)
	}
}

func TestUIStateAppliesShellCommandAndReply(t *testing.T) {
	state := NewUIState()
	state.AddUserInput("Who are you?")
	state.AddShellCommand("/help", "commands:\n- /status", nil)
	state.AddAssistantReply("I am BirdHackBot.")
	joined := strings.Join(state.Stream, "\n")
	for _, want := range []string{"User: Who are you?", "Command: /help", "System: commands:\n- /status", "Assistant: I am BirdHackBot."} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stream missing %q in:\n%s", want, joined)
		}
	}
}

func TestUIStateAppliesRunOutcome(t *testing.T) {
	state := NewUIState()
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "list files"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "ls -la",
			OutputSummary: "stdout: birdhackbot",
		},
		RunningSummary: "Status: done.",
	}
	state.AddRunOutcome(packet, "Run complete. What I did: listed files.", errors.New("blocked"))
	joined := strings.Join(state.Stream, "\n")
	for _, want := range []string{"Assistant: Run complete. What I did: listed files.", "Command: ls -la", "State: Status: done.", "Result: stdout: birdhackbot", "Error: blocked"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stream missing %q in:\n%s", want, joined)
		}
	}
	if state.Packet.TaskRuntime.State != "done" {
		t.Fatalf("packet state = %q", state.Packet.TaskRuntime.State)
	}
}

func TestUIStateSkipsDuplicateResultTraceForCompletedConciseRun(t *testing.T) {
	state := NewUIState()
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "list files"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "done"},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "ls -la",
			OutputSummary: "stdout: total 10\ndrwxr-xr-x .",
			Assessment:    "success",
			LogRefs:       []string{"/tmp/cmd.log"},
		},
		RunningSummary: "Status: done.",
	}
	state.AddRunOutcome(packet, "Run complete.\nWhat I did:\n- listed files", nil)
	joined := strings.Join(state.Stream, "\n")
	if strings.Contains(joined, "State: Status: done.") {
		t.Fatalf("stream should not duplicate state trace on concise completed run:\n%s", joined)
	}
	if strings.Contains(joined, "Result: stdout: total 10") {
		t.Fatalf("stream should not duplicate raw result trace on concise completed run:\n%s", joined)
	}
	if strings.Contains(joined, "Command: ls -la") {
		t.Fatalf("stream should not show command trace on concise completed run:\n%s", joined)
	}
}

func TestUIStateEnvironmentFallbacks(t *testing.T) {
	state := NewUIState()
	state.SetEnvironment("qwen3.5-27b", "/work", "approved_session")
	view := state.View()
	if view.Model != "qwen3.5-27b" {
		t.Fatalf("Model = %q", view.Model)
	}
	if view.WorkingDir != "/work" {
		t.Fatalf("WorkingDir = %q", view.WorkingDir)
	}
	if view.ApprovalState != "approved_session" {
		t.Fatalf("ApprovalState = %q", view.ApprovalState)
	}
}
