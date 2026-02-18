package orchestrator

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRunPlanRoundTrip(t *testing.T) {
	t.Parallel()

	in := RunPlan{
		RunID:           "run-1",
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"report_created"},
		StopCriteria:    []string{"out_of_scope"},
		MaxParallelism:  2,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Title:             "Enumerate host",
				Goal:              "Find open ports on target",
				DoneWhen:          []string{"nmap_log_exists"},
				FailWhen:          []string{"target_unreachable"},
				ExpectedArtifacts: []string{"logs/nmap.txt"},
				RiskLevel:         "active_probe",
				Budget: TaskBudget{
					MaxSteps:     8,
					MaxToolCalls: 8,
					MaxRuntime:   5 * time.Minute,
				},
			},
		},
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out RunPlan
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := ValidateRunPlan(out); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if out.RunID != in.RunID {
		t.Fatalf("run_id mismatch: got %q want %q", out.RunID, in.RunID)
	}
}

func TestBackwardCompatibleParse_IgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"run_id":"run-1",
		"constraints":["internal_only"],
		"success_criteria":["ok"],
		"stop_criteria":["manual_stop"],
		"max_parallelism":1,
		"legacy_field":"keep_ignored",
		"tasks":[{
			"task_id":"t1",
			"goal":"collect banner",
			"done_when":["banner_present"],
			"fail_when":["timeout"],
			"expected_artifacts":["banner.txt"],
			"risk_level":"recon_readonly",
			"budget":{"max_steps":3,"max_tool_calls":3,"max_runtime":1000000000},
			"legacy_task_field":"ignored"
		}]
	}`)

	var plan RunPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := ValidateRunPlan(plan); err != nil {
		t.Fatalf("validate: %v", err)
	}
}
