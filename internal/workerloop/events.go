package workerloop

import (
	"time"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

type ProgressEventKind string

const (
	EventTaskStarted          ProgressEventKind = "task_started"
	EventPlanStarted          ProgressEventKind = "plan_started"
	EventPlanFinished         ProgressEventKind = "plan_finished"
	EventActionProposed       ProgressEventKind = "action_proposed"
	EventActionReviewStarted  ProgressEventKind = "action_review_started"
	EventActionReviewFinished ProgressEventKind = "action_review_finished"
	EventExecutionStarted     ProgressEventKind = "execution_started"
	EventExecutionFinished    ProgressEventKind = "execution_finished"
	EventPostExecEvalStarted  ProgressEventKind = "post_exec_eval_started"
	EventPostExecEvalFinished ProgressEventKind = "post_exec_eval_finished"
	EventTaskCompleted        ProgressEventKind = "task_completed"
	EventTaskBlocked          ProgressEventKind = "task_blocked"
	EventTaskFailed           ProgressEventKind = "task_failed"
)

// ProgressEvent is a live runtime transition for UI and orchestration surfaces.
// It is observational only; the authoritative current-task state remains in the packet.
type ProgressEvent struct {
	Kind         ProgressEventKind
	At           time.Time
	StepIndex    int
	Message      string
	ActiveStep   string
	Action       string
	ExitStatus   string
	Assessment   string
	FailureClass string
}

type ProgressSink interface {
	EmitProgress(event ProgressEvent, packet ctxpacket.WorkerPacket) error
}

func newProgressEvent(kind ProgressEventKind, step int, message string) ProgressEvent {
	return ProgressEvent{
		Kind:      kind,
		At:        time.Now().UTC(),
		StepIndex: step,
		Message:   message,
	}
}

func emitProgressIfConfigured(sink ProgressSink, event ProgressEvent, packet ctxpacket.WorkerPacket) error {
	if sink == nil {
		return nil
	}
	return sink.EmitProgress(event, packet)
}
