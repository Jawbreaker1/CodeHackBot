package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type fakePlannerClient struct {
	content      string
	finishReason string
	err          error
	reqs         []llm.ChatRequest
}

func (f *fakePlannerClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{Content: f.content, FinishReason: f.finishReason}, nil
}

func TestSynthesizeTaskGraphWithLLMParsesTasks(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"rationale":"fan out recon and validate findings",
			"tasks":[
				{
					"task_id":"task-recon-a",
					"title":"Recon part A",
					"goal":"Enumerate service exposure in 192.168.50.0/25",
					"targets":["192.168.50.0/25"],
					"depends_on":[],
					"priority":100,
					"strategy":"recon",
					"risk_level":"recon_readonly",
					"done_when":["recon_done"],
					"fail_when":["recon_failed"],
					"expected_artifacts":["recon-a.log"],
					"action":{"type":"assist","prompt":"enumerate hosts and services"},
					"budget":{"max_steps":10,"max_tool_calls":12,"max_runtime_seconds":300}
				},
				{
					"task_id":"task-validate",
					"title":"Validate hypotheses",
					"goal":"Validate hypotheses from recon results",
					"targets":["192.168.50.0/24"],
					"depends_on":["task-recon-a"],
					"priority":80,
					"strategy":"hypothesis_validate",
					"risk_level":"active_probe",
					"done_when":["validated"],
					"fail_when":["validation_failed"],
					"expected_artifacts":["validate.log"],
					"action":{"type":"assist","prompt":"validate findings"},
					"budget":{"max_steps":12,"max_tool_calls":16,"max_runtime_seconds":420}
				}
			]
		}`,
	}

	tasks, rationale, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"qwen/qwen3-coder-30b",
		"map services",
		Scope{Networks: []string{"192.168.50.0/24"}},
		[]string{"internal_lab_only"},
		nil,
		2,
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLM: %v", err)
	}
	if rationale == "" {
		t.Fatalf("expected rationale")
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[1].DependsOn[0] != "task-recon-a" {
		t.Fatalf("unexpected dependency graph: %#v", tasks[1].DependsOn)
	}
}

func TestSynthesizeTaskGraphWithLLMRejectsInvalidTask(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{"tasks":[{"task_id":"t1","goal":"missing required fields"}]}`,
	}
	_, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"model",
		"goal",
		Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		nil,
		1,
	)
	if err == nil {
		t.Fatalf("expected validation error for invalid llm task")
	}
}

func TestSynthesizeTaskGraphWithLLMNormalizesInlineShellCommandAction(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"tasks":[
				{
					"task_id":"t1",
					"title":"discover archive",
					"goal":"find archive path",
					"targets":["127.0.0.1"],
					"depends_on":[],
					"priority":1,
					"strategy":"discovery",
					"risk_level":"recon_readonly",
					"done_when":["path found"],
					"fail_when":["path missing"],
					"expected_artifacts":["secret.zip_path"],
					"action":{"type":"command","command":"find /tmp -name secret.zip -type f || find /home -name secret.zip -type f"},
					"budget":{"max_steps":6,"max_tool_calls":8,"max_runtime_seconds":240}
				}
			]
		}`,
	}
	tasks, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"model",
		"locate secret archive",
		Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		nil,
		1,
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLM: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if got := tasks[0].Action.Command; got != "bash" {
		t.Fatalf("expected normalized shell wrapper command bash, got %q", got)
	}
	if len(tasks[0].Action.Args) != 2 || tasks[0].Action.Args[0] != "-lc" {
		t.Fatalf("expected normalized shell args [-lc <body>], got %#v", tasks[0].Action.Args)
	}
}

func TestSynthesizeTaskGraphWithLLMRejectsMalformedInlineShellCommandAction(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"tasks":[
				{
					"task_id":"t1",
					"title":"discover archive",
					"goal":"find archive path",
					"targets":["127.0.0.1"],
					"depends_on":[],
					"priority":1,
					"strategy":"discovery",
					"risk_level":"recon_readonly",
					"done_when":["path found"],
					"fail_when":["path missing"],
					"expected_artifacts":["secret.zip_path"],
					"action":{"type":"command","command":"find /tmp -name \"secret.zip\" -type f || find /home -name \"secret.zip\" -type f || find /var -name \"secret"},
					"budget":{"max_steps":6,"max_tool_calls":8,"max_runtime_seconds":240}
				}
			]
		}`,
	}
	_, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"model",
		"locate secret archive",
		Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		nil,
		1,
	)
	if err == nil {
		t.Fatalf("expected validation failure for malformed shell body")
	}
	var plannerFailure *LLMPlannerFailure
	if !errors.As(err, &plannerFailure) {
		t.Fatalf("expected LLMPlannerFailure, got %T", err)
	}
	if plannerFailure.Stage != "validate" {
		t.Fatalf("expected validate stage, got %q", plannerFailure.Stage)
	}
	if !strings.Contains(strings.ToLower(plannerFailure.Error()), "unbalanced quotes") {
		t.Fatalf("expected malformed shell quote validation error, got: %v", plannerFailure)
	}
}

