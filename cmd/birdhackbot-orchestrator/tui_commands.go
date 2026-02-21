package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func parseTUICommand(raw string) (tuiCommand, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return tuiCommand{name: "refresh"}, nil
	}
	parts := strings.Fields(trimmed)
	cmd := strings.ToLower(parts[0])
	switch cmd {
	case "q", "quit", "exit":
		return tuiCommand{name: "quit"}, nil
	case "help", "h":
		return tuiCommand{name: "help"}, nil
	case "plan":
		return tuiCommand{name: "plan"}, nil
	case "tasks":
		return tuiCommand{name: "tasks"}, nil
	case "refresh", "r":
		return tuiCommand{name: "refresh"}, nil
	case "events":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: events <count>")
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil || n <= 0 {
			return tuiCommand{}, fmt.Errorf("events count must be a positive integer")
		}
		return tuiCommand{name: "events", eventLimit: n}, nil
	case "log":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: log <up|down|top|bottom> [count]")
		}
		action := strings.ToLower(strings.TrimSpace(parts[1]))
		switch action {
		case "up", "down":
			count := 1
			if len(parts) >= 3 {
				n, err := strconv.Atoi(parts[2])
				if err != nil || n <= 0 {
					return tuiCommand{}, fmt.Errorf("log count must be a positive integer")
				}
				count = n
			}
			return tuiCommand{name: "log", scope: action, logCount: count}, nil
		case "top", "bottom":
			return tuiCommand{name: "log", scope: action}, nil
		default:
			return tuiCommand{}, fmt.Errorf("usage: log <up|down|top|bottom> [count]")
		}
	case "approve":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: approve <approval-id> [once|task|session] [reason]")
		}
		scope := "task"
		reasonStart := 2
		if len(parts) > 2 {
			switch strings.ToLower(parts[2]) {
			case "once", "task", "session":
				scope = strings.ToLower(parts[2])
				reasonStart = 3
			}
		}
		reason := "approved via tui"
		if len(parts) >= reasonStart+1 {
			reason = strings.Join(parts[reasonStart:], " ")
		}
		return tuiCommand{name: "approve", approval: parts[1], scope: scope, reason: reason}, nil
	case "deny":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: deny <approval-id> [reason]")
		}
		reason := "denied via tui"
		if len(parts) > 2 {
			reason = strings.Join(parts[2:], " ")
		}
		return tuiCommand{name: "deny", approval: parts[1], reason: reason}, nil
	case "stop":
		return tuiCommand{name: "stop"}, nil
	case "execute":
		return tuiCommand{name: "execute"}, nil
	case "regenerate":
		return tuiCommand{name: "regenerate"}, nil
	case "discard":
		return tuiCommand{name: "discard"}, nil
	case "task":
		if len(parts) < 3 {
			return tuiCommand{}, fmt.Errorf("usage: task <add|remove|set|move> ...")
		}
		sub := strings.ToLower(strings.TrimSpace(parts[1]))
		switch sub {
		case "add":
			text := strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]+" "+parts[1]))
			if text == "" {
				return tuiCommand{}, fmt.Errorf("usage: task add <title/goal>")
			}
			return tuiCommand{name: "task_add", reason: text}, nil
		case "remove":
			if len(parts) < 3 {
				return tuiCommand{}, fmt.Errorf("usage: task remove <task-id>")
			}
			return tuiCommand{name: "task_remove", taskID: strings.TrimSpace(parts[2])}, nil
		case "set":
			if len(parts) < 5 {
				return tuiCommand{}, fmt.Errorf("usage: task set <task-id> <field> <value>")
			}
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]+" "+parts[1]+" "+parts[2]+" "+parts[3]))
			if value == "" {
				return tuiCommand{}, fmt.Errorf("usage: task set <task-id> <field> <value>")
			}
			return tuiCommand{name: "task_set", taskID: strings.TrimSpace(parts[2]), field: strings.ToLower(strings.TrimSpace(parts[3])), value: value}, nil
		case "move":
			if len(parts) < 4 {
				return tuiCommand{}, fmt.Errorf("usage: task move <task-id> <position>")
			}
			position, err := strconv.Atoi(strings.TrimSpace(parts[3]))
			if err != nil || position <= 0 {
				return tuiCommand{}, fmt.Errorf("usage: task move <task-id> <position>")
			}
			return tuiCommand{name: "task_move", taskID: strings.TrimSpace(parts[2]), position: position}, nil
		default:
			return tuiCommand{}, fmt.Errorf("usage: task <add|remove|set|move> ...")
		}
	case "instruct":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: instruct <instruction>")
		}
		return tuiCommand{name: "instruct", reason: strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]))}, nil
	case "ask":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: ask <question>")
		}
		return tuiCommand{name: "ask", reason: strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]))}, nil
	default:
		return tuiCommand{name: "ask", reason: trimmed}, nil
	}
}

