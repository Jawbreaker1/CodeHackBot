package workerloop

import (
	"context"
	"strings"
	"testing"
)

func TestGoalJudgeSeesOriginalGoalDespiteNarrowPlan(t *testing.T) {
	loop, p, _ := fixtureWorker(t, 1, completeEval)
	p.SessionFoundation.Goal = "Establish A and B"
	p.CurrentStep.DoneCondition = "A and B each have evidence"
	applyPlan(&p, PlanUpdate{Summary: "Only A", Steps: []string{"A"}, ActiveStep: "A"})
	prompt := buildGoalEvaluationPrompt(p, "A is done")
	for _, want := range []string{"Establish A and B", "A and B each have evidence", "A is done", "original goal even if the active plan omits requirements"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if _, err := judgeGoalCompletion(context.Background(), loop.LLM, nil, p, "A is done"); err != nil {
		t.Fatal(err)
	}
}
