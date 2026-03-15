package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

const llmPlannerSystemPrompt = "You are the BirdHackBot Orchestrator planner. Return JSON only, no markdown. " +
	"Follow the provided response schema exactly and keep output concise. " +
	"Create an executable task graph for authorized internal-lab security testing. " +
	"Do not use destructive actions by default. " +
	"Rules: every task must be concrete, bounded, and in-scope; dependencies must form a DAG; " +
	"if tasks can be split into independent subtasks, do so and maximize safe parallel execution up to max_parallelism; " +
	"for broad CIDR targets, prefer discovery-first fan-out (discovery -> host subsets -> validation/summarize) instead of one monolithic scan; " +
	"only keep work serialized when there is a clear dependency/safety reason, and state that reason in rationale; " +
	"ground tasks in the operator goal: preserve goal-specific entities (for example router/gateway/firewall/webapp) and include at least one explicit task focused on that entity; " +
	"never emit placeholder/demo/example-only commands that just print canned findings; command actions must run real tooling against in-scope targets or prior task artifacts; " +
	"for action.type=command, set action.command to the executable and pass flags/inputs in action.args; use action.type=shell only for compound shell bodies; " +
	"prefer leaving action.working_dir empty so execution uses the default workspace; when set, it must be a real concise filesystem path (never narrative text). " +
	"Use input.default_working_dir as the preferred base path when a working directory is required, and never invent synthetic paths like /home/user/lab or /lab/...; " +
	"when input.playbooks is provided, use it as bounded procedural guidance and adapt tasks to those playbooks without copying blindly; " +
	"if the goal asks for vulnerabilities, include a dedicated vulnerability-mapping step tied to discovered versions/configuration; " +
	"for vulnerability/CVE outputs, require source-backed validation tasks (tool/advisory evidence plus target applicability checks) and avoid memory-only CVE claims."

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
	Temperature   *float32
	MaxTokens     *int
	Playbooks     string
	UseJSONSchema *bool
	Trace         func(LLMPlannerTrace)
}

type LLMPlannerTrace struct {
	RequestPayload string
	RawResponse    string
	ExtractedJSON  string
	FinishReason   string
	Fingerprint    string
	Stage          string
}

type LLMPlannerFailure struct {
	Stage        string
	Cause        error
	RawResponse  string
	Extracted    string
	Fingerprint  string
	FinishReason string
}

func (e *LLMPlannerFailure) Error() string {
	if e == nil {
		return "llm planner failure"
	}
	stage := strings.TrimSpace(e.Stage)
	if stage == "" {
		stage = "unknown"
	}
	if e.Cause == nil {
		return "llm planner " + stage + " failure"
	}
	return fmt.Sprintf("llm planner %s failure: %v", stage, e.Cause)
}

