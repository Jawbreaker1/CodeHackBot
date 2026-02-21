package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func collectTUISnapshot(manager *orchestrator.Manager, runID string, eventLimit int) (tuiSnapshot, error) {
	var snap tuiSnapshot
	status, err := manager.Status(runID)
	if err != nil {
		return snap, err
	}
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		return snap, err
	}
	workers, err := manager.Workers(runID)
	if err != nil {
		return snap, err
	}
	leases, err := manager.ReadLeases(runID)
	if err != nil {
		return snap, err
	}
	approvals, err := manager.PendingApprovals(runID)
	if err != nil {
		return snap, err
	}
	allEvents, err := manager.Events(runID, 0)
	if err != nil {
		return snap, err
	}
	events := allEvents
	if eventLimit > 0 && len(events) > eventLimit {
		events = append([]orchestrator.EventEnvelope{}, events[len(events)-eventLimit:]...)
	}
	snap.status = status
	snap.plan = plan
	snap.workers = hydrateWorkerTasks(workers, leases)
	snap.workerDebug = latestWorkerDebugByWorker(allEvents)
	snap.approvals = approvals
	snap.tasks = buildTaskRows(plan, leases)
	snap.progress = latestTaskProgressByTask(allEvents)
	snap.events = events
	snap.lastFailure = latestFailureFromEvents(allEvents)
	snap.updatedAt = time.Now().UTC()
	return snap, nil
}

func hydrateWorkerTasks(workers []orchestrator.WorkerStatus, leases []orchestrator.TaskLease) []orchestrator.WorkerStatus {
	if len(workers) == 0 {
		return workers
	}
	leaseTask := map[string]string{}
	for _, lease := range leases {
		if strings.TrimSpace(lease.WorkerID) == "" {
			continue
		}
		if lease.Status != orchestrator.LeaseStatusLeased && lease.Status != orchestrator.LeaseStatusRunning && lease.Status != orchestrator.LeaseStatusAwaitingApproval {
			continue
		}
		leaseTask[lease.WorkerID] = lease.TaskID
	}
	out := make([]orchestrator.WorkerStatus, 0, len(workers))
	for _, worker := range workers {
		if strings.TrimSpace(worker.CurrentTask) == "" {
			if taskID := strings.TrimSpace(leaseTask[worker.WorkerID]); taskID != "" {
				worker.CurrentTask = taskID
			}
		}
		out = append(out, worker)
	}
	return out
}

func buildTaskRows(plan orchestrator.RunPlan, leases []orchestrator.TaskLease) []tuiTaskRow {
	leaseByTask := map[string]orchestrator.TaskLease{}
	for _, lease := range leases {
		leaseByTask[lease.TaskID] = lease
	}
	rows := make([]tuiTaskRow, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		state := "queued"
		workerID := ""
		if lease, ok := leaseByTask[task.TaskID]; ok {
			state = lease.Status
			workerID = strings.TrimSpace(lease.WorkerID)
		}
		row := tuiTaskRow{
			TaskID:    task.TaskID,
			Title:     task.Title,
			Strategy:  task.Strategy,
			State:     normalizeDisplayState(state),
			WorkerID:  workerID,
			Priority:  task.Priority,
			IsDynamic: strings.HasPrefix(strings.ToLower(strings.TrimSpace(task.Strategy)), "adaptive_replan_"),
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left := taskStateOrder(rows[i].State)
		right := taskStateOrder(rows[j].State)
		if left != right {
			return left < right
		}
		if rows[i].Priority != rows[j].Priority {
			return rows[i].Priority > rows[j].Priority
		}
		return rows[i].TaskID < rows[j].TaskID
	})
	return rows
}

func normalizeDisplayState(state string) string {
	trimmed := strings.TrimSpace(strings.ToLower(state))
	if trimmed == "" {
		return "queued"
	}
	return trimmed
}

func stepMarker(state string) string {
	switch normalizeDisplayState(state) {
	case "completed":
		return "[x]"
	case "running", "leased", "awaiting_approval":
		return "[>]"
	case "failed":
		return "[!]"
	case "blocked":
		return "[-]"
	case "canceled":
		return "[~]"
	default:
		return "[ ]"
	}
}

