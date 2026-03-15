package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func renderWorkerBoxes(workers []orchestrator.WorkerStatus, debug map[string]tuiWorkerDebug, width int) []string {
	if len(workers) == 0 {
		return renderBox("Worker", []string{"no workers running"}, width)
	}
	sortedWorkers := sortWorkersForDebug(workers, debug)
	expanded := make([]orchestrator.WorkerStatus, 0, len(sortedWorkers))
	collapsed := make([]orchestrator.WorkerStatus, 0, len(sortedWorkers))
	for _, worker := range sortedWorkers {
		if workerShouldCollapse(worker) {
			collapsed = append(collapsed, worker)
			continue
		}
		expanded = append(expanded, worker)
	}
	out := make([]string, 0, len(sortedWorkers)*8)
	if len(expanded) == 0 {
		out = append(out, renderBox("Worker", []string{"no active workers"}, width)...)
	}
	for idx, worker := range expanded {
		task := strings.TrimSpace(worker.CurrentTask)
		if task == "" {
			task = "-"
		}
		body := []string{
			fmt.Sprintf("state=%s seq=%d task=%s", strings.TrimSpace(worker.State), worker.LastSeq, task),
		}
		if snapshot, ok := debug[worker.WorkerID]; ok {
			body = append(body, fmt.Sprintf("last=%s type=%s", snapshot.TS.Format("15:04:05"), snapshot.EventType))
			if snapshot.Step > 0 || snapshot.ToolCalls > 0 {
				body = append(body, fmt.Sprintf("step=%d tool_calls=%d", snapshot.Step, snapshot.ToolCalls))
			}
			if msg := strings.TrimSpace(snapshot.Message); msg != "" {
				body = append(body, "msg: "+msg)
			}
			if cmd := strings.TrimSpace(snapshot.Command); cmd != "" {
				args := strings.Join(snapshot.Args, " ")
				if args != "" {
					body = append(body, "cmd: "+cmd+" "+args)
				} else {
					body = append(body, "cmd: "+cmd)
				}
			}
			if reason := strings.TrimSpace(snapshot.Reason); reason != "" {
				body = append(body, "reason: "+reason)
			}
			if errText := strings.TrimSpace(snapshot.Error); errText != "" {
				body = append(body, "error: "+errText)
			}
			if path := strings.TrimSpace(snapshot.LogPath); path != "" {
				body = append(body, "log: "+path)
			}
		} else {
			body = append(body, "last: no worker output yet")
		}
		title := "Worker " + worker.WorkerID
		out = append(out, renderBox(title, body, width)...)
		if idx < len(expanded)-1 {
			out = append(out, fitLine("", width))
		}
	}
	if len(collapsed) > 0 {
		if len(out) > 0 {
			out = append(out, fitLine("", width))
		}
		body := []string{
			fmt.Sprintf("%d stopped/completed worker(s) collapsed by default", len(collapsed)),
		}
		for i, worker := range collapsed {
			if i >= 6 {
				body = append(body, fmt.Sprintf("... %d more", len(collapsed)-i))
				break
			}
			task := strings.TrimSpace(worker.CurrentTask)
			if task == "" {
				task = "-"
			}
			line := fmt.Sprintf("%s state=%s task=%s", worker.WorkerID, strings.TrimSpace(worker.State), task)
			if snapshot, ok := debug[worker.WorkerID]; ok {
				if reason := strings.TrimSpace(snapshot.Reason); reason != "" {
					line += " reason=" + reason
				}
			}
			body = append(body, line)
		}
		out = append(out, renderBox("Collapsed Workers", body, width)...)
	}
	return out
}

func workerShouldCollapse(worker orchestrator.WorkerStatus) bool {
	switch strings.ToLower(strings.TrimSpace(worker.State)) {
	case "stopped", "completed":
		return true
	default:
		return false
	}
}

func sortWorkersForDebug(workers []orchestrator.WorkerStatus, debug map[string]tuiWorkerDebug) []orchestrator.WorkerStatus {
	out := append([]orchestrator.WorkerStatus{}, workers...)
	stateRank := func(state string) int {
		switch strings.ToLower(strings.TrimSpace(state)) {
		case "active":
			return 0
		case "seen":
			return 1
		case "stopped":
			return 2
		default:
			return 3
		}
	}
	lastTS := func(worker orchestrator.WorkerStatus) time.Time {
		ts := worker.LastEvent
		if snapshot, ok := debug[worker.WorkerID]; ok && snapshot.TS.After(ts) {
			ts = snapshot.TS
		}
		return ts
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftRank := stateRank(out[i].State)
		rightRank := stateRank(out[j].State)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftTS := lastTS(out[i])
		rightTS := lastTS(out[j])
		if !leftTS.Equal(rightTS) {
			return leftTS.After(rightTS)
		}
		return out[i].WorkerID < out[j].WorkerID
	})
	return out
}

func renderBox(title string, body []string, width int) []string {
	inner := width - 5
	if inner < 12 {
		inner = 12
	}
	clip := func(text string) string {
		runes := []rune(strings.TrimSpace(text))
		if len(runes) > inner {
			if inner > 3 {
				return string(runes[:inner-3]) + "..."
			}
			return string(runes[:inner])
		}
		return string(runes)
	}
	pad := func(text string) string {
		runes := []rune(text)
		if len(runes) < inner {
			return text + strings.Repeat(" ", inner-len(runes))
		}
		return text
	}

	top := "+" + strings.Repeat("-", inner+2) + "+"
	lines := []string{
		fitLine(top, width),
		fitLine("| "+pad(clip(title))+" |", width),
		fitLine("+"+strings.Repeat("-", inner+2)+"+", width),
	}
	if len(body) == 0 {
		lines = append(lines, fitLine("| "+pad(" ")+" |", width))
	} else {
		for _, raw := range body {
			text := clip(raw)
			lines = append(lines, fitLine("| "+pad(text)+" |", width))
		}
	}
	lines = append(lines, fitLine(top, width))
	return lines
}

func failureHint(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case orchestrator.WorkerFailureAssistTimeout:
		return "LLM call timed out before task budget completed."
	case orchestrator.WorkerFailureAssistUnavailable:
		return "LLM was unreachable or returned an invalid response."
	case orchestrator.WorkerFailureAssistParseFailure:
		return "LLM responded but output could not be parsed into a valid suggestion."
	case orchestrator.WorkerFailureCommandTimeout:
		return "Tool command timed out."
	case "execution_timeout":
		return "Task runtime budget exceeded."
	default:
		return ""
	}
}