func (e *LLMPlannerFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
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
	if cwd, err := os.Getwd(); err == nil {
		if trimmed := strings.TrimSpace(cwd); trimmed != "" {
			payload["default_working_dir"] = trimmed
		}
	}
	if trimmed := strings.TrimSpace(options.Playbooks); trimmed != "" {
		payload["playbooks"] = trimmed
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("marshal planner payload: %w", err)
	}
	requestPayload := string(data)

	temperature := float32(0.1)
	if options.Temperature != nil {
		temperature = *options.Temperature
	}
	maxTokens := 0
	if options.MaxTokens != nil && *options.MaxTokens > 0 {
		maxTokens = *options.MaxTokens
	}
	useJSONSchema := true
	if options.UseJSONSchema != nil {
		useJSONSchema = *options.UseJSONSchema
	}
	request := llm.ChatRequest{
		Model:       strings.TrimSpace(model),
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Messages: []llm.Message{
			{Role: "system", Content: llmPlannerSystemPrompt},
			{Role: "user", Content: requestPayload},
		},
	}
	if useJSONSchema {
		request.ResponseFormat = llmPlannerJSONSchemaResponseFormat()
	}
	resp, err := client.Chat(ctx, request)
	if err != nil {
		if options.Trace != nil {
			options.Trace(LLMPlannerTrace{
				RequestPayload: requestPayload,
				Stage:          "request",
			})
		}
		return nil, "", &LLMPlannerFailure{
			Stage: "request",
			Cause: fmt.Errorf("llm planner request: %w", err),
		}
	}

	rawContent := strings.TrimSpace(resp.Content)
	finishReason := strings.ToLower(strings.TrimSpace(resp.FinishReason))
	raw := extractPlannerJSON(rawContent)
	decoded := llmPlannerResponse{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		stage := "parse"
		cause := fmt.Errorf("parse llm planner response: %w", err)
		if finishReason == "length" {
			stage = "truncate"
			cause = fmt.Errorf("planner output truncated by token limit: %w", err)
		}
		if options.Trace != nil {
			options.Trace(LLMPlannerTrace{
				RequestPayload: requestPayload,
				RawResponse:    rawContent,
				ExtractedJSON:  raw,
				FinishReason:   finishReason,
				Fingerprint:    llmPlannerFingerprint(raw),
				Stage:          stage,
			})
		}
		return nil, "", &LLMPlannerFailure{
			Stage:        stage,
			Cause:        cause,
			RawResponse:  rawContent,
			Extracted:    raw,
			Fingerprint:  llmPlannerFingerprint(raw),
			FinishReason: finishReason,
		}
	}
	if len(decoded.Tasks) == 0 {
		if options.Trace != nil {
			options.Trace(LLMPlannerTrace{
				RequestPayload: requestPayload,
				RawResponse:    rawContent,
				ExtractedJSON:  raw,
				FinishReason:   finishReason,
				Fingerprint:    llmPlannerFingerprint(raw),
				Stage:          "parse",
			})
		}
		return nil, "", &LLMPlannerFailure{
			Stage:        "parse",
			Cause:        fmt.Errorf("llm planner returned no tasks"),
			RawResponse:  rawContent,
			Extracted:    raw,
			Fingerprint:  llmPlannerFingerprint(raw),
			FinishReason: finishReason,
		}
	}

	tasks := make([]TaskSpec, 0, len(decoded.Tasks))
	for i, task := range decoded.Tasks {
		spec, err := toTaskSpec(task, i, scope)
		if err != nil {
			if options.Trace != nil {
				options.Trace(LLMPlannerTrace{
					RequestPayload: requestPayload,
					RawResponse:    rawContent,
					ExtractedJSON:  raw,
					FinishReason:   finishReason,
					Fingerprint:    llmPlannerFingerprint(raw),
					Stage:          "validate",
				})
			}
			return nil, "", &LLMPlannerFailure{
				Stage:        "validate",
				Cause:        err,
				RawResponse:  rawContent,
				Extracted:    raw,
				Fingerprint:  llmPlannerFingerprint(raw),
				FinishReason: finishReason,
			}
		}
		tasks = append(tasks, spec)
	}
	tasks = applyGoalAnchoring(tasks, goal)
	if options.Trace != nil {
		options.Trace(LLMPlannerTrace{
			RequestPayload: requestPayload,
			RawResponse:    rawContent,
			ExtractedJSON:  raw,
			FinishReason:   finishReason,
			Fingerprint:    llmPlannerFingerprint(raw),
			Stage:          "success",
		})
	}
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
	riskLevel = normalizeLLMTaskRiskLevel(riskLevel)
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
	budget = normalizeLLMTaskBudget(
		budget,
		riskLevel,
		actionType,
		strings.TrimSpace(task.Title),
		strings.TrimSpace(task.Goal),
		strings.TrimSpace(task.Strategy),
		strings.TrimSpace(task.Action.Prompt),
	)
	normalizedAction, err := normalizeTaskAction(TaskAction{
		Type:           actionType,
		Prompt:         strings.TrimSpace(task.Action.Prompt),
		Command:        strings.TrimSpace(task.Action.Command),
		Args:           compactStringSlice(task.Action.Args),
		WorkingDir:     strings.TrimSpace(task.Action.WorkingDir),
		TimeoutSeconds: task.Action.TimeoutSeconds,
	})
	if err != nil {
		return TaskSpec{}, fmt.Errorf("normalize llm task action: %w", err)
	}
	spec := TaskSpec{
		TaskID:            taskID,
		Title:             strings.TrimSpace(task.Title),
		Goal:              strings.TrimSpace(task.Goal),
		Targets:           normalizeLLMTaskTargets(task.Targets, scope),
		DependsOn:         compactStringSlice(task.DependsOn),
		Priority:          task.Priority,
		Strategy:          strings.TrimSpace(task.Strategy),
		Action:            normalizedAction,
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
	spec = maybePromoteVulnerabilityMappingToAssist(spec)
	spec.Budget = normalizeLLMTaskBudget(
		spec.Budget,
		spec.RiskLevel,
		spec.Action.Type,
		spec.Title,
		spec.Goal,
		spec.Strategy,
		spec.Action.Prompt,
	)
	if err := ValidateTaskSpec(spec); err != nil {
		return TaskSpec{}, fmt.Errorf("llm planner task %s invalid: %w", spec.TaskID, err)
	}
	return spec, nil
}

func maybePromoteVulnerabilityMappingToAssist(spec TaskSpec) TaskSpec {
	if !taskRequiresVulnerabilityEvidence(spec) {
		return spec
	}
	if strings.ToLower(strings.TrimSpace(spec.Action.Type)) != "command" {
		return spec
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(spec.Action.Command)))
	if !isFragileVulnMapperCommand(base) {
		return spec
	}
	if hasSubstantiveVulnerabilityQueryArgs(spec.Action.Args, spec.Targets) &&
		!(isInputOnlyFragileVulnMapper(base) && hasOnlyArtifactInputArgs(spec.Action.Args)) {
		return spec
	}
	prompt := strings.TrimSpace(spec.Action.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(spec.Goal)
	}
	if prompt == "" {
		prompt = "Map discovered services and versions to source-validated CVEs."
	}
	spec.Action = TaskAction{
		Type:           "assist",
		Prompt:         strings.TrimSpace(prompt + "\nUse prior discovery artifacts to derive product/version queries for searchsploit/metasploit/advisory checks. Do not rely on bare target IP-only lookups or input-file-only wrapper commands; validate CVE applicability against observed service/version evidence and cite artifact paths."),
		WorkingDir:     spec.Action.WorkingDir,
		TimeoutSeconds: spec.Action.TimeoutSeconds,
	}
	return spec
}

