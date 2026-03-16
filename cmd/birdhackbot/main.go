package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/buildinfo"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextinspect"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/interactivecli"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/reporoot"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
)

func main() {
	version := flag.Bool("version", false, "print version")
	goal := flag.String("goal", "", "session goal")
	resume := flag.Bool("resume", false, "resume the default rebuild worker session state")
	reporting := flag.String("reporting", "owasp", "reporting requirement")
	contextPacket := flag.Bool("context-packet", false, "print minimal worker context packet")
	inspectContext := flag.Bool("inspect-context", false, "write turn-by-turn context snapshots to the default rebuild inspection directory")
	runCommand := flag.String("run-command", "", "exact command to execute")
	runShell := flag.Bool("run-shell", false, "execute run-command through /bin/sh -lc")
	llmBaseURL := flag.String("llm-base-url", "", "OpenAI-compatible base URL without trailing /chat/completions")
	llmModel := flag.String("llm-model", "", "LLM model id")
	maxSteps := flag.Int("max-steps", 3, "maximum bounded worker-loop steps")
	allowAll := flag.Bool("allow-all", false, "allow execution without per-step approval")
	flag.Parse()

	if *version {
		fmt.Println(buildinfo.Version)
		return
	}

	if *resume && strings.TrimSpace(*goal) != "" {
		fmt.Fprintln(os.Stderr, "birdhackbot rebuild: use either --goal or --resume, not both")
		os.Exit(2)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "birdhackbot rebuild: resolve repo root: %v\n", err)
		os.Exit(2)
	}

	if *resume {
		if err := runResumedSession(repoRoot, *inspectContext); err != nil {
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: resume failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if strings.TrimSpace(*goal) == "" && strings.TrimSpace(*llmBaseURL) != "" && strings.TrimSpace(*llmModel) != "" {
		loop := workerloop.Loop{
			LLM: llmclient.Client{
				BaseURL: *llmBaseURL,
				Model:   *llmModel,
			},
			Executor: execx.Executor{
				LogDir: filepath.Join(repoRoot, "sessions", "rebuild-dev", "logs"),
			},
			Approver:  newApprover(*allowAll),
			Inspector: newInspector(repoRoot, *inspectContext),
		}
		shell := interactivecli.Shell{
			Reader:    os.Stdin,
			Writer:    os.Stdout,
			Runner:    loop,
			RepoRoot:  repoRoot,
			BaseURL:   *llmBaseURL,
			Model:     *llmModel,
			MaxSteps:  *maxSteps,
			AllowAll:  *allowAll,
			StatePath: defaultSessionStatePath(repoRoot),
		}
		if err := shell.Run(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: interactive shell failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *goal != "" {
		foundation, err := session.NewFoundation(session.Input{
			Goal:                 *goal,
			ReportingRequirement: *reporting,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: invalid session foundation: %v\n", err)
			os.Exit(2)
		}
		frame, err := behavior.Load(repoRoot, "worker", map[string]string{
			"approval_mode": approvalModeLabel(*allowAll),
			"surface":       "interactive_worker_cli",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: load behavior frame: %v\n", err)
			os.Exit(2)
		}
		if *contextPacket {
			packet := ctxpacket.WorkerPacket{
				BehaviorFrame:     frame,
				SessionFoundation: foundation,
				CurrentStep: ctxpacket.Step{
					Objective:       foundation.Goal,
					RemainingBudget: "unbounded",
				},
				TaskRuntime: ctxpacket.InitialTaskRuntime(foundation.Goal),
				PlanState: ctxpacket.PlanState{
					Steps:      []string{"understand goal", "work the named target/task", "finish with a clear result"},
					ActiveStep: foundation.Goal,
				},
				RecentConversation: []string{"User: " + foundation.Goal},
				RunningSummary:     "Session foundation created; execution loop not implemented yet.",
				OperatorState: ctxpacket.OperatorState{
					ScopeState:    "from_session_foundation",
					ApprovalState: "pending",
					Model:         "(unset)",
					ContextUsage:  "(unset)",
				},
			}
			fmt.Println(packet.Render())
			return
		}

		if *runCommand != "" {
			executor := execx.Executor{
				LogDir: filepath.Join(repoRoot, "sessions", "rebuild-dev", "logs"),
			}
			result, err := executor.Run(context.Background(), execx.Action{
				Command:  *runCommand,
				Cwd:      ".",
				UseShell: *runShell,
			})
			fmt.Printf("action=%s\nmode=%s\nexit_status=%d\nlog=%s\nstdout_summary=%s\nstderr_summary=%s\n",
				result.Action, result.ExecutionMode, result.ExitStatus, result.LogPath, result.StdoutSummary, result.StderrSummary)
			if err != nil {
				fmt.Fprintf(os.Stderr, "birdhackbot rebuild: command execution failed: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if strings.TrimSpace(*llmBaseURL) != "" && strings.TrimSpace(*llmModel) != "" {
			packet := ctxpacket.WorkerPacket{
				BehaviorFrame:     frame,
				SessionFoundation: foundation,
				CurrentStep: ctxpacket.Step{
					Objective:        foundation.Goal,
					DoneCondition:    "the stated user goal has been satisfied with evidence",
					FailCondition:    "cannot make honest progress on the stated user goal",
					ExpectedEvidence: []string{"command logs", "artifacts if produced"},
					RemainingBudget:  fmt.Sprintf("%d steps", *maxSteps),
				},
				TaskRuntime: ctxpacket.InitialTaskRuntime(foundation.Goal),
				PlanState: ctxpacket.PlanState{
					Steps:      []string{"understand goal", "work the named target/task", "verify and finish"},
					ActiveStep: foundation.Goal,
				},
				RecentConversation: []string{"User: " + foundation.Goal},
				RunningSummary:     "Worker loop starting from the stated user goal.",
				OperatorState: ctxpacket.OperatorState{
					ScopeState:    "from_session_foundation",
					ApprovalState: approvalState(*allowAll),
					Model:         *llmModel,
					ContextUsage:  "(unset)",
				},
			}
			statePath := defaultSessionStatePath(repoRoot)
			if err := sessionstate.Save(statePath, sessionstate.State{
				Status:   "active",
				BaseURL:  *llmBaseURL,
				Model:    *llmModel,
				MaxSteps: *maxSteps,
				AllowAll: *allowAll,
				Packet:   packet,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "birdhackbot rebuild: save session state: %v\n", err)
				os.Exit(2)
			}

			loop := workerloop.Loop{
				LLM: llmclient.Client{
					BaseURL: *llmBaseURL,
					Model:   *llmModel,
				},
				Executor: execx.Executor{
					LogDir: filepath.Join(repoRoot, "sessions", "rebuild-dev", "logs"),
				},
				Approver:  newApprover(*allowAll),
				Inspector: newInspector(repoRoot, *inspectContext),
			}
			outcome, err := loop.Run(context.Background(), packet, *maxSteps)
			if err != nil {
				_ = sessionstate.Save(statePath, sessionstate.State{
					Status:    "stopped",
					BaseURL:   *llmBaseURL,
					Model:     *llmModel,
					MaxSteps:  *maxSteps,
					AllowAll:  *allowAll,
					Packet:    outcome.Packet,
					LastError: err.Error(),
				})
				fmt.Fprintf(os.Stderr, "birdhackbot rebuild: worker loop failed: %v\n", err)
				os.Exit(1)
			}
			if err := sessionstate.Save(statePath, sessionstate.State{
				Status:   "completed",
				BaseURL:  *llmBaseURL,
				Model:    *llmModel,
				MaxSteps: *maxSteps,
				AllowAll: *allowAll,
				Packet:   outcome.Packet,
				Summary:  outcome.Summary,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "birdhackbot rebuild: save completed session state: %v\n", err)
				os.Exit(2)
			}
			fmt.Printf("worker summary: %s\n", outcome.Summary)
			fmt.Printf("session state: %s\n", statePath)
			return
		}

		fmt.Fprintf(os.Stderr, "birdhackbot rebuild: session foundation ready (goal=%q, reporting=%s)\n",
			foundation.Goal, foundation.ReportingRequirement)
		return
	}

	fmt.Fprintln(os.Stderr, "birdhackbot rebuild: worker loop not implemented yet")
}

func approvalState(allowAll bool) string {
	if allowAll {
		return "approved_session"
	}
	return "pending"
}

func approvalModeLabel(allowAll bool) string {
	if allowAll {
		return "session_allow_all"
	}
	return "approve_per_execution"
}

func newApprover(allowAll bool) approval.Approver {
	if allowAll {
		return approval.StaticApprover{Decision: approval.DecisionApproveSession}
	}
	return &approval.PromptApprover{
		Reader: os.Stdin,
		Writer: os.Stdout,
	}
}

func newInspector(repoRoot string, enabled bool) workerloop.Inspector {
	if !enabled {
		return nil
	}
	return contextinspect.Recorder{
		Dir: filepath.Join(repoRoot, "sessions", "rebuild-dev", "context"),
	}
}

func defaultSessionStatePath(repoRoot string) string {
	return filepath.Join(repoRoot, "sessions", "rebuild-dev", "session.json")
}

func runResumedSession(repoRoot string, inspectContext bool) error {
	statePath := defaultSessionStatePath(repoRoot)
	state, err := sessionstate.Load(statePath)
	if err != nil {
		return err
	}
	if state.Status == "completed" {
		fmt.Printf("worker summary: %s\n", strings.TrimSpace(state.Summary))
		fmt.Printf("session state: %s\n", statePath)
		return nil
	}
	if strings.TrimSpace(state.BaseURL) == "" || strings.TrimSpace(state.Model) == "" {
		return fmt.Errorf("saved state is missing llm connection details")
	}

	loop := workerloop.Loop{
		LLM: llmclient.Client{
			BaseURL: state.BaseURL,
			Model:   state.Model,
		},
		Executor: execx.Executor{
			LogDir: filepath.Join(repoRoot, "sessions", "rebuild-dev", "logs"),
		},
		Approver:  newApprover(state.AllowAll),
		Inspector: newInspector(repoRoot, inspectContext),
	}
	outcome, err := loop.Run(context.Background(), state.Packet, state.MaxSteps)
	if err != nil {
		_ = sessionstate.Save(statePath, sessionstate.State{
			Status:    "stopped",
			BaseURL:   state.BaseURL,
			Model:     state.Model,
			MaxSteps:  state.MaxSteps,
			AllowAll:  state.AllowAll,
			Packet:    outcome.Packet,
			LastError: err.Error(),
		})
		return err
	}
	if err := sessionstate.Save(statePath, sessionstate.State{
		Status:   "completed",
		BaseURL:  state.BaseURL,
		Model:    state.Model,
		MaxSteps: state.MaxSteps,
		AllowAll: state.AllowAll,
		Packet:   outcome.Packet,
		Summary:  outcome.Summary,
	}); err != nil {
		return err
	}
	fmt.Printf("worker summary: %s\n", outcome.Summary)
	fmt.Printf("session state: %s\n", statePath)
	return nil
}

func resolveRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return reporoot.Find(cwd)
}
