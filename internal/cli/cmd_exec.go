package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
	"github.com/Jawbreaker1/CodeHackBot/internal/msf"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func (r *Runner) handleRun(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /run <command> [args...]")
	}
	runCommand := strings.TrimSpace(args[0])
	runArgs := append([]string{}, args[1:]...)
	if adaptedCmd, adaptedArgs, notes, err := msf.AdaptRuntimeCommand(runCommand, runArgs, r.currentWorkingDir()); err != nil {
		r.logger.Printf("Runtime adaptation failed: %v", err)
	} else {
		if adaptedCmd != runCommand || !equalStringSlices(adaptedArgs, runArgs) {
			runCommand = adaptedCmd
			runArgs = adaptedArgs
		}
		for _, note := range notes {
			if strings.TrimSpace(note) != "" {
				r.logger.Printf("%s", note)
			}
		}
	}
	r.setTask(formatWorkingCommandTask("run", runCommand, runArgs))
	defer r.clearTask()
	if !r.cfg.Tools.Shell.Enabled {
		return fmt.Errorf("shell execution disabled by config")
	}
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: run not permitted")
	}
	start := time.Now()
	requireApproval := r.cfg.Permissions.Level == "default" && r.cfg.Permissions.RequireApproval
	timeout := time.Duration(r.cfg.Tools.Shell.TimeoutSeconds) * time.Second
	resolvedTimeout := resolveShellIdleTimeout(timeout, runCommand, runArgs)
	if resolvedTimeout != timeout && !r.cfg.UI.Verbose {
		r.logger.Printf("Adjusted idle timeout to %s for command: %s", resolvedTimeout, runCommand)
	}
	timeout = resolvedTimeout
	if requireApproval {
		approved, err := r.confirm(fmt.Sprintf("Run command: %s %s?", runCommand, strings.Join(runArgs, " ")))
		if err != nil {
			return err
		}
		if !approved {
			return fmt.Errorf("execution not approved")
		}
	}
	liveWriter := r.liveWriter()
	activityWriter := newActivityWriter(liveWriter)
	stopIndicator := r.startWorkingIndicator(activityWriter)
	defer stopIndicator()
	if activityWriter != nil {
		liveWriter = activityWriter
	}
	runner := exec.Runner{
		Permissions:      exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval:  false,
		LogDir:           filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:          timeout,
		Reader:           r.reader,
		PromptWriter:     r.outputWriter(),
		ScopeNetworks:    r.cfg.Scope.Networks,
		ScopeTargets:     r.cfg.Scope.Targets,
		ScopeDenyTargets: r.cfg.Scope.DenyTargets,
		LiveWriter:       liveWriter,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interruptCh, stopInterrupt, keyErr := startInterruptWatcher()
	if keyErr == nil {
		r.logger.Printf("Press ESC or Ctrl-C to interrupt")
	} else if r.isTTY() {
		r.logger.Printf("Ctrl-C to interrupt (ESC unavailable: %v)", keyErr)
	}
	if interruptCh != nil {
		go func() {
			<-interruptCh
			cancel()
		}()
	}

	result, err := runner.RunCommandWithContext(ctx, runCommand, runArgs...)
	wasCanceled := errors.Is(err, context.Canceled)
	if stopInterrupt != nil {
		stopInterrupt()
	}
	r.ensureTTYLineBreak()
	if result.LogPath != "" {
		r.logger.Printf("Log saved: %s", result.LogPath)
		r.recordActionArtifact(result.LogPath)
	}
	r.recordObservationFromResult("run", result, err)
	r.maybeAutoSummarize(result.LogPath, "run")
	ledgerStatus := "disabled"
	if r.cfg.Session.LedgerEnabled {
		if result.LogPath == "" {
			ledgerStatus = "skipped"
		} else {
			sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
			if ledgerErr := session.AppendLedger(sessionDir, r.cfg.Session.LedgerFilename, strings.Join(append([]string{runCommand}, runArgs...), " "), result.LogPath, ""); ledgerErr != nil {
				r.logger.Printf("Ledger update failed: %v", ledgerErr)
				ledgerStatus = "error"
			} else {
				ledgerStatus = "appended"
			}
		}
	}
	safePrint(renderExecSummary(r.currentTask, runCommand, runArgs, time.Since(start), result.LogPath, ledgerStatus, result.Output, err))
	if err != nil {
		if wasCanceled {
			err = fmt.Errorf("command interrupted")
			r.logger.Printf("Interrupted. What should I do differently?")
			return err
		}
		return commandError{Result: result, Err: err}
	}
	return nil
}