func taskStateOrder(state string) int {
	switch normalizeDisplayState(state) {
	case "running":
		return 1
	case "awaiting_approval":
		return 2
	case "leased":
		return 3
	case "queued":
		return 4
	case "failed":
		return 5
	case "blocked":
		return 6
	case "completed":
		return 7
	case "canceled":
		return 8
	default:
		return 9
	}
}

func latestFailureFromEvents(events []orchestrator.EventEnvelope) *tuiFailure {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		payload := decodeEventPayload(event.Payload)
		switch event.Type {
		case orchestrator.EventTypeTaskFailed:
			reason := strings.TrimSpace(stringFromAny(payload["reason"]))
			if reason == "" {
				reason = "task_failed"
			}
			details := make([]string, 0)
			missingArtifacts := stringSliceFromAny(payload["missing_artifacts"])
			if len(missingArtifacts) > 0 {
				details = append(details, "missing artifacts: "+strings.Join(missingArtifacts, ", "))
			}
			missingFindings := stringSliceFromAny(payload["missing_findings"])
			if len(missingFindings) > 0 {
				details = append(details, "missing findings: "+strings.Join(missingFindings, ", "))
			}
			verifiedArtifacts := stringSliceFromAny(payload["verified_artifacts"])
			if len(verifiedArtifacts) > 0 {
				details = append(details, fmt.Sprintf("verified artifacts: %d", len(verifiedArtifacts)))
			}
			if verificationStatus := strings.TrimSpace(stringFromAny(payload["verification_status"])); verificationStatus != "" {
				details = append(details, "contract: "+verificationStatus)
			}
			return &tuiFailure{
				TS:      event.TS,
				TaskID:  event.TaskID,
				Worker:  event.WorkerID,
				Reason:  reason,
				Error:   strings.TrimSpace(stringFromAny(payload["error"])),
				LogPath: strings.TrimSpace(stringFromAny(payload["log_path"])),
				Details: details,
			}
		case orchestrator.EventTypeWorkerStopped:
			status := strings.ToLower(strings.TrimSpace(stringFromAny(payload["status"])))
			if status != "failed" {
				continue
			}
			return &tuiFailure{
				TS:      event.TS,
				TaskID:  event.TaskID,
				Worker:  event.WorkerID,
				Reason:  "worker_stopped_failed",
				Error:   strings.TrimSpace(stringFromAny(payload["error"])),
				LogPath: strings.TrimSpace(stringFromAny(payload["log_path"])),
			}
		}
	}
	return nil
}

func latestWorkerDebugByWorker(events []orchestrator.EventEnvelope) map[string]tuiWorkerDebug {
	out := map[string]tuiWorkerDebug{}
	for _, event := range events {
		workerID := normalizeDebugWorkerID(event.WorkerID)
		if workerID == "" {
			continue
		}
		current, exists := out[workerID]
		if exists && event.TS.Before(current.TS) {
			continue
		}
		payload := decodeEventPayload(event.Payload)
		next := current
		next.WorkerID = workerID
		next.TaskID = strings.TrimSpace(event.TaskID)
		next.EventType = strings.TrimSpace(event.Type)
		next.TS = event.TS
		switch event.Type {
		case orchestrator.EventTypeTaskStarted:
			next.Message = strings.TrimSpace(stringFromAny(payload["goal"]))
		case orchestrator.EventTypeTaskProgress:
			next.Message = strings.TrimSpace(stringFromAny(payload["message"]))
			next.Step = intFromAny(payload["step"])
			if next.Step == 0 {
				next.Step = intFromAny(payload["steps"])
			}
			next.ToolCalls = intFromAny(payload["tool_calls"])
			if next.ToolCalls == 0 {
				next.ToolCalls = intFromAny(payload["tool_call"])
			}
			cmd := strings.TrimSpace(stringFromAny(payload["command"]))
			if cmd != "" {
				next.Command = cmd
			}
		case orchestrator.EventTypeTaskArtifact:
			next.Command = strings.TrimSpace(stringFromAny(payload["command"]))
			next.Args = append([]string{}, stringSliceFromAny(payload["args"])...)
			next.LogPath = strings.TrimSpace(stringFromAny(payload["path"]))
			next.Step = intFromAny(payload["step"])
			if next.ToolCalls == 0 {
				next.ToolCalls = intFromAny(payload["tool_call"])
			}
		case orchestrator.EventTypeTaskFailed:
			next.Reason = strings.TrimSpace(stringFromAny(payload["reason"]))
			next.Error = strings.TrimSpace(stringFromAny(payload["error"]))
			next.LogPath = strings.TrimSpace(stringFromAny(payload["log_path"]))
		case orchestrator.EventTypeWorkerStopped:
			next.Reason = strings.TrimSpace(stringFromAny(payload["status"]))
			next.Error = strings.TrimSpace(stringFromAny(payload["error"]))
			if logPath := strings.TrimSpace(stringFromAny(payload["log_path"])); logPath != "" {
				next.LogPath = logPath
			}
		}
		out[workerID] = next
	}
	return out
}

