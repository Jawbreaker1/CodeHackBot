package interactivecli

import (
	"strings"
	"testing"
)

func TestRenderDashboardShowsPaneHeadersAndInput(t *testing.T) {
	view := ViewState{
		SessionStatus:     "active",
		Goal:              "Extract contents of secret.zip",
		ActiveStep:        "Attempt password recovery",
		CurrentStepState:  StepVisualInProgress,
		LatestCommand:     "fcrackzip -u -D ...",
		LatestActionReview: ReviewVisualRevise,
		Model:             "qwen3.5-27b",
		ApprovalState:     "approved_session",
		PlanSteps: []PlanStepView{
			{Label: "Inspect metadata", State: StepVisualDone},
			{Label: "Attempt password recovery", State: StepVisualInProgress},
		},
	}
	out := renderDashboard(view, []string{"User: test", "Assistant: ok"}, "birdhackbot> ")
	for _, needle := range []string{"Stream", "Status", "Input", "Commands: /status /step /plan /lastlog /stats /packet /help", "Extract contents of secret.zip", "Attempt password recovery", "fcrackzip -u -D"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("dashboard missing %q:\n%s", needle, out)
		}
	}
}
