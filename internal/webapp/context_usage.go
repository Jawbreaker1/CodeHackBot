package webapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// Older sessions did not persist coordinator usage. Their last planning
// request is enough to show a truthful historical measurement after restart.
func lastCoordinatorRequestContext(root string, limit int) contextWindowView {
	entries, err := os.ReadDir(root)
	if err != nil {
		return contextWindowView{LimitBytes: limit}
	}
	var latest string
	var modified time.Time
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "coordinator-") || !strings.HasSuffix(name, "-request.json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			continue
		}
		if latest == "" || info.ModTime().After(modified) {
			latest, modified = name, info.ModTime()
		}
	}
	if latest == "" {
		return contextWindowView{LimitBytes: limit}
	}
	data, err := os.ReadFile(filepath.Join(root, latest))
	if err != nil {
		return contextWindowView{LimitBytes: limit}
	}
	var messages []llmclient.Message
	if json.Unmarshal(data, &messages) != nil {
		return contextWindowView{LimitBytes: limit}
	}
	used := 0
	for _, message := range messages {
		used += len(message.Content)
	}
	view := contextWindowView{UsedBytes: used, LimitBytes: limit}
	usageData, err := os.ReadFile(filepath.Join(root, strings.TrimSuffix(latest, "-request.json")+"-usage.json"))
	if err == nil {
		var usage struct {
			RequestInputBytes   int `json:"request_input_bytes"`
			ProviderInputTokens int `json:"provider_input_tokens"`
		}
		if json.Unmarshal(usageData, &usage) == nil && usage.RequestInputBytes == used && usage.ProviderInputTokens > 0 {
			view.ProviderInputTokens = usage.ProviderInputTokens
		}
	}
	return view
}

func measuredContext(used, limit int, role, id, status string, active bool) contextWindowView {
	if limit < 0 {
		limit = 0
	}
	remaining, percent := limit-used, 0
	if remaining < 0 {
		remaining = 0
	}
	if limit > 0 {
		percent = min(100, used*100/limit)
	}
	view := contextWindowView{UsedBytes: used, LimitBytes: limit, RemainingBytes: remaining, Percent: percent, AgentID: id, Role: role, Status: status, Active: active}
	if role == "worker" {
		view.WorkerID = id
	}
	return view
}

func terminalWorkerPhase(phase string) bool {
	switch phase {
	case "done", "task_completed", "failed", "task_failed", "blocked", "task_blocked", "aborted", "interrupted":
		return true
	default:
		return false
	}
}

func agentContextWindows(state assessment.State, coordinator contextWindowView, workers []workerView, status string, started, chatBusy bool, events []eventRecord) (contextWindowView, []contextWindowView) {
	limit := coordinator.LimitBytes
	if limit == 0 {
		limit = state.MaxInputBytes
	}
	coordinatorActive := chatBusy
	if started && len(events) > 0 {
		last := events[len(events)-1].Event.Kind
		coordinatorActive = coordinatorActive || last == "planning" || last == "plan_revision_requested"
	}
	coordinatorStatus := "idle"
	switch {
	case coordinatorActive:
		coordinatorStatus = "thinking"
	case status == "interrupted":
		coordinatorStatus = "interrupted"
	case status == "paused":
		coordinatorStatus = "paused"
	case started:
		coordinatorStatus = "waiting"
	case status == "completed" || status == "completed_with_gaps":
		coordinatorStatus = "finished"
	}
	primary := measuredContext(coordinator.UsedBytes, limit, "coordinator", "coordinator", coordinatorStatus, coordinatorActive)
	primary.ProviderInputTokens = coordinator.ProviderInputTokens
	all := []contextWindowView{primary}
	for _, worker := range workers {
		// The coordinator resumes from saved results, not from a finished or
		// interrupted worker packet. Show only contexts that can receive another
		// decision in the current run; historical packets remain in the debugger.
		if !started || terminalWorkerPhase(worker.Phase) {
			continue
		}
		workerLimit := worker.ContextLimitBytes
		if workerLimit == 0 {
			workerLimit = state.MaxInputBytes
		}
		active := worker.Phase != "approval_required" && worker.Phase != "user_question" && worker.Phase != "waiting_user"
		entry := measuredContext(worker.ContextUsedBytes, workerLimit, "worker", worker.ID, worker.Phase, active)
		entry.ProviderInputTokens = worker.ProviderInputTokens
		all = append(all, entry)
	}
	return primary, all
}
