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
	"only keep work serialized when there is a clear dependency/safety reason, and state that reason in rationale; " +
	"ground tasks in the operator goal: preserve goal-specific entities (for example router/gateway/firewall/webapp) and include at least one explicit task focused on that entity; " +
	"if the goal asks for vulnerabilities, include a dedicated vulnerability-mapping step tied to discovered versions/configuration."

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

type LLMPlannerOptions struct {
	Temperature *float32
	MaxTokens   *int
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
	return SynthesizeTaskGraphWithLLMWithOptions(
		ctx,
		client,
		model,
		goal,
		scope,
		constraints,
		hypotheses,
		maxParallelism,
		LLMPlannerOptions{},
	)
}

func SynthesizeTaskGraphWithLLMWithOptions(
	ctx context.Context,
	client llm.Client,
	model string,
	goal string,
	scope Scope,
	constraints []string,
	hypotheses []Hypothesis,
	maxParallelism int,
	options LLMPlannerOptions,
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

	temperature := float32(0.1)
	if options.Temperature != nil {
		temperature = *options.Temperature
	}
	maxTokens := 0
	if options.MaxTokens != nil && *options.MaxTokens > 0 {
		maxTokens = *options.MaxTokens
	}
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       strings.TrimSpace(model),
		Temperature: temperature,
		MaxTokens:   maxTokens,
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
		spec, err := toTaskSpec(task, i, scope)
		if err != nil {
			return nil, "", err
		}
		tasks = append(tasks, spec)
	}
	tasks = applyGoalAnchoring(tasks, goal)
	return tasks, strings.TrimSpace(decoded.Rationale), nil
}

func toTaskSpec(task llmPlannerTask, index int, scope Scope) (TaskSpec, error) {
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
		Targets:           normalizeLLMTaskTargets(task.Targets, scope),
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

func normalizeLLMTaskTargets(rawTargets []string, scope Scope) []string {
	scopedDefaults := scopeTargets(scope)
	targets := compactStringSlice(rawTargets)
	if len(targets) == 0 {
		return scopedDefaults
	}
	policy := NewScopePolicy(scope)
	allowed := make([]string, 0, len(targets))
	for _, target := range targets {
		candidate := TaskSpec{Targets: []string{target}}
		if err := policy.ValidateTaskTargets(candidate); err != nil {
			continue
		}
		allowed = append(allowed, target)
	}
	allowed = compactStringSlice(allowed)
	if len(allowed) == 0 {
		return scopedDefaults
	}
	return allowed
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

func applyGoalAnchoring(tasks []TaskSpec, goal string) []TaskSpec {
	if len(tasks) == 0 {
		return tasks
	}
	trimmedGoal := strings.TrimSpace(goal)
	if trimmedGoal == "" {
		return tasks
	}
	goalLower := strings.ToLower(trimmedGoal)

	if containsAny(goalLower, "router", "gateway", "default gateway", "default gw", "firewall", "access point", "ap ") &&
		!tasksMentionAny(tasks, "router", "gateway", "default gateway", "firewall", "access point") {
		idx := chooseGoalAnchorTask(tasks, "recon", "scan", "discover", "host", "network", "enumerate")
		attachGoalFocus(&tasks[idx], "router focus", "Primary goal focus: identify the in-scope router/gateway and prioritize router-specific validation and findings.")
	}

	if containsAny(goalLower, "vulnerability", "vulnerabilities", "cve", "exploit", "misconfig", "weakness") &&
		!tasksMentionAny(tasks, "vulnerability", "cve", "exploit", "misconfig", "weakness", "searchsploit", "metasploit") {
		idx := chooseGoalAnchorTask(tasks, "version", "service", "validate", "analyze", "probe", "summary")
		attachGoalFocus(&tasks[idx], "vuln mapping", "Primary goal focus: map discovered versions/configuration to known vulnerabilities (CVE/searchsploit/metasploit modules) and capture reproducible evidence.")
	}

	for i := range tasks {
		if strings.ToLower(strings.TrimSpace(tasks[i].Action.Type)) != "assist" {
			continue
		}
		prompt := strings.TrimSpace(tasks[i].Action.Prompt)
		goalLine := "Operator goal: " + trimmedGoal
		if strings.Contains(strings.ToLower(prompt), strings.ToLower(trimmedGoal)) || strings.Contains(strings.ToLower(prompt), "operator goal:") {
			continue
		}
		if prompt == "" {
			tasks[i].Action.Prompt = goalLine
			continue
		}
		tasks[i].Action.Prompt = prompt + "\n" + goalLine
	}
	return tasks
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func tasksMentionAny(tasks []TaskSpec, needles ...string) bool {
	for _, task := range tasks {
		combined := strings.ToLower(strings.TrimSpace(task.Title + " " + task.Goal + " " + task.Action.Prompt + " " + strings.Join(task.ExpectedArtifacts, " ")))
		for _, needle := range needles {
			if strings.Contains(combined, needle) {
				return true
			}
		}
	}
	return false
}

func chooseGoalAnchorTask(tasks []TaskSpec, preferredTerms ...string) int {
	bestIdx := 0
	bestScore := -1
	for i, task := range tasks {
		score := 0
		if strings.ToLower(strings.TrimSpace(task.Action.Type)) == "assist" {
			score += 3
		}
		if strings.Contains(strings.ToLower(task.RiskLevel), string(RiskReconReadonly)) || strings.Contains(strings.ToLower(task.RiskLevel), string(RiskActiveProbe)) {
			score += 2
		}
		text := strings.ToLower(strings.TrimSpace(task.Title + " " + task.Goal + " " + task.Strategy))
		for _, term := range preferredTerms {
			if strings.Contains(text, term) {
				score += 1
			}
		}
		if task.Priority > 0 {
			score += task.Priority / 50
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return bestIdx
}

func attachGoalFocus(task *TaskSpec, titleSuffix, note string) {
	if task == nil {
		return
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return
	}
	titleSuffix = strings.TrimSpace(titleSuffix)
	if titleSuffix != "" && !strings.Contains(strings.ToLower(task.Title), strings.ToLower(titleSuffix)) {
		task.Title = strings.TrimSpace(task.Title + " (" + titleSuffix + ")")
	}
	if !strings.Contains(strings.ToLower(task.Goal), strings.ToLower(note)) {
		task.Goal = strings.TrimSpace(task.Goal + " " + note)
	}
	if strings.ToLower(strings.TrimSpace(task.Action.Type)) == "assist" && !strings.Contains(strings.ToLower(task.Action.Prompt), strings.ToLower(note)) {
		prompt := strings.TrimSpace(task.Action.Prompt)
		if prompt == "" {
			task.Action.Prompt = note
		} else {
			task.Action.Prompt = prompt + "\n" + note
		}
	}
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
