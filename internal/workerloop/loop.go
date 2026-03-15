package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

type Inspector interface {
	Capture(step int, stage string, packet ctxpacket.WorkerPacket) error
}

type Loop struct {
	LLM       llmclient.Client
	Executor  execx.Executor
	Approver  approval.Approver
	Inspector Inspector
}

type Outcome struct {
	Summary string
	Packet  ctxpacket.WorkerPacket
}

func (l Loop) Run(ctx context.Context, packet ctxpacket.WorkerPacket, maxSteps int) (Outcome, error) {
	if maxSteps <= 0 {
		maxSteps = 1
	}
	current := packet
	for step := 1; step <= maxSteps; step++ {
		if err := captureIfConfigured(l.Inspector, step, "pre-llm", current); err != nil {
			return Outcome{Packet: current}, fmt.Errorf("capture pre-llm context: %w", err)
		}
		respText, err := l.LLM.Chat(ctx, []llmclient.Message{
			{Role: "system", Content: current.BehaviorFrame.PromptText()},
			{Role: "user", Content: buildUserPrompt(current)},
		})
		if err != nil {
			return Outcome{Packet: current}, fmt.Errorf("llm chat: %w", err)
		}

		resp, err := ParseResponse(respText)
		if err != nil {
			return Outcome{Packet: current}, fmt.Errorf("parse llm response: %w; raw=%q", err, respText)
		}

		switch resp.Type {
		case "step_complete":
			current.RunningSummary = strings.TrimSpace(resp.Summary)
			if err := captureIfConfigured(l.Inspector, step, "step-complete", current); err != nil {
				return Outcome{Packet: current}, fmt.Errorf("capture completion context: %w", err)
			}
			return Outcome{Summary: resp.Summary, Packet: current}, nil
		case "ask_user":
			if err := captureIfConfigured(l.Inspector, step, "ask-user", current); err != nil {
				return Outcome{Packet: current}, fmt.Errorf("capture ask-user context: %w", err)
			}
			return Outcome{Packet: current}, fmt.Errorf("worker requires user input: %s", resp.Question)
		case "action":
			if l.Approver == nil {
				return Outcome{Packet: current}, fmt.Errorf("execution requires an approver")
			}
			decision, err := l.Approver.Approve(ctx, approval.Request{
				Command:  resp.Command,
				UseShell: resp.UseShell,
			})
			if err != nil {
				return Outcome{Packet: current}, fmt.Errorf("approval failed: %w", err)
			}
			current.OperatorState.ApprovalState = string(decision)
			if decision == approval.DecisionDeny {
				return Outcome{Packet: current}, fmt.Errorf("execution denied by user")
			}
			result, execErr := l.Executor.Run(ctx, execx.Action{
				Command:  resp.Command,
				Cwd:      ".",
				UseShell: resp.UseShell,
			})
			nextResult := ctxpacket.ExecutionResult{
				Action:        result.Action,
				ExitStatus:    fmt.Sprintf("%d", result.ExitStatus),
				OutputSummary: compactOutputSummary(combineSummaries(result.StdoutSummary, result.StderrSummary)),
				LogRefs:       []string{result.LogPath},
				ArtifactRefs:  result.ArtifactRefs,
				FailureClass:  result.FailureClass,
			}
			current.RelevantRecentResults = updateRelevantRecentResults(current.RelevantRecentResults, current.LatestExecutionResult)
			current.LatestExecutionResult = nextResult
			current.RunningSummary = buildRunningSummary(current.CurrentStep.Objective, current.LatestExecutionResult, current.RelevantRecentResults)
			if err := captureIfConfigured(l.Inspector, step, "post-action", current); err != nil {
				return Outcome{Packet: current}, fmt.Errorf("capture post-action context: %w", err)
			}
			if execErr != nil {
				// Still feed the failure result into the next turn.
			}
		}
	}
	return Outcome{Packet: current}, fmt.Errorf("step did not complete within %d steps", maxSteps)
}

func buildUserPrompt(packet ctxpacket.WorkerPacket) string {
	payload := map[string]any{
		"instructions": []string{
			"Respond with JSON only.",
			"Choose exactly one of: action, step_complete, ask_user.",
			"If choosing action, use: {\"type\":\"action\",\"command\":\"...\",\"use_shell\":true|false}.",
			"If choosing step_complete, use: {\"type\":\"step_complete\",\"summary\":\"...\"}.",
			"If choosing ask_user, use: {\"type\":\"ask_user\",\"question\":\"...\"}.",
			"Do not invent hidden steps.",
			"Do not use alternative key names or nested envelopes.",
		},
		"context_packet": packet.Render(),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func combineSummaries(stdout, stderr string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(stdout) != "" && stdout != "(none)" {
		parts = append(parts, "stdout: "+stdout)
	}
	if strings.TrimSpace(stderr) != "" && stderr != "(none)" {
		parts = append(parts, "stderr: "+stderr)
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " | ")
}

func compactOutputSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "(none)" {
		return "(none)"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 2 {
		lines = lines[:2]
	}
	s = strings.Join(lines, "\n")
	const limit = 220
	if len(s) > limit {
		return strings.TrimSpace(s[:limit]) + "..."
	}
	return s
}

func updateRelevantRecentResults(current []ctxpacket.ExecutionResult, previousLatest ctxpacket.ExecutionResult) []ctxpacket.ExecutionResult {
	if strings.TrimSpace(previousLatest.Action) == "" {
		return current
	}
	next := append([]ctxpacket.ExecutionResult{previousLatest}, current...)
	if len(next) > 3 {
		next = next[:3]
	}
	return next
}

func buildRunningSummary(objective string, latest ctxpacket.ExecutionResult, recent []ctxpacket.ExecutionResult) string {
	objective = strings.TrimSpace(objective)
	status := "in progress"
	if strings.TrimSpace(latest.FailureClass) != "" || strings.TrimSpace(latest.ExitStatus) == "-1" {
		status = "encountered a failure"
	}
	if strings.TrimSpace(latest.ExitStatus) != "" && strings.TrimSpace(latest.ExitStatus) != "(none)" && strings.TrimSpace(latest.ExitStatus) != "0" {
		status = "encountered a failure"
	}

	parts := []string{
		fmt.Sprintf("Objective: %s.", blankOrFallback(objective, "make progress on the current goal")),
		fmt.Sprintf("Latest result: %q exited with %s.", latest.Action, blankOrFallback(strings.TrimSpace(latest.ExitStatus), "(none)")),
	}
	if strings.TrimSpace(latest.OutputSummary) != "" && strings.TrimSpace(latest.OutputSummary) != "(none)" {
		parts = append(parts, fmt.Sprintf("Key output: %s.", singleLine(latest.OutputSummary)))
	}
	if len(recent) > 0 {
		parts = append(parts, fmt.Sprintf("Recent prior results retained: %d.", len(recent)))
	}
	parts = append(parts, fmt.Sprintf("Current status: %s.", status))
	return strings.Join(parts, " ")
}

func singleLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " | ")
	return s
}

func blankOrFallback(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func captureIfConfigured(inspector Inspector, step int, stage string, packet ctxpacket.WorkerPacket) error {
	if inspector == nil {
		return nil
	}
	return inspector.Capture(step, stage, packet)
}
