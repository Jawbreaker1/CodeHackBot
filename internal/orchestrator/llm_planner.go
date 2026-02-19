package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

const llmPlannerSystemPrompt = "You are the BirdHackBot Orchestrator planner. Return JSON only, no markdown. " +
	"Create an executable task graph for authorized internal-lab security testing. " +
	"Do not use destructive actions by default. " +
	"Schema: {\"rationale\":\"...\",\"tasks\":[{\"task_id\":\"...\",\"title\":\"...\",\"goal\":\"...\",\"targets\":[\"...\"],\"depends_on\":[\"...\"],\"priority\":1,\"strategy\":\"...\",\"risk_level\":\"recon_readonly|active_probe|exploit_controlled|priv_esc|disruptive\",\"done_when\":[\"...\"],\"fail_when\":[\"...\"],\"expected_artifacts\":[\"...\"],\"action\":{\"type\":\"assist|command|shell\",\"prompt\":\"...\",\"command\":\"...\",\"args\":[\"...\"],\"working_dir\":\"...\",\"timeout_seconds\":120},\"budget\":{\"max_steps\":12,\"max_tool_calls\":20,\"max_runtime_seconds\":600}}]}. " +
	"Rules: every task must be concrete, bounded, and in-scope; dependencies must form a DAG; " +
	"if tasks can be split into independent subtasks, do so and maximize safe parallel execution up to max_parallelism; " +
	"for broad CIDR targets, prefer discovery-first fan-out (discovery -> host subsets -> validation/summarize) instead of one monolithic scan; " +
	"only keep work serialized when there is a clear dependency/safety reason, and state that reason in rationale."

type llmPlannerResponse struct {
	Rationale string           `json:"rationale"`
	Tasks     []llmPlannerTask `json:"tasks"`
}

type llmPlannerTask struct {
	TaskID            string               `json:"task_id"`
	Title             string               `json:"title"`
	Goal              string               `json:"goal"`
	Targets           []string             `json:"targets"`
	DependsOn         []string             `json:"depends_on"`
	Priority          int                  `json:"priority"`
	Strategy          string               `json:"strategy"`
	RiskLevel         string               `json:"risk_level"`
	DoneWhen          []string             `json:"done_when"`
	FailWhen          []string             `json:"fail_when"`
	ExpectedArtifacts []string             `json:"expected_artifacts"`
	Action            llmPlannerTaskAction `json:"action"`
	Budget            llmPlannerTaskBudget `json:"budget"`
}