func isFragileVulnMapperCommand(base string) bool {
	base = normalizeVulnMapperCommandBase(base)
	switch base {
	case "searchsploit", "msfconsole", "metasploit", "vulnerscan", "vulners", "nmap-vulners":
		return true
	}
	if strings.Contains(base, "vulners") && (strings.Contains(base, "nmap") || strings.Contains(base, "wrapper")) {
		return true
	}
	return strings.HasPrefix(base, "nmap-vuln")
}

func isInputOnlyFragileVulnMapper(base string) bool {
	base = normalizeVulnMapperCommandBase(base)
	switch base {
	case "vulnerscan", "vulners", "nmap-vulners":
		return true
	default:
		if strings.Contains(base, "vulners") && (strings.Contains(base, "nmap") || strings.Contains(base, "wrapper")) {
			return true
		}
		return strings.HasPrefix(base, "nmap-vuln")
	}
}

func normalizeVulnMapperCommandBase(base string) string {
	base = strings.TrimSpace(strings.ToLower(filepath.Base(base)))
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.ReplaceAll(base, "_", "-")
	return base
}

func hasOnlyArtifactInputArgs(args []string) bool {
	sawArtifactArg := false
	skipNext := false
	for _, raw := range args {
		arg := strings.TrimSpace(raw)
		if arg == "" {
			continue
		}
		lower := strings.ToLower(arg)
		if skipNext {
			skipNext = false
			if !looksLikeArtifactPath(lower) {
				return false
			}
			sawArtifactArg = true
			continue
		}
		if strings.HasPrefix(lower, "-") {
			switch lower {
			case "-i", "--input", "-o", "--output":
				skipNext = true
			}
			continue
		}
		if !looksLikeArtifactPath(lower) {
			return false
		}
		sawArtifactArg = true
	}
	return sawArtifactArg
}