func looksLikeQuestion(input string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(input))
	if trimmed == "" {
		return false
	}
	if strings.HasSuffix(trimmed, "?") {
		return true
	}
	questionPrefixes := []string{
		"what", "why", "how", "when", "where", "who", "which",
		"can", "could", "would", "should", "is", "are", "do", "does", "did",
	}
	for _, prefix := range questionPrefixes {
		if strings.HasPrefix(trimmed, prefix+" ") {
			return true
		}
	}
	return false
}

func executeTUICommand(manager *orchestrator.Manager, runID string, eventLimit *int, cmd tuiCommand, commandLogScroll *int) (bool, string) {
	switch cmd.name {
	case "quit":
		return true, "exiting tui"
	case "help":
		return false, "commands: help, plan, tasks, ask <question>, instruct <text>, execute, regenerate, discard, task add/remove/set/move, refresh, events <n>, log <up|down|top|bottom> [n], approve <id> [scope] [reason], deny <id> [reason], stop, quit"
	case "plan":
		plan, err := manager.LoadRunPlan(runID)
		if err != nil {
			return false, "plan failed: " + err.Error()
		}
		goal := strings.TrimSpace(plan.Metadata.Goal)
		if goal == "" {
			goal = "(no goal metadata)"
		}
		return false, fmt.Sprintf("plan: goal=%q tasks=%d success=%d stop=%d", goal, len(plan.Tasks), len(plan.SuccessCriteria), len(plan.StopCriteria))
	case "tasks":
		plan, err := manager.LoadRunPlan(runID)
		if err != nil {
			return false, "tasks failed loading plan: " + err.Error()
		}
		leases, err := manager.ReadLeases(runID)
		if err != nil {
			return false, "tasks failed reading leases: " + err.Error()
		}
		rows := buildTaskRows(plan, leases)
		counts := map[string]int{}
		for _, row := range rows {
			counts[row.State]++
		}
		return false, fmt.Sprintf("tasks: total=%d running=%d awaiting=%d queued=%d failed=%d blocked=%d completed=%d", len(rows), counts["running"], counts["awaiting_approval"], counts["queued"], counts["failed"], counts["blocked"], counts["completed"])
	case "refresh":
		return false, "refreshed"
	case "events":
		if eventLimit != nil {
			*eventLimit = cmd.eventLimit
		}
		return false, fmt.Sprintf("event window set to %d", cmd.eventLimit)
	case "log":
		if commandLogScroll == nil {
			return false, "command log scrolling unavailable"
		}
		switch cmd.scope {
		case "up":
			step := cmd.logCount
			if step <= 0 {
				step = 1
			}
			*commandLogScroll += step
			return false, fmt.Sprintf("command log scrolled up %d", step)
		case "down":
			step := cmd.logCount
			if step <= 0 {
				step = 1
			}
			*commandLogScroll -= step
			if *commandLogScroll < 0 {
				*commandLogScroll = 0
			}
			return false, fmt.Sprintf("command log scrolled down %d", step)
		case "top":
			*commandLogScroll = 1 << 20
			return false, "command log moved to oldest lines"
		case "bottom":
			*commandLogScroll = 0
			return false, "command log moved to latest lines"
		default:
			return false, "usage: log <up|down|top|bottom> [count]"
		}
	case "approve":
		if err := manager.SubmitApprovalDecision(runID, cmd.approval, true, cmd.scope, "tui", cmd.reason, 0); err != nil {
			return false, "approve failed: " + err.Error()
		}
		return false, fmt.Sprintf("approved %s (%s)", cmd.approval, cmd.scope)
	case "deny":
		if err := manager.SubmitApprovalDecision(runID, cmd.approval, false, "", "tui", cmd.reason, 0); err != nil {
			return false, "deny failed: " + err.Error()
		}
		return false, fmt.Sprintf("denied %s", cmd.approval)
	case "stop":
		if err := manager.Stop(runID); err != nil {
			return false, "stop failed: " + err.Error()
		}
		return false, "stop requested"
	case "execute":
		msg, done, err := executeTUIPlanningCommand(manager, runID, "execute")
		if err != nil {
			return false, "execute failed: " + err.Error()
		}
		return done, msg
	case "regenerate":
		msg, done, err := executeTUIPlanningCommand(manager, runID, "regenerate")
		if err != nil {
			return false, "regenerate failed: " + err.Error()
		}
		return done, msg
	case "discard":
		msg, done, err := executeTUIPlanningCommand(manager, runID, "discard")
		if err != nil {
			return false, "discard failed: " + err.Error()
		}
		return done, msg
	case "task_add":
		msg, err := planningTaskAdd(manager, runID, cmd.reason)
		if err != nil {
			return false, "task add failed: " + err.Error()
		}
		return false, msg
	case "task_remove":
		msg, err := planningTaskRemove(manager, runID, cmd.taskID)
		if err != nil {
			return false, "task remove failed: " + err.Error()
		}
		return false, msg
	case "task_set":
		msg, err := planningTaskSetField(manager, runID, cmd.taskID, cmd.field, cmd.value)
		if err != nil {
			return false, "task set failed: " + err.Error()
		}
		return false, msg
	case "task_move":
		msg, err := planningTaskMove(manager, runID, cmd.taskID, cmd.position)
		if err != nil {
			return false, "task move failed: " + err.Error()
		}
		return false, msg
	case "ask":
		return false, handleTUIAsk(manager, runID, cmd.reason)
	case "instruct":
		instruction := strings.TrimSpace(cmd.reason)
		if instruction == "" {
			return false, "instruct failed: instruction is required"
		}
		if plan, err := manager.LoadRunPlan(runID); err == nil {
			if isPlanningPhase(runPhaseFromPlan(plan)) {
				msg, planErr := planningInstructionToDraft(manager, runID, instruction)
				if planErr != nil {
					return false, "instruct failed: " + planErr.Error()
				}
				return false, msg
			}
		}
		if err := queueTUIInstruction(manager, runID, instruction); err != nil {
			return false, "instruct failed: " + err.Error()
		}
		return false, "instruction queued: " + instruction
	default:
		return false, ""
	}
}