type llmPlannerTaskAction struct {
	Type           string   `json:"type"`
	Prompt         string   `json:"prompt"`
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	WorkingDir     string   `json:"working_dir"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type llmPlannerTaskBudget struct {
	MaxSteps          int `json:"max_steps"`
	MaxToolCalls      int `json:"max_tool_calls"`
	MaxRuntimeSeconds int `json:"max_runtime_seconds"`
}

func SynthesizeTaskGraphWithLLM(
	ctx context.Context,
	client llm.Client,
	model string,
	goal string,
	scope Scope,
	constraints []string,
	hypotheses []Hypothesis,
	maxParallelism int,
) ([]TaskSpec, string, error) {
	if client == nil {
		return nil, "", fmt.Errorf("llm planner client is required")
	}
	if strings.TrimSpace(model) == "" {
		return nil, "", fmt.Errorf("llm planner model is required")
	}
	if strings.TrimSpace(goal) == "" {
		return nil, "", fmt.Errorf("goal is required")
	}

	payload := map[string]any{
		"goal":            strings.TrimSpace(goal),
		"scope":           scope,
		"constraints":     constraints,
		"max_parallelism": maxParallelism,
		"hypotheses":      hypotheses,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("marshal planner payload: %w", err)
	}

	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       strings.TrimSpace(model),
		Temperature: 0.1,
		Messages: []llm.Message{
			{Role: "system", Content: llmPlannerSystemPrompt},
			{Role: "user", Content: string(data)},
		},
	})
	if err != nil {
		return nil, "", fmt.Errorf("llm planner request: %w", err)
	}

	raw := extractPlannerJSON(resp.Content)
	decoded := llmPlannerResponse{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, "", fmt.Errorf("parse llm planner response: %w", err)
	}
	if len(decoded.Tasks) == 0 {
		return nil, "", fmt.Errorf("llm planner returned no tasks")
	}

	tasks := make([]TaskSpec, 0, len(decoded.Tasks))
	for i, task := range decoded.Tasks {
		spec, err := toTaskSpec(task, i)
		if err != nil {
			return nil, "", err
		}
		tasks = append(tasks, spec)
	}
	return tasks, strings.TrimSpace(decoded.Rationale), nil
}

func toTaskSpec(task llmPlannerTask, index int) (TaskSpec, error) {
	taskID := strings.TrimSpace(task.TaskID)
	if taskID == "" {
		taskID = fmt.Sprintf("task-llm-%02d", index+1)
	}
	actionType := strings.ToLower(strings.TrimSpace(task.Action.Type))
	if actionType == "" {
		actionType = "assist"
	}
	if actionType == "shell" {
		actionType = "command"
	}
	riskLevel := strings.TrimSpace(task.RiskLevel)
	if riskLevel == "" {
		riskLevel = string(RiskReconReadonly)
	}
	budget := TaskBudget{
		MaxSteps:     task.Budget.MaxSteps,
		MaxToolCalls: task.Budget.MaxToolCalls,
		MaxRuntime:   time.Duration(task.Budget.MaxRuntimeSeconds) * time.Second,
	}
	if budget.MaxSteps <= 0 {
		budget.MaxSteps = 12
	}
	if budget.MaxToolCalls <= 0 {
		budget.MaxToolCalls = 20
	}
	if budget.MaxRuntime <= 0 {
		budget.MaxRuntime = 8 * time.Minute
	}
	budget = normalizeLLMTaskBudget(budget, riskLevel)
	spec := TaskSpec{
		TaskID:            taskID,
		Title:             strings.TrimSpace(task.Title),
		Goal:              strings.TrimSpace(task.Goal),
		Targets:           compactStringSlice(task.Targets),
		DependsOn:         compactStringSlice(task.DependsOn),
		Priority:          task.Priority,
		Strategy:          strings.TrimSpace(task.Strategy),
		Action:            TaskAction{Type: actionType, Prompt: strings.TrimSpace(task.Action.Prompt), Command: strings.TrimSpace(task.Action.Command), Args: compactStringSlice(task.Action.Args), WorkingDir: strings.TrimSpace(task.Action.WorkingDir), TimeoutSeconds: task.Action.TimeoutSeconds},
		DoneWhen:          compactStringSlice(task.DoneWhen),
		FailWhen:          compactStringSlice(task.FailWhen),
		ExpectedArtifacts: compactStringSlice(task.ExpectedArtifacts),
		RiskLevel:         riskLevel,
		Budget:            budget,
	}
	if spec.Title == "" {
		spec.Title = spec.TaskID
	}
	if spec.Goal == "" {
		spec.Goal = spec.Title
	}
	if spec.Priority <= 0 {
		spec.Priority = 50
	}
	if spec.Action.Type == "assist" && strings.TrimSpace(spec.Action.Prompt) == "" {
		spec.Action.Prompt = spec.Goal
	}
	if err := ValidateTaskSpec(spec); err != nil {
		return TaskSpec{}, fmt.Errorf("llm planner task %s invalid: %w", spec.TaskID, err)
	}
	return spec, nil
}

func normalizeLLMTaskBudget(budget TaskBudget, riskLevel string) TaskBudget {
	risk := strings.ToLower(strings.TrimSpace(riskLevel))
	minSteps := 6
	minToolCalls := 8
	minRuntime := 4 * time.Minute
	if risk == string(RiskActiveProbe) {
		minSteps = 8
		minToolCalls = 12
		minRuntime = 6 * time.Minute
	}
	if budget.MaxSteps < minSteps {
		budget.MaxSteps = minSteps
	}
	if budget.MaxToolCalls < minToolCalls {
		budget.MaxToolCalls = minToolCalls
	}
	if budget.MaxRuntime < minRuntime {
		budget.MaxRuntime = minRuntime
	}
	return budget
}

func compactStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func extractPlannerJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimLeft(trimmed, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
		trimmed = strings.TrimSpace(trimmed)
	}
	if strings.HasSuffix(trimmed, "```") {
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	if obj, ok := extractPlannerJSONObject(trimmed); ok {
		return obj
	}
	return trimmed
}

func extractPlannerJSONObject(text string) (string, bool) {
	start := -1
	depth := 0
	inString := false
	escape := false
	for i, r := range text {
		if start == -1 {
			if r == '{' {
				start = i
				depth = 1
			}
			continue
		}
		if inString {
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : i+1]), true
			}
		}
	}
	return "", false
}

func ParsePlannerMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", "auto":
		return "auto", nil
	case "static", "llm":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid planner mode %q", raw)
	}
}

func ParseRuntimeSeconds(raw any, fallback int) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed
		}
	}
	return fallback
}