func TestLLMPlannerJSONSchemaActionLengthAllowsLongerShellBodies(t *testing.T) {
	t.Parallel()

	format := llmPlannerJSONSchemaResponseFormat()
	jsonSchema, ok := format["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("json_schema missing")
	}
	schema, ok := jsonSchema["schema"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing")
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties missing")
	}
	tasksProp, ok := properties["tasks"].(map[string]any)
	if !ok {
		t.Fatalf("tasks property missing")
	}
	taskItems, ok := tasksProp["items"].(map[string]any)
	if !ok {
		t.Fatalf("tasks.items missing")
	}
	taskProps, ok := taskItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tasks.items.properties missing")
	}
	actionProp, ok := taskProps["action"].(map[string]any)
	if !ok {
		t.Fatalf("action property missing")
	}
	actionProps, ok := actionProp["properties"].(map[string]any)
	if !ok {
		t.Fatalf("action.properties missing")
	}
	commandProp, ok := actionProps["command"].(map[string]any)
	if !ok {
		t.Fatalf("action.command missing")
	}
	if got := int(commandProp["maxLength"].(int)); got < 512 {
		t.Fatalf("expected action.command maxLength >= 512, got %d", got)
	}
	argsProp, ok := actionProps["args"].(map[string]any)
	if !ok {
		t.Fatalf("action.args missing")
	}
	argItem, ok := argsProp["items"].(map[string]any)
	if !ok {
		t.Fatalf("action.args.items missing")
	}
	if got := int(argItem["maxLength"].(int)); got < 512 {
		t.Fatalf("expected action.args item maxLength >= 512, got %d", got)
	}
}

func TestSynthesizeTaskGraphWithLLMClassifiesTruncatedOutput(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content:      `{"rationale":"partial","tasks":[`,
		finishReason: "length",
	}
	_, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"model",
		"goal",
		Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		nil,
		1,
	)
	if err == nil {
		t.Fatalf("expected parse failure")
	}
	var plannerFailure *LLMPlannerFailure
	if !errors.As(err, &plannerFailure) {
		t.Fatalf("expected LLMPlannerFailure, got %T", err)
	}
	if plannerFailure.Stage != "truncate" {
		t.Fatalf("expected truncate stage, got %q", plannerFailure.Stage)
	}
	if plannerFailure.FinishReason != "length" {
		t.Fatalf("expected finish reason length, got %q", plannerFailure.FinishReason)
	}
}