func queueTUIInstruction(manager *orchestrator.Manager, runID, instruction string) error {
	return manager.EmitEvent(runID, "operator", "", orchestrator.EventTypeOperatorInstruction, map[string]any{
		"instruction": strings.TrimSpace(instruction),
		"source":      "tui",
	})
}

func handleTUIAsk(manager *orchestrator.Manager, runID, input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "ask failed: question is required"
	}

	if reply, err := askTUIWithLLM(manager, runID, trimmed); err == nil {
		lines := []string{fmt.Sprintf("assistant: %s", strings.TrimSpace(reply.Reply))}
		queuedInstruction := ""
		if reply.QueueInstruction {
			instruction := strings.TrimSpace(reply.Instruction)
			if instruction == "" {
				instruction = trimmed
			}
			if err := queueTUIInstruction(manager, runID, instruction); err != nil {
				lines = append(lines, "instruction failed: "+err.Error())
			} else {
				lines = append(lines, "instruction queued: "+instruction)
				queuedInstruction = instruction
			}
		}
		recordPlanningConversation(manager, runID, trimmed, strings.TrimSpace(reply.Reply), queuedInstruction)
		return strings.Join(lines, "\n")
	}

	fallback := answerTUIQuestion(manager, runID, trimmed)
	if looksLikeQuestion(trimmed) {
		return fallback
	}
	if plan, err := manager.LoadRunPlan(runID); err == nil {
		if isPlanningPhase(runPhaseFromPlan(plan)) {
			msg, planErr := planningInstructionToDraft(manager, runID, trimmed)
			if planErr != nil {
				return fallback + "\nplanning update failed: " + planErr.Error()
			}
			return fallback + "\n" + msg
		}
	}
	if err := queueTUIInstruction(manager, runID, trimmed); err != nil {
		return fallback + "\ninstruction failed: " + err.Error()
	}
	recordPlanningConversation(manager, runID, trimmed, fallback, trimmed)
	return fallback + "\ninstruction queued: " + trimmed
}

