package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/plan"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func (r *Runner) handlePlan(args []string) error {
	r.setTask("plan")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: plan updates not permitted")
	}

	if len(args) == 0 {
		return r.handlePlanWizardStart()
	}
	if len(args) > 0 {
		mode := strings.ToLower(args[0])
		if mode == "start" {
			return r.handlePlanWizardStart()
		}
		if mode == "done" || mode == "finish" {
			return r.handlePlanWizardDone()
		}
		if mode == "cancel" || mode == "exit" {
			return r.handlePlanWizardCancel()
		}
		if mode == "auto" || mode == "llm" {
			return r.handlePlanAuto(strings.Join(args[1:], " "))
		}
	}
	return r.handlePlanManual(args)
}

func (r *Runner) handlePlanManual(args []string) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}

	planText := strings.TrimSpace(strings.Join(args, " "))
	if planText == "" {
		r.logger.Printf("Enter plan text. End with a single '.' line.")
		lines := []string{}
		for {
			line, err := r.readLine("> ")
			if err != nil && err != io.EOF {
				return err
			}
			if strings.TrimSpace(line) == "." {
				break
			}
			lines = append(lines, line)
			if err == io.EOF {
				break
			}
		}
		planText = strings.TrimSpace(strings.Join(lines, "\n"))
	}

	if planText == "" {
		return fmt.Errorf("plan text is empty")
	}
	planPath, err := session.AppendPlan(sessionDir, r.cfg.Session.PlanFilename, planText)
	if err != nil {
		return err
	}
	r.logger.Printf("Plan updated: %s", planPath)
	return nil
}

func (r *Runner) handlePlanAuto(reason string) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	input, err := r.planInput(sessionDir)
	if err != nil {
		return err
	}
	if reason != "" {
		input.Goal = reason
	}
	input.Playbooks = r.playbookHints(reason)
	planner := r.planGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("plan")
	content, err := planner.Plan(ctx, input)
	stopIndicator()
	if err != nil {
		return err
	}
	if reason != "" {
		content = fmt.Sprintf("### Auto Plan (%s)\n\n%s", reason, content)
	}
	planPath, err := session.AppendPlan(sessionDir, r.cfg.Session.PlanFilename, content)
	if err != nil {
		return err
	}
	r.logger.Printf("Auto plan written: %s", planPath)
	return nil
}

func (r *Runner) handleNext(_ []string) error {
	r.setTask("next")
	defer r.clearTask()

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	input, err := r.planInput(sessionDir)
	if err != nil {
		return err
	}
	input.Playbooks = r.playbookHints(input.Plan)
	planner := r.planGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("next")
	steps, err := planner.Next(ctx, input)
	stopIndicator()
	if err != nil {
		return err
	}
	r.logger.Printf("Next steps:")
	for _, step := range steps {
		r.logger.Printf("- %s", step)
	}
	return nil
}

func (r *Runner) handleExecute(args []string) error {
	r.setTask("execute")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: execute not permitted")
	}

	mode := ""
	if len(args) > 0 {
		mode = strings.ToLower(args[0])
	}

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}

	if mode == "auto" || mode == "plan" {
		if err := r.handlePlanAuto("execute"); err != nil {
			return err
		}
	}

	input, err := r.planInput(sessionDir)
	if err != nil {
		return err
	}
	input.Playbooks = r.playbookHints(input.Plan)
	planner := r.planGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("next")
	steps, err := planner.Next(ctx, input)
	stopIndicator()
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		return fmt.Errorf("no next steps available")
	}

	stepIndex := 0
	if len(args) > 1 {
		if value, err := strconv.Atoi(args[1]); err == nil && value > 0 && value <= len(steps) {
			stepIndex = value - 1
		}
	}
	step := steps[stepIndex]
	r.logger.Printf("Executing step: %s", step)
	return r.handleAssistGoal(step, false)
}

func (r *Runner) planInput(sessionDir string) (plan.Input, error) {
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return plan.Input{}, err
	}
	summaryText := readFileTrimmed(artifacts.SummaryPath)
	facts, _ := memory.ReadBullets(artifacts.FactsPath)
	inventory := readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.InventoryFilename))
	planText := readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.PlanFilename))

	return plan.Input{
		SessionID:  r.sessionID,
		Scope:      r.cfg.Scope.Networks,
		Targets:    r.cfg.Scope.Targets,
		Summary:    summaryText,
		KnownFacts: facts,
		Inventory:  inventory,
		Plan:       planText,
	}, nil
}

func (r *Runner) planGenerator() plan.Planner {
	llmPlanner := plan.LLMPlanner{Client: llm.NewLMStudioClient(r.cfg), Model: r.cfg.LLM.Model}
	fallback := plan.FallbackPlanner{}
	return guardedPlanner{
		allow:     r.llmAllowed,
		onSuccess: r.recordLLMSuccess,
		onFailure: r.recordLLMFailure,
		primary:   llmPlanner,
		fallback:  fallback,
	}
}
