package exec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type PermissionLevel string

const (
	PermissionReadOnly PermissionLevel = "readonly"
	PermissionDefault  PermissionLevel = "default"
	PermissionAll      PermissionLevel = "all"
)

type Runner struct {
	Permissions      PermissionLevel
	RequireApproval  bool
	LogDir           string
	Timeout          time.Duration
	Reader           *bufio.Reader
	Now              func() time.Time
	ScopeNetworks    []string
	ScopeTargets     []string
	ScopeDenyTargets []string
	LiveWriter       io.Writer
}

type CommandResult struct {
	Command string
	Args    []string
	Output  string
	Error   error
	LogPath string
}

func (r *Runner) RunCommandWithContext(ctx context.Context, command string, args ...string) (CommandResult, error) {
	if r.Permissions == PermissionReadOnly {
		return CommandResult{}, fmt.Errorf("readonly mode: execution not permitted")
	}
	if err := r.validateScope(command, args); err != nil {
		return CommandResult{}, err
	}
	if r.RequireApproval {
		approved, err := r.confirm(fmt.Sprintf("Run command: %s %s?", command, strings.Join(args, " ")))
		if err != nil {
			return CommandResult{}, err
		}
		if !approved {
			return CommandResult{}, fmt.Errorf("execution not approved")
		}
	}
	if r.Timeout == 0 {
		r.Timeout = 30 * time.Second
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.LogDir == "" {
		r.LogDir = "sessions"
	}

	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	var output string
	var err error
	if r.LiveWriter != nil {
		output, err = runWithStreaming(cmd, r.LiveWriter)
	} else {
		var combined []byte
		combined, err = cmd.CombinedOutput()
		output = string(combined)
	}
	result := CommandResult{
		Command: command,
		Args:    args,
		Output:  strings.TrimSpace(output),
		Error:   err,
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.Error = fmt.Errorf("command timeout after %s", r.Timeout)
	}

	logPath, logErr := r.writeLog(result)
	if logErr == nil {
		result.LogPath = logPath
	}
	return result, result.Error
}

func (r *Runner) RunCommand(command string, args ...string) (CommandResult, error) {
	return r.RunCommandWithContext(context.Background(), command, args...)
}

func runWithStreaming(cmd *exec.Cmd, live io.Writer) (string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	var mu sync.Mutex
	writer := streamWriter{
		buf:  &buf,
		live: live,
		mu:   &mu,
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(writer, stdout)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(writer, stderr)
	}()

	waitErr := cmd.Wait()
	wg.Wait()
	return buf.String(), waitErr
}

type streamWriter struct {
	buf  *bytes.Buffer
	live io.Writer
	mu   *sync.Mutex
}

func (w streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.live != nil {
		_, _ = w.live.Write(p)
	}
	return w.buf.Write(p)
}

func (r *Runner) writeLog(result CommandResult) (string, error) {
	timestamp := r.Now().UTC().Format("20060102-150405.000000000")
	base := fmt.Sprintf("cmd-%s.log", timestamp)
	path := filepath.Join(r.LogDir, base)
	if err := os.MkdirAll(r.LogDir, 0o755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}
	content := fmt.Sprintf("$ %s %s\n\n%s\n", result.Command, strings.Join(result.Args, " "), result.Output)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write log: %w", err)
	}
	return path, nil
}

func (r *Runner) confirm(prompt string) (bool, error) {
	reader := r.Reader
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	for {
		fmt.Printf("%s [y/N]: ", prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" || answer == "n" || answer == "no" {
			return false, nil
		}
		if answer == "y" || answer == "yes" {
			return true, nil
		}
	}
}