func recordPlanningConversation(manager *orchestrator.Manager, runID, operatorInput, assistantReply, queuedInstruction string) {
	plan, err := manager.LoadRunPlan(runID)
	if err != nil || !isPlanningPhase(runPhaseFromPlan(plan)) {
		return
	}
	payload := map[string]any{
		"action":          "ask",
		"operator_input":  strings.TrimSpace(operatorInput),
		"assistant_reply": strings.TrimSpace(assistantReply),
		"task_count":      len(plan.Tasks),
		"phase":           runPhaseFromPlan(plan),
	}
	if queued := strings.TrimSpace(queuedInstruction); queued != "" {
		payload["queued_instruction"] = queued
	}
	_ = appendPlanningTranscript(manager, runID, payload)
}

func askTUIWithLLM(manager *orchestrator.Manager, runID, operatorInput string) (tuiAssistantDecision, error) {
	cfg, client, model, timeout, err := resolveTUIAssistantClient()
	if err != nil {
		return tuiAssistantDecision{}, err
	}
	temperature, maxTokens := cfg.ResolveLLMRoleOptions("tui_assistant", 0.1, 600)
	contextText, err := buildTUIAssistantContext(manager, runID)
	if err != nil {
		return tuiAssistantDecision{}, err
	}

	chatCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, err := client.Chat(chatCtx, llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			{
				Role: "system",
				Content: "You are BirdHackBot Orchestrator's operator assistant for authorized internal lab security testing. " +
					"You must answer the operator based on run context and decide whether to queue a new operator instruction. " +
					"Return strict JSON only with keys: reply (string), queue_instruction (boolean), instruction (string). " +
					"Set queue_instruction=true only when the operator asks to change execution or run a new action. " +
					"Keep reply concise and specific.",
			},
			{
				Role:    "user",
				Content: "RUN CONTEXT:\n" + contextText + "\n\nOPERATOR INPUT:\n" + operatorInput,
			},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return tuiAssistantDecision{}, err
	}
	parsed, err := parseTUIAssistantDecision(resp.Content)
	if err != nil {
		reply := sanitizeLogLine(resp.Content)
		if reply == "" {
			return tuiAssistantDecision{}, err
		}
		return tuiAssistantDecision{
			Reply:            reply,
			QueueInstruction: !looksLikeQuestion(operatorInput),
		}, nil
	}
	parsed.Reply = strings.TrimSpace(parsed.Reply)
	if parsed.Reply == "" {
		parsed.Reply = "I did not generate a usable answer."
	}
	parsed.Instruction = strings.TrimSpace(parsed.Instruction)
	return parsed, nil
}

func resolveTUIAssistantClient() (config.Config, llm.Client, string, time.Duration, error) {
	cfg := config.Config{}
	cfg.LLM.TimeoutSeconds = 45

	loadPath := strings.TrimSpace(detectWorkerConfigPath())
	if loadPath != "" {
		loaded, _, err := config.Load(loadPath, "", "")
		if err != nil {
			return config.Config{}, nil, "", 0, fmt.Errorf("load config from %s: %w", loadPath, err)
		}
		cfg = loaded
		if cfg.LLM.TimeoutSeconds <= 0 {
			cfg.LLM.TimeoutSeconds = 45
		}
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMBaseURLEnv)); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMModelEnv)); v != "" {
		cfg.LLM.Model = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMAPIKeyEnv)); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMTimeoutEnv)); v != "" {
		if parsed, convErr := strconv.Atoi(v); convErr == nil && parsed > 0 {
			cfg.LLM.TimeoutSeconds = parsed
		}
	}

	model := strings.TrimSpace(cfg.LLM.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.Model)
	}
	if strings.TrimSpace(cfg.LLM.BaseURL) == "" || model == "" {
		return config.Config{}, nil, "", 0, fmt.Errorf("llm configuration unavailable")
	}
	timeout := time.Duration(cfg.LLM.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	if timeout > 45*time.Second {
		timeout = 45 * time.Second
	}
	return cfg, llm.NewLMStudioClient(cfg), model, timeout, nil
}

