package workertask

import (
	"strings"
	"testing"
)

func TestParseDecision(t *testing.T) {
	raw := `{"action":"start_new_task","reason":"previous task is complete and user asked for a new goal"}`
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Action != ActionStartNewTask {
		t.Fatalf("action = %q, want %q", got.Action, ActionStartNewTask)
	}
}

func TestValidateDecisionAcceptsSupportedActions(t *testing.T) {
	for _, decision := range []Decision{
		{Action: ActionContinueActiveTask, Reason: "latest turn refines the current task"},
		{Action: ActionStartNewTask, Reason: "previous task is done and latest turn starts a new goal"},
	} {
		if report := Validate(decision); !report.Valid() {
			t.Fatalf("Validate(%+v) unexpected error = %v", decision, report.Error())
		}
	}
}

func TestValidateDecisionRejectsInvalidShape(t *testing.T) {
	report := Validate(Decision{Action: "maybe", Reason: ""})
	if report.Valid() {
		t.Fatalf("Validate() expected invalid report")
	}
	for _, want := range []string{"unsupported task boundary action", "task boundary action requires reason"} {
		if !strings.Contains(report.Error().Error(), want) {
			t.Fatalf("report missing %q in %v", want, report.Error())
		}
	}
}

func TestFallbackDecisionStartsNewTask(t *testing.T) {
	got := FallbackDecision()
	if got.Action != ActionStartNewTask {
		t.Fatalf("action = %q, want %q", got.Action, ActionStartNewTask)
	}
	if strings.TrimSpace(got.Reason) == "" {
		t.Fatal("reason must not be empty")
	}
}
