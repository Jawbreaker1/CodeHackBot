package orchestrator

import "strings"

const (
	runProjectionStateUnknown   = "unknown"
	runProjectionStateRunning   = "running"
	runProjectionStateStopped   = "stopped"
	runProjectionStateCompleted = "completed"

	taskProjectionStateQueued  = "queued"
	taskProjectionStateRunning = "running"
	taskProjectionStateDone    = "done"

	workerProjectionStateSeen    = "seen"
	workerProjectionStateActive  = "active"
	workerProjectionStateStopped = "stopped"
)

func applyEventToRunTaskWorkerProjection(
	hasRunStarted *bool,
	runState *string,
	workerActive map[string]bool,
	taskState map[string]string,
	event EventEnvelope,
) {
	taskID := strings.TrimSpace(event.TaskID)

	if EventMutatesDomain(event.Type, MutationDomainRunProjection) {
		switch event.Type {
		case EventTypeRunStarted:
			*hasRunStarted = true
		case EventTypeRunStopped:
			*runState = runProjectionStateStopped
		case EventTypeRunCompleted:
			*runState = runProjectionStateCompleted
		}
	}

	if EventMutatesDomain(event.Type, MutationDomainTaskProjection) && taskID != "" {
		switch event.Type {
		case EventTypeTaskLeased:
			taskState[taskID] = taskProjectionStateQueued
		case EventTypeTaskStarted, EventTypeTaskProgress, EventTypeTaskArtifact, EventTypeTaskFinding:
			taskState[taskID] = taskProjectionStateRunning
		case EventTypeTaskCompleted, EventTypeTaskFailed:
			taskState[taskID] = taskProjectionStateDone
		}
	}

	if EventMutatesDomain(event.Type, MutationDomainWorkerProjection) {
		switch event.Type {
		case EventTypeWorkerStarted:
			workerActive[event.WorkerID] = true
		case EventTypeWorkerStopped:
			workerActive[event.WorkerID] = false
		}
	}
}

func applyEventToWorkerStatusMap(byWorker map[string]WorkerStatus, event EventEnvelope) {
	if strings.TrimSpace(event.WorkerID) == "" {
		return
	}
	ws := byWorker[event.WorkerID]
	if ws.WorkerID == "" {
		ws.WorkerID = event.WorkerID
		ws.State = workerProjectionStateSeen
	}
	ws.LastSeq = maxI64(ws.LastSeq, event.Seq)
	if event.TS.After(ws.LastEvent) {
		ws.LastEvent = event.TS
	}

	if EventMutatesDomain(event.Type, MutationDomainWorkerProjection) {
		switch event.Type {
		case EventTypeWorkerStarted:
			ws.State = workerProjectionStateActive
		case EventTypeWorkerHeartbeat:
			if ws.State != workerProjectionStateStopped {
				ws.State = workerProjectionStateActive
			}
		case EventTypeWorkerStopped:
			ws.State = workerProjectionStateStopped
		case EventTypeTaskStarted, EventTypeTaskProgress, EventTypeTaskArtifact, EventTypeTaskFinding:
			if ws.State != workerProjectionStateStopped {
				ws.State = workerProjectionStateActive
			}
			ws.CurrentTask = event.TaskID
		case EventTypeTaskCompleted, EventTypeTaskFailed:
			if ws.CurrentTask == event.TaskID {
				ws.CurrentTask = ""
			}
		}
	}
	byWorker[event.WorkerID] = ws
}