func buildTUIAssistantContext(manager *orchestrator.Manager, runID string) (string, error) {
	snap, err := collectTUISnapshot(manager, runID, tuiAskEventWindow)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	goal := strings.TrimSpace(snap.plan.Metadata.Goal)
	if goal == "" {
		goal = "(no goal metadata)"
	}
	phase := orchestrator.NormalizeRunPhase(snap.plan.Metadata.RunPhase)
	if phase == "" {
		phase = "-"
	}
	fmt.Fprintf(&b, "run_id=%s state=%s phase=%s active_workers=%d queued=%d running=%d\n", runID, snap.status.State, phase, snap.status.ActiveWorkers, snap.status.QueuedTasks, snap.status.RunningTasks)
	fmt.Fprintf(&b, "goal=%s\n", goal)
	fmt.Fprintf(&b, "tasks_total=%d completed=%d running=%d queued=%d failed=%d awaiting_approval=%d\n",
		len(snap.tasks),
		countTaskState(snap.tasks, "completed"),
		countTaskState(snap.tasks, "running")+countTaskState(snap.tasks, "leased"),
		countTaskState(snap.tasks, "queued"),
		countTaskState(snap.tasks, "failed"),
		countTaskState(snap.tasks, "awaiting_approval"),
	)
	if path := latestReportPath(snap.events); path != "" {
		fmt.Fprintf(&b, "latest_report_path=%s\n", path)
	}
	if len(snap.approvals) > 0 {
		fmt.Fprintln(&b, "pending_approvals:")
		for i, approval := range snap.approvals {
			if i >= 6 {
				fmt.Fprintf(&b, "- ... %d more\n", len(snap.approvals)-i)
				break
			}
			fmt.Fprintf(&b, "- %s task=%s tier=%s reason=%s\n", approval.ApprovalID, approval.TaskID, approval.RiskTier, approval.Reason)
		}
	}
	if len(snap.tasks) > 0 {
		fmt.Fprintln(&b, "task_board:")
		for i, task := range snap.tasks {
			if i >= 14 {
				fmt.Fprintf(&b, "- ... %d more\n", len(snap.tasks)-i)
				break
			}
			progress := formatProgressSummary(snap.progress[task.TaskID])
			if progress != "" {
				fmt.Fprintf(&b, "- %s [%s] worker=%s strategy=%s progress=%s\n", task.TaskID, task.State, emptyDash(task.WorkerID), emptyDash(task.Strategy), progress)
			} else {
				fmt.Fprintf(&b, "- %s [%s] worker=%s strategy=%s\n", task.TaskID, task.State, emptyDash(task.WorkerID), emptyDash(task.Strategy))
			}
		}
	}
	if len(snap.workers) > 0 {
		fmt.Fprintln(&b, "workers:")
		ordered := sortWorkersForDebug(snap.workers, snap.workerDebug)
		for i, worker := range ordered {
			if i >= 10 {
				fmt.Fprintf(&b, "- ... %d more\n", len(ordered)-i)
				break
			}
			fmt.Fprintf(&b, "- %s state=%s task=%s seq=%d\n", worker.WorkerID, worker.State, emptyDash(worker.CurrentTask), worker.LastSeq)
			if dbg, ok := snap.workerDebug[worker.WorkerID]; ok {
				if msg := strings.TrimSpace(dbg.Message); msg != "" {
					fmt.Fprintf(&b, "  last=%s\n", msg)
				}
				if cmd := strings.TrimSpace(dbg.Command); cmd != "" {
					fmt.Fprintf(&b, "  cmd=%s %s\n", cmd, strings.TrimSpace(strings.Join(dbg.Args, " ")))
				}
				if reason := strings.TrimSpace(dbg.Reason); reason != "" {
					fmt.Fprintf(&b, "  reason=%s\n", reason)
				}
				if errText := strings.TrimSpace(dbg.Error); errText != "" {
					fmt.Fprintf(&b, "  error=%s\n", errText)
				}
			}
		}
	}
	if snap.lastFailure != nil {
		fmt.Fprintf(&b, "last_failure task=%s worker=%s reason=%s error=%s log=%s\n",
			emptyDash(snap.lastFailure.TaskID),
			emptyDash(snap.lastFailure.Worker),
			emptyDash(snap.lastFailure.Reason),
			emptyDash(snap.lastFailure.Error),
			emptyDash(snap.lastFailure.LogPath),
		)
	}
	if len(snap.events) > 0 {
		fmt.Fprintln(&b, "recent_events:")
		for i, event := range snap.events {
			if i >= 16 {
				fmt.Fprintf(&b, "- ... %d more\n", len(snap.events)-i)
				break
			}
			fmt.Fprintf(&b, "- %s %s worker=%s task=%s\n", event.TS.Format("15:04:05"), event.Type, emptyDash(event.WorkerID), emptyDash(event.TaskID))
		}
	}
	text := b.String()
	if len(text) > tuiAskLLMMaxContext {
		return text[:tuiAskLLMMaxContext], nil
	}
	return text, nil
}