func looksLikeArtifactPath(arg string) bool {
	arg = strings.TrimSpace(strings.ToLower(arg))
	if arg == "" {
		return false
	}
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
		return true
	}
	if strings.Contains(arg, "/") {
		return true
	}
	for _, ext := range []string{".xml", ".json", ".txt", ".log", ".nmap", ".csv"} {
		if strings.HasSuffix(arg, ext) {
			return true
		}
	}
	return false
}

func hasSubstantiveVulnerabilityQueryArgs(args []string, knownTargets []string) bool {
	targetSet := make(map[string]struct{}, len(knownTargets))
	for _, target := range knownTargets {
		target = strings.ToLower(strings.TrimSpace(target))
		if target != "" {
			targetSet[target] = struct{}{}
		}
	}
	skipNext := false
	for _, raw := range args {
		arg := strings.TrimSpace(raw)
		if arg == "" {
			continue
		}
		lower := strings.ToLower(arg)
		if skipNext {
			skipNext = false
			if !looksLikeTargetOnlyQueryArg(lower, targetSet) {
				return true
			}
			continue
		}
		if strings.HasPrefix(lower, "-") {
			// Keep parser simple for common command syntaxes (e.g. msfconsole -x "...").
			if lower == "-x" || lower == "--exec" || lower == "-e" {
				skipNext = true
			}
			continue
		}
		if !looksLikeTargetOnlyQueryArg(lower, targetSet) {
			return true
		}
	}
	return false
}

func looksLikeTargetOnlyQueryArg(arg string, targetSet map[string]struct{}) bool {
	arg = strings.TrimSpace(strings.ToLower(arg))
	if arg == "" {
		return true
	}
	if _, ok := targetSet[arg]; ok {
		return true
	}
	if looksLikeIPv4(arg) || strings.Contains(arg, "/") {
		return true
	}
	if strings.Contains(arg, ":") {
		return true
	}
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		return true
	}
	return false
}

func looksLikeIPv4(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
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

func normalizeLLMTaskBudget(
	budget TaskBudget,
	riskLevel string,
	actionType string,
	title string,
	goal string,
	strategy string,
	prompt string,
) TaskBudget {
	risk := strings.ToLower(strings.TrimSpace(riskLevel))
	minSteps := 6
	minToolCalls := 8
	minRuntime := 4 * time.Minute
	if risk == string(RiskActiveProbe) {
		minSteps = 8
		minToolCalls = 12
		minRuntime = 6 * time.Minute
	}
	actionType = strings.ToLower(strings.TrimSpace(actionType))
	intentText := strings.ToLower(strings.TrimSpace(strings.Join([]string{title, goal, strategy, prompt}, " ")))
	if actionType == "assist" {
		assistMinSteps := 10
		assistMinToolCalls := 14
		assistMinRuntime := 6 * time.Minute
		if taskLikelySummaryIntent(intentText) {
			assistMinSteps = 6
			assistMinToolCalls = 8
			assistMinRuntime = 4 * time.Minute
		} else if taskLikelyDeepValidationIntent(intentText) {
			assistMinSteps = 14
			assistMinToolCalls = 20
			assistMinRuntime = 8 * time.Minute
		}
		if assistMinSteps > minSteps {
			minSteps = assistMinSteps
		}
		if assistMinToolCalls > minToolCalls {
			minToolCalls = assistMinToolCalls
		}
		if assistMinRuntime > minRuntime {
			minRuntime = assistMinRuntime
		}
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
	if budget.MaxSteps > maxSynthesizedTaskSteps {
		budget.MaxSteps = maxSynthesizedTaskSteps
	}
	if budget.MaxToolCalls > maxSynthesizedTaskToolCalls {
		budget.MaxToolCalls = maxSynthesizedTaskToolCalls
	}
	if budget.MaxRuntime > maxSynthesizedTaskRuntime {
		budget.MaxRuntime = maxSynthesizedTaskRuntime
	}
	return budget
}

func taskLikelySummaryIntent(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	return containsAny(
		text,
		"summarize",
		"summary",
		"final report",
		"report synthesis",
		"plan summary",
	)
}

func taskLikelyDeepValidationIntent(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	return containsAny(
		text,
		"validate",
		"validation",
		"verify",
		"verification",
		"vulnerab",
		"cve",
		"exploit",
		"post-access",
		"triage",
		"recover",
		"recovery",
	)
}

func normalizeLLMTaskRiskLevel(raw string) string {
	tier, err := ParseRiskTier(raw)
	if err != nil {
		return string(RiskReconReadonly)
	}
	switch tier {
	case RiskReconReadonly, RiskActiveProbe:
		return string(tier)
	default:
		// Synthesized plans are bounded to recon/active probe. Clamp higher-risk
		// planner output into active probing instead of hard-failing the run.
		return string(RiskActiveProbe)
	}
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

func llmPlannerFingerprint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:8])
}

