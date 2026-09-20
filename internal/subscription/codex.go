package subscription

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// This connection only refreshes account credentials. It never creates a Codex
// thread, starts a model turn, or requests tool execution.
func refreshWithCodex(ctx context.Context, home string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "-c", `cli_auth_credentials_store="file"`, "-c", `model_provider="openai"`, "app-server")
	cmd.Dir = home
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "CODEX_HOME", "OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "CODEX_HOME="+home)
	cmd.Stderr = io.Discard // account diagnostics must not enter worker logs
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start codex for subscription refresh; install Codex CLI")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	encoder := json.NewEncoder(stdin)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	receive := func(id int) error {
		for scanner.Scan() {
			var reply struct {
				ID    *int            `json:"id"`
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal(scanner.Bytes(), &reply) != nil {
				return fmt.Errorf("invalid Codex auth response")
			}
			if reply.ID == nil || *reply.ID != id {
				continue
			}
			if len(reply.Error) != 0 && string(reply.Error) != "null" {
				return fmt.Errorf("subscription refresh failed; sign in with codex login again")
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("Codex auth connection closed before refresh completed")
	}
	if err := encoder.Encode(map[string]any{"id": 0, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "birdhackbot", "version": "0.1.0"}}}); err != nil {
		return err
	}
	if err := receive(0); err != nil {
		return err
	}
	if err := encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
		return err
	}
	if err := encoder.Encode(map[string]any{"id": 1, "method": "account/read", "params": map[string]bool{"refreshToken": true}}); err != nil {
		return err
	}
	return receive(1)
}
