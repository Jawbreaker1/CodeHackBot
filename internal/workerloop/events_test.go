package workerloop

import (
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

type recordingProgressSink struct {
	events  []ProgressEvent
	packets []ctxpacket.WorkerPacket
}

func (r *recordingProgressSink) EmitProgress(event ProgressEvent, packet ctxpacket.WorkerPacket) error {
	r.events = append(r.events, event)
	r.packets = append(r.packets, packet)
	return nil
}

func TestEmitProgressIfConfiguredNoopWhenNil(t *testing.T) {
	if err := emitProgressIfConfigured(nil, newProgressEvent(EventTaskStarted, 1, "start"), ctxpacket.WorkerPacket{}); err != nil {
		t.Fatalf("emitProgressIfConfigured(nil) error = %v", err)
	}
}

func TestEmitProgressIfConfiguredForwardsEvent(t *testing.T) {
	sink := &recordingProgressSink{}
	event := newProgressEvent(EventExecutionFinished, 2, "execution finished")
	event.Action = "pwd"
	event.ExitStatus = "0"
	packet := ctxpacket.WorkerPacket{RunningSummary: "done"}
	if err := emitProgressIfConfigured(sink, event, packet); err != nil {
		t.Fatalf("emitProgressIfConfigured() error = %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	if sink.events[0].Kind != EventExecutionFinished {
		t.Fatalf("kind = %q", sink.events[0].Kind)
	}
	if sink.events[0].Action != "pwd" {
		t.Fatalf("action = %q", sink.events[0].Action)
	}
	if sink.events[0].At.IsZero() {
		t.Fatal("expected timestamp")
	}
	if sink.packets[0].RunningSummary != "done" {
		t.Fatalf("packet running summary = %q", sink.packets[0].RunningSummary)
	}
}