func (r *Runner) handleMSF(args []string) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: msf search not permitted")
	}
	if !r.cfg.Tools.Shell.Enabled {
		return fmt.Errorf("shell execution disabled by config")
	}
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: run not permitted")
	}
	if r.cfg.Tools.Metasploit.DiscoveryMode != "msfconsole" {
		if r.cfg.Tools.Metasploit.RPCEnabled {
			return fmt.Errorf("msfrpcd discovery not implemented; set discovery_mode to msfconsole")
		}
		return fmt.Errorf("metasploit discovery disabled by config")
	}

	query := msf.Query{}
	extra := []string{}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "service="):
			query.Service = strings.TrimPrefix(arg, "service=")
		case strings.HasPrefix(arg, "platform="):
			query.Platform = strings.TrimPrefix(arg, "platform=")
		case strings.HasPrefix(arg, "keyword="):
			query.Keyword = strings.TrimPrefix(arg, "keyword=")
		default:
			extra = append(extra, arg)
		}
	}
	if len(extra) > 0 {
		if query.Keyword == "" {
			query.Keyword = strings.Join(extra, " ")
		} else {
			query.Keyword = query.Keyword + " " + strings.Join(extra, " ")
		}
	}

	search := msf.BuildSearch(query)
	command := msf.BuildCommand(search)

	r.setTask("msf search")
	defer r.clearTask()

	if r.cfg.Permissions.RequireApproval {
		approved, err := r.confirm(fmt.Sprintf("Run msfconsole search: %s?", search))
		if err != nil {
			return err
		}
		if !approved {
			return fmt.Errorf("execution not approved")
		}
	}

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interruptCh, stopInterrupt, keyErr := startInterruptWatcher()
	if keyErr == nil {
		r.logger.Printf("Press ESC or Ctrl-C to interrupt")
	} else if r.isTTY() {
		r.logger.Printf("Ctrl-C to interrupt (ESC unavailable: %v)", keyErr)
	}
	if interruptCh != nil {
		go func() {
			<-interruptCh
			cancel()
		}()
	}

	liveWriter := r.liveWriter()
	activityWriter := newActivityWriter(liveWriter)
	stopIndicator := r.startWorkingIndicator(activityWriter)
	defer stopIndicator()
	if activityWriter != nil {
		liveWriter = activityWriter
	}
	cmdArgs := []string{"-q", "-x", command}
	execRunner := exec.Runner{
		Permissions:      exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval:  false,
		LogDir:           filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:          resolveShellIdleTimeout(2*time.Minute, "msfconsole", cmdArgs),
		Reader:           r.reader,
		PromptWriter:     r.outputWriter(),
		ScopeNetworks:    r.cfg.Scope.Networks,
		ScopeTargets:     r.cfg.Scope.Targets,
		ScopeDenyTargets: r.cfg.Scope.DenyTargets,
		LiveWriter:       liveWriter,
	}
	result, err := execRunner.RunCommandWithContext(ctx, "msfconsole", cmdArgs...)
	wasCanceled := errors.Is(err, context.Canceled)
	if stopInterrupt != nil {
		stopInterrupt()
	}
	r.ensureTTYLineBreak()
	if result.LogPath != "" {
		r.logger.Printf("Log saved: %s", result.LogPath)
		r.recordActionArtifact(result.LogPath)
	}
	r.recordObservationFromResult("msf", result, err)
	r.maybeAutoSummarize(result.LogPath, "msf")

	safePrint(renderExecSummary(r.currentTask, "msfconsole", cmdArgs, time.Since(start), result.LogPath, "disabled", result.Output, err))
	if err != nil {
		if wasCanceled {
			r.logger.Printf("Interrupted. What should I do differently?")
			return fmt.Errorf("command interrupted")
		}
		return err
	}

	lines := msf.ParseSearchOutput(result.Output)
	if len(lines) == 0 {
		r.logger.Printf("No modules found")
		return nil
	}
	r.logger.Printf("Modules:")
	for _, line := range lines {
		r.logger.Printf("%s", line)
	}
	return nil
}
