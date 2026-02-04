package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/plan"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

type planWizard struct {
	step      int
	answers   []string
	questions []string
}

func newPlanWizard() *planWizard {
	return &planWizard{
		answers: []string{},
		questions: []string{
			"What is the primary goal for this assessment?",
			"What targets or scope should be included (IPs/domains/URLs)?",
			"Any constraints or allowed actions (rate limits, no-DoS, etc.)?",
			"What outputs do you want (report format, evidence, logs)?",
		},
	}
}

func (r *Runner) planWizardActive() bool {
	return r.planWizard != nil
}

func (r *Runner) handlePlanWizardStart() error {
	if r.planWizardActive() {
		r.logger.Printf("Planning mode already active. Use /plan done or /plan cancel.")
		return nil
	}
	r.planWizard = newPlanWizard()
	r.setMode("planning")
	r.logger.Printf("Planning mode started. Answer the prompts; use /plan done to finish.")
	r.logger.Printf(r.planWizard.questions[0])
	return nil
}

func (r *Runner) handlePlanWizardCancel() error {
	if !r.planWizardActive() {
		return nil
	}
	r.planWizard = nil
	r.clearMode()
	r.logger.Printf("Planning mode canceled.")
	return nil
}

func (r *Runner) handlePlanWizardDone() error {
	if !r.planWizardActive() {
		return nil
	}
	return r.finalizePlanWizard()
}

func (r *Runner) handlePlanWizardInput(line string) error {
	if !r.planWizardActive() {
		return nil
	}
	answer := strings.TrimSpace(line)
	r.planWizard.answers = append(r.planWizard.answers, answer)
	r.planWizard.step++
	if r.planWizard.step >= len(r.planWizard.questions) {
		return r.finalizePlanWizard()
	}
	r.logger.Printf(r.planWizard.questions[r.planWizard.step])
	return nil
}

func (r *Runner) finalizePlanWizard() error {
	defer func() {
		r.planWizard = nil
		r.clearMode()
	}()
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	goal := wizardAnswer(r.planWizard, 0)
	scope := wizardAnswer(r.planWizard, 1)
	constraints := wizardAnswer(r.planWizard, 2)
	outputs := wizardAnswer(r.planWizard, 3)

	input, err := r.planInput(sessionDir)
	if err != nil {
		return err
	}
	input.Goal = strings.TrimSpace(strings.Join([]string{goal, scope, constraints, outputs}, " "))
	input.Playbooks = r.playbookHints(input.Goal)
	input.Plan = strings.TrimSpace(readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.PlanFilename)))

	builder := strings.Builder{}
	builder.WriteString("### Planning Inputs\n")
	if goal != "" {
		builder.WriteString(fmt.Sprintf("- Goal: %s\n", goal))
	}
	if scope != "" {
		builder.WriteString(fmt.Sprintf("- Scope/Targets: %s\n", scope))
	}
	if constraints != "" {
		builder.WriteString(fmt.Sprintf("- Constraints: %s\n", constraints))
	}
	if outputs != "" {
		builder.WriteString(fmt.Sprintf("- Outputs: %s\n", outputs))
	}
	builder.WriteString("\n")

	content := ""
	if r.llmAllowed() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		planner := r.planGenerator()
		planContent, err := planner.Plan(ctx, input)
		if err == nil {
			content = planContent
		}
	}
	if content == "" {
		planContent, _ := (plan.FallbackPlanner{}).Plan(context.Background(), input)
		content = planContent
	}

	finalPlan := builder.String() + content
	planPath, err := session.AppendPlan(sessionDir, r.cfg.Session.PlanFilename, finalPlan)
	if err != nil {
		return err
	}
	r.logger.Printf("Plan updated: %s", planPath)
	fmt.Println(finalPlan)
	return nil
}

func wizardAnswer(w *planWizard, index int) string {
	if w == nil || index < 0 || index >= len(w.answers) {
		return ""
	}
	return w.answers[index]
}
