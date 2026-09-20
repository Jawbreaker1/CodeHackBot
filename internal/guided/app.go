package guided

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
)

type App struct {
	RepoRoot string
	Reader   io.Reader
	Writer   io.Writer
}

func (a App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := NewConsole(ctx, a.Reader, a.Writer)
	c.Print("BirdHackBot — guided assessment (lab preview)\n\nCtrl-C stops setup or broadcasts stop to every active worker.\n")
	prefs, err := configureProvider(ctx, c, filepath.Join(a.RepoRoot, ".birdhackbot", "preferences.json"))
	if err != nil {
		return err
	}
	client, stop, err := startProvider(ctx, prefs)
	if err != nil {
		return fmt.Errorf("model access unavailable: %w; restart BirdHackBot after correcting sign-in or choose another provider", err)
	}
	defer stop()
	goal, err := required(ctx, c, "What should this assessment establish?")
	if err != nil {
		return err
	}
	scope, err := required(ctx, c, "What is explicitly in scope? Include exact targets or absolute file paths, allowed actions, and exclusions.")
	if err != nil {
		return err
	}
	limits := assessment.DefaultLimits()
	c.Print("\nAssessment review\nGoal: %s\nDeclared scope: %s\nProvider: %s / %s\nPermissions: approve each action\nLimits: up to %d workers, %d tasks, %d model calls\n", goal, scope, prefs.Provider, prefs.Model, limits.Workers, limits.Tasks, limits.ModelCalls)
	c.Print("Requested reasoning: %s\n", reasoningLabel(prefs))
	if prefs.Provider == "local" {
		c.Print("Local response budget: %d output tokens, up to 10 minutes per request. Ctrl-C remains available.\n", prefs.MaxOutputTokens)
	}
	if prefs.Provider == "subscription" {
		c.Print("Selected task context and evidence will be sent to OpenAI (input ceiling: %d bytes).\n", prefs.MaxInputBytes)
	}
	c.Print("This lab preview does not enforce a network allowlist or filesystem sandbox. Use only your authorized isolated lab. Reports are drafts for review.\n")
	answer, err := c.Ask(ctx, "Type start to confirm this is an authorized isolated lab and begin, or press Enter to cancel")
	if err != nil {
		return err
	}
	if strings.ToLower(answer) != "start" {
		c.Print("Assessment canceled before execution.\n")
		return nil
	}
	frame, err := behavior.Load(a.RepoRoot, "assessment_coordinator", map[string]string{"approval_mode": "per_action", "scope": scope})
	if err != nil {
		return err
	}
	base := filepath.Join(a.RepoRoot, "sessions")
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	root, err := os.MkdirTemp(base, "assessment-"+time.Now().UTC().Format("20060102-150405")+"-")
	if err != nil {
		return err
	}
	c.Print("\nAssessment directory: %s\n", root)
	runner := assessment.Coordinator{LLM: client, Frame: frame, Limits: limits, Emit: c.Progress, Approver: func(task assessment.Task) approval.Approver {
		return taskApprover{console: c, task: task, scope: scope}
	}}
	runner.AskUser = func(ctx context.Context, task assessment.Task, question string) (string, error) {
		return required(ctx, c, "Question from "+task.ID+": "+question)
	}
	state, runErr := runner.Run(ctx, root, goal, scope)
	c.Print("\nAssessment %s. Model calls: %d.\nEvidence and worker state: %s\n", state.Status, state.Usage.Calls, root)
	if _, err := os.Stat(filepath.Join(root, "report.md")); err == nil {
		c.Print("Report: %s\n", filepath.Join(root, "report.md"))
	}
	if runErr != nil {
		c.Print("Review the saved evidence and any report, then resolve the stated limitation before starting further work.\n")
		return runErr
	}
	if len(state.Plans) > 0 {
		c.Print("\n%s\n", state.Plans[len(state.Plans)-1].Summary)
	}
	return nil
}

func required(ctx context.Context, c *Console, prompt string) (string, error) {
	for {
		text, err := c.Ask(ctx, prompt)
		if err != nil {
			return "", err
		}
		if text != "" {
			return text, nil
		}
		c.Print("Please enter a value, or use Ctrl-C to cancel.\n")
	}
}