func normalizeDebugWorkerID(workerID string) string {
	trimmed := strings.TrimSpace(workerID)
	if trimmed == "" {
		return ""
	}
	return strings.TrimPrefix(trimmed, "signal-")
}

func latestTaskProgressByTask(events []orchestrator.EventEnvelope) map[string]tuiTaskProgress {
	progress := map[string]tuiTaskProgress{}
	for _, event := range events {
		taskID := strings.TrimSpace(event.TaskID)
		if taskID == "" {
			continue
		}
		switch event.Type {
		case orchestrator.EventTypeTaskStarted:
			payload := decodeEventPayload(event.Payload)
			msg := strings.TrimSpace(stringFromAny(payload["goal"]))
			if msg == "" {
				msg = "task started"
			}
			current, ok := progress[taskID]
			if !ok || event.TS.After(current.TS) {
				progress[taskID] = tuiTaskProgress{
					TS:      event.TS,
					Message: msg,
					Mode:    "start",
				}
			}
		case orchestrator.EventTypeTaskProgress:
			payload := decodeEventPayload(event.Payload)
			next := tuiTaskProgress{
				TS:        event.TS,
				Step:      intFromAny(payload["step"]),
				ToolCalls: intFromAny(payload["tool_calls"]),
				Message:   strings.TrimSpace(stringFromAny(payload["message"])),
				Mode:      strings.TrimSpace(stringFromAny(payload["mode"])),
			}
			if next.Step == 0 {
				next.Step = intFromAny(payload["steps"])
			}
			if next.ToolCalls == 0 {
				next.ToolCalls = intFromAny(payload["tool_call"])
			}
			if next.Message == "" {
				next.Message = strings.TrimSpace(stringFromAny(payload["type"]))
			}
			current, ok := progress[taskID]
			if !ok || event.TS.After(current.TS) || (event.TS.Equal(current.TS) && next.Step >= current.Step) {
				progress[taskID] = next
			}
		}
	}
	return progress
}

func activeTaskRows(rows []tuiTaskRow) []tuiTaskRow {
	active := make([]tuiTaskRow, 0, len(rows))
	for _, row := range rows {
		switch normalizeDisplayState(row.State) {
		case "running", "leased", "awaiting_approval":
			active = append(active, row)
		}
	}
	return active
}

func formatProgressSummary(progress tuiTaskProgress) string {
	parts := make([]string, 0, 3)
	if progress.Step > 0 {
		parts = append(parts, fmt.Sprintf("step %d", progress.Step))
	}
	if progress.Message != "" {
		parts = append(parts, progress.Message)
	}
	if progress.ToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("tools:%d", progress.ToolCalls))
	}
	return strings.Join(parts, " | ")
}

func decodeEventPayload(raw json.RawMessage) map[string]any {
	payload := map[string]any{}
	if len(raw) == 0 {
		return payload
	}
	_ = json.Unmarshal(raw, &payload)
	return payload
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return ""
	}
}

func intFromAny(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
		if parsed, err := value.Float64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return 0
}

func stringSliceFromAny(v any) []string {
	switch values := v.(type) {
	case []string:
		out := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(stringFromAny(value))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return nil
	}
}