func llmPlannerJSONSchemaResponseFormat() map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "birdhackbot_orchestrator_plan",
			"strict": true,
			"schema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"rationale", "tasks"},
				"properties": map[string]any{
					"rationale": map[string]any{
						"type":      "string",
						"maxLength": 1200,
					},
					"tasks": map[string]any{
						"type":     "array",
						"maxItems": 10,
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required": []string{
								"task_id", "title", "goal", "targets", "depends_on", "priority", "strategy",
								"risk_level", "done_when", "fail_when", "expected_artifacts", "action", "budget",
							},
							"properties": map[string]any{
								"task_id": map[string]any{"type": "string", "maxLength": 64},
								"title":   map[string]any{"type": "string", "maxLength": 120},
								"goal":    map[string]any{"type": "string", "maxLength": 240},
								"targets": map[string]any{
									"type":     "array",
									"maxItems": 16,
									"items":    map[string]any{"type": "string", "maxLength": 128},
								},
								"depends_on": map[string]any{
									"type":     "array",
									"maxItems": 16,
									"items":    map[string]any{"type": "string", "maxLength": 64},
								},
								"priority": map[string]any{"type": "integer"},
								"strategy": map[string]any{"type": "string", "maxLength": 120},
								"risk_level": map[string]any{
									"type": "string",
									"enum": []string{"recon_readonly", "active_probe", "exploit_controlled", "priv_esc", "disruptive"},
								},
								"done_when": map[string]any{
									"type":     "array",
									"maxItems": 4,
									"items":    map[string]any{"type": "string", "maxLength": 180},
								},
								"fail_when": map[string]any{
									"type":     "array",
									"maxItems": 4,
									"items":    map[string]any{"type": "string", "maxLength": 180},
								},
								"expected_artifacts": map[string]any{
									"type":     "array",
									"maxItems": 6,
									"items":    map[string]any{"type": "string", "maxLength": 160},
								},
								"action": map[string]any{
									"type":                 "object",
									"additionalProperties": false,
									"required":             []string{"type"},
									"properties": map[string]any{
										"type":            map[string]any{"type": "string", "enum": []string{"assist", "command", "shell"}},
										"prompt":          map[string]any{"type": "string", "maxLength": 800},
										"command":         map[string]any{"type": "string", "maxLength": 1024},
										"args":            map[string]any{"type": "array", "maxItems": 24, "items": map[string]any{"type": "string", "maxLength": 1024}},
										"working_dir":     map[string]any{"type": "string", "maxLength": 256},
										"timeout_seconds": map[string]any{"type": "integer"},
									},
								},
								"budget": map[string]any{
									"type":                 "object",
									"additionalProperties": false,
									"required":             []string{"max_steps", "max_tool_calls", "max_runtime_seconds"},
									"properties": map[string]any{
										"max_steps":           map[string]any{"type": "integer"},
										"max_tool_calls":      map[string]any{"type": "integer"},
										"max_runtime_seconds": map[string]any{"type": "integer"},
									},
								},
							},
						},
					},
				},
			},
		},
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
