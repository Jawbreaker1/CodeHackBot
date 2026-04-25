package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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

type SessionPaths struct {
	Root       string
	LogsDir    string
	ContextDir string
	StatePath  string
}

func main() {
	version := flag.Bool("version", false, "print version")
	goal := flag.String("goal", "", "session goal")
	resume := flag.Bool("resume", false, "resume a saved rebuild worker session state")
	sessionDir := flag.String("session-dir", "", "session directory for state, logs, and context snapshots")
	sessionsDir := flag.String("sessions-dir", "", "root directory used for newly created session directories")
	reporting := flag.String("reporting", "owasp", "reporting requirement")
	contextPacket := flag.Bool("context-packet", false, "print minimal worker context packet")
	inspectContext := flag.Bool("inspect-context", false, "write turn-by-turn context snapshots to the active session directory")
	debugRunCommand := flag.String("debug-run-command", "", "development/debugging only: execute an exact command outside the worker loop")
	debugRunShell := flag.Bool("debug-run-shell", false, "execute debug-run-command through /bin/sh -lc")
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
	if *resume && strings.TrimSpace(*sessionDir) == "" {
		fmt.Fprintln(os.Stderr, "birdhackbot rebuild: use --resume with --session-dir")
		os.Exit(2)
	}
	if strings.TrimSpace(*debugRunCommand) != "" && strings.TrimSpace(*goal) != "" {
		fmt.Fprintln(os.Stderr, "birdhackbot rebuild: use either --goal or --debug-run-command, not both")
		os.Exit(2)
	}
	if strings.TrimSpace(*debugRunCommand) != "" && *resume {
		fmt.Fprintln(os.Stderr, "birdhackbot rebuild: use either --resume or --debug-run-command, not both")
		os.Exit(2)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "birdhackbot rebuild: resolve repo root: %v\n", err)
		os.Exit(2)
	}

	if *resume {
		runCtx, stop := signalAwareContext()
		defer stop()
		paths, err := existingSessionPaths(*sessionDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: invalid session dir: %v\n", err)
			os.Exit(2)
		}
		if err := runResumedSession(runCtx, paths, *inspectContext); err != nil {
			if isAborted(runCtx, err) {
				fmt.Fprintln(os.Stderr, "birdhackbot rebuild: aborted by signal")
				os.Exit(130)
			}
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: resume failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if strings.TrimSpace(*debugRunCommand) != "" {
		runCtx, stop := signalAwareContext()
		defer stop()
		paths, err := allocateSessionPaths(repoRoot, *sessionsDir, *sessionDir, "debug")
		if err != nil {
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: prepare debug session: %v\n", err)
			os.Exit(2)
		}
		if err := runDebugCommand(runCtx, paths, *debugRunCommand, *debugRunShell); err != nil {
			if isAborted(runCtx, err) {
				fmt.Fprintln(os.Stderr, "birdhackbot rebuild: aborted by signal")
				os.Exit(130)
			}
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: debug command execution failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if strings.TrimSpace(*goal) == "" && strings.TrimSpace(*llmBaseURL) != "" && strings.TrimSpace(*llmModel) != "" {
		paths, err := allocateSessionPaths(repoRoot, *sessionsDir, *sessionDir, "interactive")
		if err != nil {
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: prepare interactive session: %v\n", err)
			os.Exit(2)
		}
		loop := workerloop.Loop{
			LLM: llmclient.Client{
				BaseURL: *llmBaseURL,
				Model:   *llmModel,
			},
			Executor: execx.Executor{
				LogDir: paths.LogsDir,
			},
			Approver:  newApprover(*allowAll),
			Inspector: newInspector(paths.ContextDir, *inspectContext),
		}
		shell := interactivecli.Shell{
			Reader:    os.Stdin,
			Writer:    os.Stdout,
			Runner:    &loop,
			RepoRoot:  repoRoot,
			BaseURL:   *llmBaseURL,
			Model:     *llmModel,
			MaxSteps:  *maxSteps,
			AllowAll:  *allowAll,
			StatePath: paths.StatePath,
		}
		runCtx, stop := signalAwareContext()
		defer stop()
		fmt.Printf("session dir: %s\n", paths.Root)
		if err := shell.Run(runCtx); err != nil {
			if isAborted(runCtx, err) {
				fmt.Fprintln(os.Stderr, "birdhackbot rebuild: aborted by signal")
				os.Exit(130)
			}
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: interactive shell failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *goal != "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "birdhackbot rebuild: resolve working directory: %v\n", err)
			os.Exit(2)
		}
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
			packet := ctxpacket.NewInitialWorkerPacket(frame, foundation, cwd, "(unset)", "pending", 1)
			packet.CurrentStep.RemainingBudget = "unbounded"
			packet.PlanState.Steps = []string{"understand goal", "work the named target/task", "finish with a clear result"}
			packet.RunningSummary = "Session foundation created; worker loop ready to run with LLM configuration."
			fmt.Println(packet.Render())
			return
		}

		if strings.TrimSpace(*llmBaseURL) != "" && strings.TrimSpace(*llmModel) != "" {
			runCtx, stop := signalAwareContext()
			defer stop()
			paths, err := allocateSessionPaths(repoRoot, *sessionsDir, *sessionDir, "run")
			if err != nil {
				fmt.Fprintf(os.Stderr, "birdhackbot rebuild: prepare session: %v\n", err)
				os.Exit(2)
			}
			packet := ctxpacket.NewInitialWorkerPacket(frame, foundation, cwd, *llmModel, approvalState(*allowAll), *maxSteps)
			statePath := paths.StatePath
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
					LogDir: paths.LogsDir,
				},
				Approver:  newApprover(*allowAll),
				Inspector: newInspector(paths.ContextDir, *inspectContext),
			}
			outcome, err := loop.Run(runCtx, packet, *maxSteps)
			if err != nil {
				lastError := terminalError(runCtx, err)
				_ = sessionstate.Save(statePath, sessionstate.State{
					Status:    "stopped",
					BaseURL:   *llmBaseURL,
					Model:     *llmModel,
					MaxSteps:  *maxSteps,
					AllowAll:  *allowAll,
					Packet:    outcome.Packet,
					LastError: lastError,
				})
				if isAborted(runCtx, err) {
					fmt.Fprintln(os.Stderr, "birdhackbot rebuild: aborted by signal")
					os.Exit(130)
				}
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
			fmt.Printf("session dir: %s\n", paths.Root)
			fmt.Printf("session state: %s\n", statePath)
			return
		}

		fmt.Fprintf(os.Stderr, "birdhackbot rebuild: session foundation ready (goal=%q, reporting=%s)\n",
			foundation.Goal, foundation.ReportingRequirement)
		return
	}

	fmt.Fprintln(os.Stderr, "birdhackbot rebuild: provide --goal, or use --resume, --debug-run-command, or interactive LLM shell flags")
}

func approvalState(allowAll bool) string {
	if allowAll {
		return "approved_session"
	}
	return "pending"
}

func runDebugCommand(ctx context.Context, paths SessionPaths, command string, useShell bool) error {
	executor := execx.Executor{
		LogDir: paths.LogsDir,
	}
	result, err := executor.Run(ctx, execx.Action{
		Command:  command,
		Cwd:      ".",
		UseShell: useShell,
	})
	fmt.Fprintln(os.Stderr, "birdhackbot rebuild: running dev/debug exact command outside the worker loop")
	fmt.Printf("session dir: %s\n", paths.Root)
	fmt.Printf("action=%s\nmode=%s\nexit_status=%d\nlog=%s\nstdout_summary=%s\nstderr_summary=%s\n",
		result.Action, result.ExecutionMode, result.ExitStatus, result.LogPath, result.StdoutSummary, result.StderrSummary)
	return err
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

func newInspector(contextDir string, enabled bool) workerloop.Inspector {
	if !enabled {
		return nil
	}
	return contextinspect.Recorder{Dir: contextDir}
}

func existingSessionPaths(sessionDir string) (SessionPaths, error) {
	root, err := normalizePath(sessionDir)
	if err != nil {
		return SessionPaths{}, err
	}
	return sessionPathsForRoot(root), nil
}

func allocateSessionPaths(repoRoot, sessionsRootFlag, sessionDirFlag, prefix string) (SessionPaths, error) {
	if strings.TrimSpace(sessionDirFlag) != "" {
		root, err := normalizePath(sessionDirFlag)
		if err != nil {
			return SessionPaths{}, err
		}
		paths := sessionPathsForRoot(root)
		return paths, ensureSessionPaths(paths)
	}
	sessionsRoot, err := normalizeSessionsRoot(repoRoot, sessionsRootFlag)
	if err != nil {
		return SessionPaths{}, err
	}
	paths := sessionPathsForRoot(filepath.Join(sessionsRoot, sessionDirName(prefix)))
	return paths, ensureSessionPaths(paths)
}

func normalizeSessionsRoot(repoRoot, override string) (string, error) {
	if strings.TrimSpace(override) == "" {
		return filepath.Join(repoRoot, "sessions"), nil
	}
	return normalizePath(override)
}

func normalizePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func sessionPathsForRoot(root string) SessionPaths {
	return SessionPaths{
		Root:       root,
		LogsDir:    filepath.Join(root, "logs"),
		ContextDir: filepath.Join(root, "context"),
		StatePath:  filepath.Join(root, "session.json"),
	}
}

func sessionDirName(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "run"
	}
	return fmt.Sprintf("%s-%s", prefix, time.Now().UTC().Format("20060102-150405.000000000"))
}

func ensureSessionPaths(paths SessionPaths) error {
	for _, dir := range []string{paths.Root, paths.LogsDir, paths.ContextDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func runResumedSession(ctx context.Context, paths SessionPaths, inspectContext bool) error {
	statePath := paths.StatePath
	state, err := sessionstate.Load(statePath)
	if err != nil {
		return err
	}
	if state.Status == "completed" {
		fmt.Printf("worker summary: %s\n", strings.TrimSpace(state.Summary))
		fmt.Printf("session dir: %s\n", paths.Root)
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
			LogDir: paths.LogsDir,
		},
		Approver:  newApprover(state.AllowAll),
		Inspector: newInspector(paths.ContextDir, inspectContext),
	}
	outcome, err := loop.Run(ctx, state.Packet, state.MaxSteps)
	if err != nil {
		lastError := terminalError(ctx, err)
		_ = sessionstate.Save(statePath, sessionstate.State{
			Status:    "stopped",
			BaseURL:   state.BaseURL,
			Model:     state.Model,
			MaxSteps:  state.MaxSteps,
			AllowAll:  state.AllowAll,
			Packet:    outcome.Packet,
			LastError: lastError,
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
	fmt.Printf("session dir: %s\n", paths.Root)
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

func signalAwareContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func isAborted(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func terminalError(ctx context.Context, err error) string {
	if isAborted(ctx, err) {
		return "aborted by signal"
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