func TestSynthesizeTaskGraphWithLLMAppliesBudgetFloors(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"tasks":[
				{
					"task_id":"task-recon-low",
					"title":"Recon",
					"goal":"recon",
					"targets":["192.168.50.0/24"],
					"priority":80,
					"strategy":"recon",
					"risk_level":"recon_readonly",
					"done_when":["done"],
					"fail_when":["fail"],
					"expected_artifacts":["recon.log"],
					"action":{"type":"assist","prompt":"recon"},
					"budget":{"max_steps":1,"max_tool_calls":1,"max_runtime_seconds":30}
				},
				{
					"task_id":"task-probe-low",
					"title":"Probe",
					"goal":"probe",
					"targets":["192.168.50.0/24"],
					"priority":70,
					"strategy":"active_probe",
					"risk_level":"active_probe",
					"done_when":["done"],
					"fail_when":["fail"],
					"expected_artifacts":["probe.log"],
					"action":{"type":"assist","prompt":"probe"},
					"budget":{"max_steps":2,"max_tool_calls":2,"max_runtime_seconds":45}
				}
			]
		}`,
	}

	tasks, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"model",
		"map services",
		Scope{Networks: []string{"192.168.50.0/24"}},
		[]string{"internal_lab_only"},
		nil,
		2,
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLM: %v", err)
	}
	if got := tasks[0].Budget.MaxSteps; got < 6 {
		t.Fatalf("expected recon step floor, got %d", got)
	}
	if got := tasks[0].Budget.MaxRuntime; got < 4*time.Minute {
		t.Fatalf("expected recon runtime floor, got %s", got)
	}
	if got := tasks[1].Budget.MaxSteps; got < 8 {
		t.Fatalf("expected active probe step floor, got %d", got)
	}
	if got := tasks[1].Budget.MaxRuntime; got < 6*time.Minute {
		t.Fatalf("expected active probe runtime floor, got %s", got)
	}
}

func TestSynthesizeTaskGraphWithLLMClampsBudgetCeilings(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"tasks":[
				{
					"task_id":"task-oversized-budget",
					"title":"Oversized budget task",
					"goal":"collect evidence",
					"targets":["127.0.0.1"],
					"priority":90,
					"strategy":"recon",
					"risk_level":"recon_readonly",
					"done_when":["done"],
					"fail_when":["fail"],
					"expected_artifacts":["out.log"],
					"action":{"type":"assist","prompt":"collect evidence"},
					"budget":{"max_steps":9999,"max_tool_calls":9999,"max_runtime_seconds":99999}
				}
			]
		}`,
	}

	tasks, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"model",
		"collect evidence",
		Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		nil,
		1,
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLM: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if got := tasks[0].Budget.MaxSteps; got != maxSynthesizedTaskSteps {
		t.Fatalf("expected max steps cap %d, got %d", maxSynthesizedTaskSteps, got)
	}
	if got := tasks[0].Budget.MaxToolCalls; got != maxSynthesizedTaskToolCalls {
		t.Fatalf("expected max tool call cap %d, got %d", maxSynthesizedTaskToolCalls, got)
	}
	if got := tasks[0].Budget.MaxRuntime; got != maxSynthesizedTaskRuntime {
		t.Fatalf("expected max runtime cap %s, got %s", maxSynthesizedTaskRuntime, got)
	}
}

