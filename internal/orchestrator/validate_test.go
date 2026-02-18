package orchestrator

import (
	"errors"
	"testing"
	"time"
)

func TestValidateRunPlan_RejectsMissingFields(t *testing.T) {
	t.Parallel()

	err := ValidateRunPlan(RunPlan{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("expected ErrInvalidPlan, got %v", err)
	}
}

func TestValidateTaskSpec_RejectsInvalidBudget(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "test",
		DoneWhen:          []string{"d"},
		FailWhen:          []string{"f"},
		ExpectedArtifacts: []string{"a"},
		RiskLevel:         "recon_readonly",
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 0,
			MaxRuntime:   time.Second,
		},
	}
	err := ValidateTaskSpec(task)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("expected ErrInvalidTask, got %v", err)
	}
}

func TestValidateEventEnvelope(t *testing.T) {
	t.Parallel()

	event := EventEnvelope{
		EventID:  "e1",
		RunID:    "r1",
		WorkerID: "w1",
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeTaskStarted,
	}
	if err := ValidateEventEnvelope(event); err != nil {
		t.Fatalf("expected valid event, got %v", err)
	}
}

func TestValidateTaskSpec_RejectsInvalidAction(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "test",
		DoneWhen:          []string{"d"},
		FailWhen:          []string{"f"},
		ExpectedArtifacts: []string{"a"},
		RiskLevel:         "recon_readonly",
		Action: TaskAction{
			Type: "python",
		},
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   time.Second,
		},
	}
	err := ValidateTaskSpec(task)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("expected ErrInvalidTask, got %v", err)
	}
}
