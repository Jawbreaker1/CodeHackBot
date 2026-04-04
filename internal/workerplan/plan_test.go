package workerplan

import "testing"

func TestValidatePlannedExecutionOK(t *testing.T) {
	p := Plan{
		Mode:       ModePlannedExecution,
		WorkerGoal: "extract secret.zip and identify password",
		PlanSteps: []string{
			"identify archive input",
			"inspect encryption and metadata",
			"attempt bounded recovery",
			"verify extraction",
			"report result",
		},
		ActiveStep:       "identify archive input",
		ReplanConditions: []string{"current step is genuinely blocked"},
	}
	if report := Validate(p); !report.Valid() {
		t.Fatalf("Validate() unexpected error = %v", report.Error())
	}
}

func TestValidateRejectsProceduralSteps(t *testing.T) {
	p := Plan{Mode: ModePlannedExecution, WorkerGoal: "x", PlanSteps: []string{"ls -la", "report"}, ActiveStep: "ls -la"}
	if report := Validate(p); report.Valid() {
		t.Fatalf("Validate() expected invalid plan")
	}
}

func TestParsePlan(t *testing.T) {
	raw := `{"mode":"planned_execution","worker_goal":"inspect zip","plan_summary":"x","plan_steps":["inspect archive","report result"],"active_step":"inspect archive","replan_conditions":["blocked"]}`
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if p.Mode != ModePlannedExecution || p.ActiveStep != "inspect archive" {
		t.Fatalf("unexpected parsed plan: %#v", p)
	}
}