func TestSynthesizeTaskGraphWithLLMNormalizesDisallowedRiskLevels(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"tasks":[
				{
					"task_id":"task-risk-high",
					"title":"High risk task from planner",
					"goal":"map vulnerabilities",
					"targets":["192.168.50.1"],
					"priority":90,
					"strategy":"vuln_mapping",
					"risk_level":"exploit_controlled",
					"done_when":["done"],
					"fail_when":["failed"],
					"expected_artifacts":["vuln.log"],
					"action":{"type":"assist","prompt":"map vulnerabilities safely"},
					"budget":{"max_steps":8,"max_tool_calls":12,"max_runtime_seconds":300}
				},
				{
					"task_id":"task-risk-invalid",
					"title":"Invalid risk",
					"goal":"collect evidence",
					"targets":["192.168.50.1"],
					"priority":70,
					"strategy":"recon",
					"risk_level":"unknown_tier",
					"done_when":["done"],
					"fail_when":["failed"],
					"expected_artifacts":["recon.log"],
					"action":{"type":"assist","prompt":"collect evidence"},
					"budget":{"max_steps":8,"max_tool_calls":12,"max_runtime_seconds":300}
				}
			]
		}`,
	}

	tasks, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"model",
		"assess router vulnerabilities",
		Scope{Targets: []string{"192.168.50.1"}},
		[]string{"internal_lab_only"},
		nil,
		1,
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLM: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].RiskLevel != string(RiskActiveProbe) {
		t.Fatalf("expected disallowed risk to clamp to active_probe, got %q", tasks[0].RiskLevel)
	}
	if tasks[1].RiskLevel != string(RiskReconReadonly) {
		t.Fatalf("expected invalid risk to default to recon_readonly, got %q", tasks[1].RiskLevel)
	}
}

func TestParsePlannerMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: "auto"},
		{in: "auto", want: "auto"},
		{in: "static", want: "static"},
		{in: "llm", want: "llm"},
		{in: "weird", wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("mode_%s", tc.in), func(t *testing.T) {
			t.Parallel()
			got, err := ParsePlannerMode(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected mode: got %s want %s", got, tc.want)
			}
		})
	}
}

func TestSynthesizeTaskGraphWithLLMAnchorsRouterAndVulnGoal(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"rationale":"generic recon split",
			"tasks":[
				{
					"task_id":"task-1",
					"title":"Discovery",
					"goal":"Scan subnet and collect open ports",
					"targets":["192.168.50.0/24"],
					"priority":90,
					"strategy":"recon_seed",
					"risk_level":"recon_readonly",
					"done_when":["done"],
					"fail_when":["failed"],
					"expected_artifacts":["discovery.log"],
					"action":{"type":"assist","prompt":"scan subnet"},
					"budget":{"max_steps":10,"max_tool_calls":12,"max_runtime_seconds":300}
				},
				{
					"task_id":"task-2",
					"title":"Version enumeration",
					"goal":"Enumerate service versions",
					"targets":["192.168.50.0/24"],
					"depends_on":["task-1"],
					"priority":80,
					"strategy":"active_probe",
					"risk_level":"active_probe",
					"done_when":["done"],
					"fail_when":["failed"],
					"expected_artifacts":["versions.log"],
					"action":{"type":"assist","prompt":"enumerate versions"},
					"budget":{"max_steps":12,"max_tool_calls":16,"max_runtime_seconds":420}
				}
			]
		}`,
	}

	goal := "Investigate the router on this network for potential vulnerabilities"
	tasks, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"qwen/qwen3-coder-30b",
		goal,
		Scope{Networks: []string{"192.168.50.0/24"}},
		[]string{"internal_lab_only"},
		nil,
		2,
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLM: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	allText := strings.ToLower(tasks[0].Title + " " + tasks[0].Goal + " " + tasks[1].Title + " " + tasks[1].Goal)
	if !strings.Contains(allText, "router") && !strings.Contains(allText, "gateway") {
		t.Fatalf("expected router/gateway goal anchoring in tasks, got: %s", allText)
	}
	if !strings.Contains(allText, "vulnerab") && !strings.Contains(allText, "cve") {
		t.Fatalf("expected vulnerability goal anchoring in tasks, got: %s", allText)
	}
	for _, task := range tasks {
		if strings.ToLower(task.Action.Type) != "assist" {
			continue
		}
		if !strings.Contains(strings.ToLower(task.Action.Prompt), "operator goal:") {
			t.Fatalf("expected assist prompt to include operator goal, got %q", task.Action.Prompt)
		}
	}
}

