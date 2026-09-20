package guided

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

type dashboardTask struct {
	id         string
	goal       string
	doneWhen   string
	dependsOn  []string
	status     string
	phase      string
	detail     string
	step       int
	evidence   int
	budget     string
	contextUse string
}

// assessmentDashboard is a presentation-only projection of coordinator
// events. It deliberately does not make decisions or duplicate runtime state.
type assessmentDashboard struct {
	tasks map[string]*dashboardTask
}

func newAssessmentDashboard() assessmentDashboard {
	return assessmentDashboard{tasks: make(map[string]*dashboardTask)}
}

func (d *assessmentDashboard) apply(e assessment.Event) []string {
	if e.TaskID == "" {
		if e.Kind == "operator_message" {
			return []string{"Coordinator received operator message: " + dashboardText(e.Message, 180)}
		}
		if e.Kind == "planning" {
			return []string{"\nCoordinator: " + dashboardText(e.Message, 180)}
		}
		if e.Kind == "plan" {
			return []string{"Coordinator plan: " + dashboardText(e.Message, 180)}
		}
		return nil
	}

	task := d.tasks[e.TaskID]
	if task == nil {
		task = &dashboardTask{id: e.TaskID}
		d.tasks[e.TaskID] = task
	}
	if e.Goal != "" {
		task.goal = e.Goal
	}
	if e.DoneWhen != "" {
		task.doneWhen = e.DoneWhen
	}
	if e.DependsOn != nil {
		task.dependsOn = append([]string(nil), e.DependsOn...)
	}
	if e.Step > 0 {
		task.step = e.Step
	}
	if e.EvidenceCount >= 0 {
		task.evidence = e.EvidenceCount
	}
	if e.RemainingBudget != "" {
		task.budget = e.RemainingBudget
	}
	if e.ContextUsage != "" && e.ContextUsage != "(unset)" {
		task.contextUse = e.ContextUsage
	}
	if e.Message != "" {
		task.detail = e.Message
	}

	switch e.Kind {
	case "task_queued":
		task.status, task.phase = "queued", "waiting to start"
		return d.snapshot()
	case "task_started":
		task.status, task.phase = "running", "starting"
	case "decision_started":
		task.status, task.phase = "running", "deciding next step"
	case "plan_finished":
		task.status, task.phase = "running", "updating plan"
	case "action_proposed":
		task.status, task.phase = "running", "action proposed"
	case "approval_required":
		task.status, task.phase = "waiting", "awaiting operator approval"
	case "execution_started":
		task.status, task.phase = "running", "executing approved action"
	case "execution_finished":
		task.status, task.phase = "running", "reviewing command result"
	case "post_exec_eval_started":
		task.status, task.phase = "running", "evaluating evidence"
	case "post_exec_eval_finished":
		task.status, task.phase = "running", "evaluation returned"
	case "user_question", "waiting_user":
		task.status, task.phase = "waiting", "needs operator input"
	case "user_answered":
		task.status, task.phase = "running", "continuing after operator input"
	case "task_completed", "completed", "done":
		task.status, task.phase = "done", "completed"
	case "task_blocked", "blocked":
		task.status, task.phase = "blocked", "blocked"
	case "task_failed", "failed":
		task.status, task.phase = "failed", "failed"
	case "aborted":
		task.status, task.phase = "aborted", "stopped"
	default:
		if task.status == "" {
			task.status = "running"
		}
		task.phase = e.Kind
	}

	return []string{d.eventLine(task)}
}

func (d *assessmentDashboard) snapshot() []string {
	ids := make([]string, 0, len(d.tasks))
	for id := range d.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	lines := []string{"\nWorkers"}
	for _, id := range ids {
		lines = append(lines, d.eventLine(d.tasks[id]))
	}
	return lines
}

func (d *assessmentDashboard) eventLine(task *dashboardTask) string {
	depends := "-"
	if len(task.dependsOn) > 0 {
		depends = strings.Join(task.dependsOn, ",")
	}
	detail := dashboardText(task.detail, 120)
	if detail == "" {
		detail = task.goal
	}
	step := "-"
	if task.step > 0 {
		step = fmt.Sprintf("%d", task.step)
	}
	budget := task.budget
	if budget == "" {
		budget = "-"
	}
	contextUse := ""
	if task.contextUse != "" {
		contextUse = " context " + dashboardText(task.contextUse, 12)
	}
	return fmt.Sprintf("  %-12s %-8s %-25s step %-3s budget %-9s evidence %-2d depends %-12s%s %s", task.id, task.status, task.phase, step, dashboardText(budget, 9), task.evidence, dashboardText(depends, 12), contextUse, detail)
}

func dashboardText(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}
