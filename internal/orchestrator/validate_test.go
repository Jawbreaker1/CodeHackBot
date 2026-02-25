package orchestrator

import (
	"errors"
	"strings"
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

func TestValidateTaskSpec_AssistActionAllowed(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "test assist action",
		DoneWhen:          []string{"d"},
		FailWhen:          []string{"f"},
		ExpectedArtifacts: []string{"a"},
		RiskLevel:         "recon_readonly",
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Investigate and summarize findings.",
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   time.Second,
		},
	}
	if err := ValidateTaskSpec(task); err != nil {
		t.Fatalf("expected assist action to validate, got %v", err)
	}
}

func TestValidateTaskSpec_AssistActionRejectsCommandArgs(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "test assist action",
		DoneWhen:          []string{"d"},
		FailWhen:          []string{"f"},
		ExpectedArtifacts: []string{"a"},
		RiskLevel:         "recon_readonly",
		Action: TaskAction{
			Type:    "assist",
			Command: "ls",
			Args:    []string{"-la"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
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

func TestValidateTaskSpec_RejectsMalformedShellWrapperBody(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "locate archive",
		DoneWhen:          []string{"located"},
		FailWhen:          []string{"not_found"},
		ExpectedArtifacts: []string{"scan.log"},
		RiskLevel:         "recon_readonly",
		Action: TaskAction{
			Type:    "command",
			Command: "bash",
			Args:    []string{"-lc", `find /tmp -name "secret.zip`},
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
	if !strings.Contains(err.Error(), "unbalanced quotes") {
		t.Fatalf("expected malformed quote detail, got %v", err)
	}
}

func TestValidateTaskSpec_AllowsWellFormedShellWrapperBody(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t1",
		Goal:              "locate archive",
		DoneWhen:          []string{"located"},
		FailWhen:          []string{"not_found"},
		ExpectedArtifacts: []string{"scan.log"},
		RiskLevel:         "recon_readonly",
		Action: TaskAction{
			Type:    "command",
			Command: "bash",
			Args:    []string{"-lc", `find /tmp -name "secret.zip" -type f 2>/dev/null`},
		},
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   time.Second,
		},
	}
	if err := ValidateTaskSpec(task); err != nil {
		t.Fatalf("expected shell wrapper to validate, got %v", err)
	}
}
