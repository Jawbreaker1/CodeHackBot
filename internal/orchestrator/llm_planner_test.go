package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type fakePlannerClient struct {
	content string
	err     error
}

func (f fakePlannerClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{Content: f.content}, nil
}

func TestSynthesizeTaskGraphWithLLMParsesTasks(t *testing.T) {
	t.Parallel()

	client := fakePlannerClient{
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

	client := fakePlannerClient{
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