func TestSynthesizeTaskGraphWithLLMRewritesOutOfScopePlaceholderTargets(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"rationale":"router focus with placeholder target",
			"tasks":[
				{
					"task_id":"task-router",
					"title":"Router scan",
					"goal":"Investigate router weaknesses",
					"targets":["router_ip"],
					"priority":90,
					"strategy":"recon_router",
					"risk_level":"recon_readonly",
					"done_when":["done"],
					"fail_when":["failed"],
					"expected_artifacts":["router.log"],
					"action":{"type":"assist","prompt":"scan router"},
					"budget":{"max_steps":10,"max_tool_calls":12,"max_runtime_seconds":300}
				}
			]
		}`,
	}

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	tasks, _, err := SynthesizeTaskGraphWithLLM(
		context.Background(),
		client,
		"qwen/qwen3-coder-30b",
		"Investigate router vulnerabilities",
		scope,
		[]string{"internal_lab_only"},
		nil,
		1,
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLM: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(tasks))
	}
	if got := tasks[0].Targets; len(got) != 1 || got[0] != "192.168.50.0/24" {
		t.Fatalf("expected placeholder target rewritten to scoped network, got %#v", got)
	}
}

func TestSynthesizeTaskGraphWithLLMWithOptionsUsesConfiguredValues(t *testing.T) {
	t.Parallel()

	temperature := float32(0.02)
	maxTokens := 444
	client := &fakePlannerClient{
		content: `{
			"rationale":"single task",
			"tasks":[
				{
					"task_id":"task-1",
					"title":"Recon",
					"goal":"Collect recon",
					"targets":["127.0.0.1"],
					"priority":90,
					"strategy":"recon_seed",
					"risk_level":"recon_readonly",
					"done_when":["done"],
					"fail_when":["failed"],
					"expected_artifacts":["recon.log"],
					"action":{"type":"assist","prompt":"collect recon"},
					"budget":{"max_steps":8,"max_tool_calls":10,"max_runtime_seconds":120}
				}
			]
		}`,
	}

	_, _, err := SynthesizeTaskGraphWithLLMWithOptions(
		context.Background(),
		client,
		"model",
		"goal",
		Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		nil,
		1,
		LLMPlannerOptions{
			Temperature: &temperature,
			MaxTokens:   &maxTokens,
		},
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLMWithOptions: %v", err)
	}
	if len(client.reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(client.reqs))
	}
	if client.reqs[0].Temperature != temperature || client.reqs[0].MaxTokens != maxTokens {
		t.Fatalf("unexpected llm options: %+v", client.reqs[0])
	}
}

func TestSynthesizeTaskGraphWithLLMWithOptionsIncludesPlaybooksInPayload(t *testing.T) {
	t.Parallel()

	client := &fakePlannerClient{
		content: `{
			"rationale":"single task",
			"tasks":[
				{
					"task_id":"task-1",
					"title":"Recon",
					"goal":"Collect recon",
					"targets":["127.0.0.1"],
					"priority":90,
					"strategy":"recon_seed",
					"risk_level":"recon_readonly",
					"done_when":["done"],
					"fail_when":["failed"],
					"expected_artifacts":["recon.log"],
					"action":{"type":"assist","prompt":"collect recon"},
					"budget":{"max_steps":8,"max_tool_calls":10,"max_runtime_seconds":120}
				}
			]
		}`,
	}

	_, _, err := SynthesizeTaskGraphWithLLMWithOptions(
		context.Background(),
		client,
		"model",
		"goal",
		Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		nil,
		1,
		LLMPlannerOptions{
			Playbooks: "- Network Scan (network-scan.md)\nStep 1",
		},
	)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraphWithLLMWithOptions: %v", err)
	}
	if len(client.reqs) != 1 || len(client.reqs[0].Messages) < 2 {
		t.Fatalf("expected one llm request with user payload")
	}
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(client.reqs[0].Messages[1].Content), &payload); err != nil {
		t.Fatalf("unmarshal llm payload: %v", err)
	}
	playbooks, _ := payload["playbooks"].(string)
	if got := strings.TrimSpace(playbooks); got == "" {
		t.Fatalf("expected playbooks in llm payload")
	}
}