func parseTUIAssistantDecision(raw string) (tuiAssistantDecision, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return tuiAssistantDecision{}, fmt.Errorf("assistant response empty")
	}
	decoded := tuiAssistantDecision{}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded, nil
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return tuiAssistantDecision{}, fmt.Errorf("assistant response is not json")
	}
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &decoded); err != nil {
		return tuiAssistantDecision{}, fmt.Errorf("parse assistant json: %w", err)
	}
	return decoded, nil
}

func latestReportPath(events []orchestrator.EventEnvelope) string {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != orchestrator.EventTypeTaskArtifact {
			continue
		}
		payload := decodeEventPayload(event.Payload)
		path := strings.TrimSpace(stringFromAny(payload["path"]))
		if path == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(path), ".md") || strings.Contains(strings.ToLower(path), "report") {
			return path
		}
	}
	return ""
}

func emptyDash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	return trimmed
}

func answerTUIQuestion(manager *orchestrator.Manager, runID, question string) string {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return "ask failed: question is required"
	}
	lower := strings.ToLower(trimmed)

	status, statusErr := manager.Status(runID)
	if statusErr != nil {
		return "ask failed reading status: " + statusErr.Error()
	}
	plan, planErr := manager.LoadRunPlan(runID)
	leases, leaseErr := manager.ReadLeases(runID)
	if planErr != nil || leaseErr != nil {
		return fmt.Sprintf("status: state=%s workers=%d queued=%d running=%d", status.State, status.ActiveWorkers, status.QueuedTasks, status.RunningTasks)
	}

	rows := buildTaskRows(plan, leases)
	stateByTask := map[string]string{}
	for _, row := range rows {
		stateByTask[row.TaskID] = row.State
	}
	active := activeTaskRows(rows)

	if strings.Contains(lower, "plan") || strings.Contains(lower, "steps") {
		stepParts := make([]string, 0, len(plan.Tasks))
		for i, task := range plan.Tasks {
			state := stateByTask[task.TaskID]
			if state == "" {
				state = "queued"
			}
			stepParts = append(stepParts, fmt.Sprintf("%d)%s[%s]", i+1, task.Title, state))
			if len(stepParts) >= 10 && len(plan.Tasks) > 10 {
				stepParts = append(stepParts, fmt.Sprintf("... +%d more", len(plan.Tasks)-10))
				break
			}
		}
		return fmt.Sprintf("plan (%d steps): %s", len(plan.Tasks), strings.Join(stepParts, " | "))
	}

	if strings.Contains(lower, "current") || strings.Contains(lower, "working on") || strings.Contains(lower, "status") {
		if len(active) == 0 {
			return fmt.Sprintf("state=%s. no active step right now. queued=%d failed=%d completed=%d", status.State, countTaskState(rows, "queued"), countTaskState(rows, "failed"), countTaskState(rows, "completed"))
		}
		activeParts := make([]string, 0, len(active))
		for _, row := range active {
			activeParts = append(activeParts, fmt.Sprintf("%s[%s]", row.TaskID, row.State))
		}
		return fmt.Sprintf("state=%s. current active: %s", status.State, strings.Join(activeParts, " | "))
	}

	return "I can answer run-state questions (plan, steps, current status). Use `instruct <text>` to change the run."
}

func countTaskState(rows []tuiTaskRow, state string) int {
	target := normalizeDisplayState(state)
	count := 0
	for _, row := range rows {
		if normalizeDisplayState(row.State) == target {
			count++
		}
	}
	return count
}
