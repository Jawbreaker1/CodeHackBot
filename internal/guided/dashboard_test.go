package guided

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func TestAssessmentDashboardTracksQueuedWorkersAndDependencies(t *testing.T) {
	d := newAssessmentDashboard()
	d.apply(assessment.Event{TaskID: "discover", Kind: "task_queued", Goal: "discover services", DependsOn: nil})
	d.apply(assessment.Event{TaskID: "validate", Kind: "task_queued", Goal: "validate lead", DependsOn: []string{"discover"}})
	lines := d.apply(assessment.Event{TaskID: "validate", Kind: "execution_started", Message: "running bounded validation", Step: 1, EvidenceCount: 1, RemainingBudget: "5 steps"})
	if len(lines) != 1 || !strings.Contains(lines[0], "validate") || !strings.Contains(lines[0], "executing approved action") || !strings.Contains(lines[0], "budget 5 steps") || !strings.Contains(lines[0], "depends discover") {
		t.Fatalf("unexpected dashboard event: %#v", lines)
	}
	if got := len(d.tasks); got != 2 {
		t.Fatalf("task count = %d, want 2", got)
	}
}

func TestAssessmentDashboardKeepsTerminalStatesVisible(t *testing.T) {
	d := newAssessmentDashboard()
	d.apply(assessment.Event{TaskID: "control", Kind: "task_queued", Goal: "control check"})
	d.apply(assessment.Event{TaskID: "control", Kind: "task_started"})
	lines := d.apply(assessment.Event{TaskID: "control", Kind: "task_completed", Message: "evidence recorded", EvidenceCount: 2})
	if len(lines) != 1 || !strings.Contains(lines[0], "done") || !strings.Contains(lines[0], "evidence 2") {
		t.Fatalf("unexpected terminal dashboard event: %#v", lines)
	}
}

func TestDashboardTextCompactsMultilineMessages(t *testing.T) {
	if got := dashboardText("one\ntwo\nthree", 50); got != "one two three" {
		t.Fatalf("dashboard text = %q", got)
	}
	if got := dashboardText("123456789", 6); got != "123..." {
		t.Fatalf("dashboard truncation = %q", got)
	}
}
