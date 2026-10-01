package subscription

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LoginState contains only the device-code instructions needed by the UI.
// OAuth tokens and account details remain in Codex's local credential store.
type LoginState struct {
	Status          string `json:"status"`
	VerificationURL string `json:"verification_url,omitempty"`
	UserCode        string `json:"user_code,omitempty"`
	Message         string `json:"message,omitempty"`
}

// LoginManager owns one browser-initiated ChatGPT device-code sign-in. The
// device flow also works when the web browser and Kali VM have different
// loopback addresses, unlike a callback hosted inside the VM.
type LoginManager struct {
	Home   string
	mu     sync.Mutex
	state  LoginState
	cancel context.CancelFunc
	gen    uint64
}

func (m *LoginManager) Status() LoginState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Status == "" {
		return LoginState{Status: "idle"}
	}
	return m.state
}

func (m *LoginManager) Start() LoginState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Status == "starting" || m.state.Status == "awaiting_user" {
		return m.state
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	m.gen++
	gen := m.gen
	m.cancel = cancel
	m.state = LoginState{Status: "starting", Message: "Preparing ChatGPT sign-in…"}
	go m.run(ctx, gen, cancel)
	return m.state
}

func (m *LoginManager) Cancel() LoginState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.gen++
	m.state = LoginState{Status: "idle"}
	return m.state
}

func (m *LoginManager) update(gen uint64, state LoginState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A cancellation or replacement must not be overwritten by the old process.
	if m.gen != gen {
		return
	}
	m.state = state
}

func (m *LoginManager) run(ctx context.Context, gen uint64, cancel context.CancelFunc) {
	defer cancel()
	defer func() {
		m.mu.Lock()
		if m.gen == gen {
			m.cancel = nil
		}
		m.mu.Unlock()
	}()
	if err := m.login(ctx, gen); err != nil && ctx.Err() == nil {
		m.update(gen, LoginState{Status: "failed", Message: err.Error()})
	}
}

func (m *LoginManager) login(ctx context.Context, gen uint64) error {
	home := m.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot find a local credential directory")
		}
		home = filepath.Join(home, ".codex")
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return fmt.Errorf("cannot prepare the local credential directory")
	}
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
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("cannot open Codex sign-in")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("cannot open Codex sign-in")
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Codex CLI is required for ChatGPT sign-in on this host")
	}
	defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(map[string]any{"id": 0, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "birdhackbot", "title": "BirdHackBot", "version": "0.1.0"}}}); err != nil {
		return fmt.Errorf("cannot initialize ChatGPT sign-in")
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var loginID string
	for scanner.Scan() {
		var event struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return fmt.Errorf("invalid Codex sign-in response")
		}
		if event.ID != nil && len(event.Error) > 0 && string(event.Error) != "null" {
			return fmt.Errorf("Codex could not start ChatGPT sign-in")
		}
		if event.ID != nil && *event.ID == 0 {
			if err := encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
				return fmt.Errorf("cannot initialize ChatGPT sign-in")
			}
			if err := encoder.Encode(map[string]any{"id": 1, "method": "account/login/start", "params": map[string]string{"type": "chatgptDeviceCode"}}); err != nil {
				return fmt.Errorf("cannot start ChatGPT sign-in")
			}
		}
		if event.ID != nil && *event.ID == 1 {
			var result struct {
				Type            string `json:"type"`
				LoginID         string `json:"loginId"`
				VerificationURL string `json:"verificationUrl"`
				UserCode        string `json:"userCode"`
			}
			if json.Unmarshal(event.Result, &result) != nil || result.Type != "chatgptDeviceCode" || result.LoginID == "" || result.UserCode == "" || !validVerificationURL(result.VerificationURL) {
				return fmt.Errorf("Codex returned incomplete ChatGPT sign-in instructions")
			}
			loginID = result.LoginID
			m.update(gen, LoginState{Status: "awaiting_user", VerificationURL: result.VerificationURL, UserCode: result.UserCode, Message: "Open ChatGPT and enter this one-time code."})
		}
		if event.Method == "account/login/completed" {
			var result struct {
				LoginID string `json:"loginId"`
				Success bool   `json:"success"`
			}
			if json.Unmarshal(event.Params, &result) != nil || result.LoginID == "" || result.LoginID != loginID {
				continue
			}
			if !result.Success {
				return fmt.Errorf("ChatGPT sign-in was not completed; try again")
			}
			if _, err := (&CodexAuth{Home: home}).Read(); err != nil {
				return fmt.Errorf("ChatGPT sign-in completed, but file-based credentials are unavailable")
			}
			m.update(gen, LoginState{Status: "connected", Message: "ChatGPT sign-in completed on this Kali host."})
			return nil
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("ChatGPT sign-in timed out; start it again")
	}
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("Codex sign-in closed before completion")
}

func validVerificationURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Hostname() == "auth.openai.com" && u.Port() == ""
}
