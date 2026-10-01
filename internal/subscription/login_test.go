package subscription

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoginManagerDeviceFlow(t *testing.T) {
	home := t.TempDir()
	gate := filepath.Join(t.TempDir(), "complete")
	installFakeCodex(t, gate)
	m := &LoginManager{Home: home}
	if state := m.Start(); state.Status != "starting" {
		t.Fatalf("start: %+v", state)
	}
	awaitLoginState(t, m, "awaiting_user")
	state := m.Status()
	if state.UserCode != "ABCD-1234" || state.VerificationURL != "https://auth.openai.com/codex/device" {
		t.Fatalf("device instructions: %+v", state)
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	awaitLoginState(t, m, "connected")
	if _, err := (&CodexAuth{Home: home}).Read(); err != nil {
		t.Fatalf("Codex credentials were not saved: %v", err)
	}
}

func TestLoginManagerCancel(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "never-complete")
	installFakeCodex(t, gate)
	m := &LoginManager{Home: t.TempDir()}
	m.Start()
	awaitLoginState(t, m, "awaiting_user")
	if state := m.Cancel(); state.Status != "idle" {
		t.Fatalf("cancel: %+v", state)
	}
	time.Sleep(50 * time.Millisecond)
	if state := m.Status(); state.Status != "idle" {
		t.Fatalf("cancelled process changed state: %+v", state)
	}
}

func TestVerificationURLIsProviderOnly(t *testing.T) {
	for _, raw := range []string{"http://auth.openai.com/codex/device", "https://auth.openai.com.evil.test/", "https://user@auth.openai.com/", "https://chatgpt.com/"} {
		if validVerificationURL(raw) {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func awaitLoginState(t *testing.T, m *LoginManager, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := m.Status()
		if state.Status == want {
			return
		}
		if state.Status == "failed" {
			t.Fatalf("login failed: %+v", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waiting for %s; got %+v", want, m.Status())
}

func installFakeCodex(t *testing.T, gate string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	launcher := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=TestLoginHelperProcess -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(path, "codex"), []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", path+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BHB_FAKE_CODEX_LOGIN", "1")
	t.Setenv("BHB_FAKE_CODEX_GATE", gate)
}

func TestLoginHelperProcess(t *testing.T) {
	if os.Getenv("BHB_FAKE_CODEX_LOGIN") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		if request.ID != nil && *request.ID == 0 && request.Method == "initialize" {
			fmt.Println(`{"id":0,"result":{"userAgent":"fake"}}`)
		}
		if request.ID != nil && *request.ID == 1 && request.Method == "account/login/start" {
			fmt.Println(`{"id":1,"result":{"type":"chatgptDeviceCode","loginId":"test-login","verificationUrl":"https://auth.openai.com/codex/device","userCode":"ABCD-1234"}}`)
			gate := os.Getenv("BHB_FAKE_CODEX_GATE")
			for i := 0; i < 500; i++ {
				if _, err := os.Stat(gate); err == nil {
					home := os.Getenv("CODEX_HOME")
					_ = os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"fixture","account_id":"fixture-account"}}`), 0600)
					fmt.Println(`{"method":"account/login/completed","params":{"loginId":"test-login","success":true,"error":null}}`)
					os.Exit(0)
				}
				time.Sleep(10 * time.Millisecond)
			}
			os.Exit(3)
		}
	}
	os.Exit(4)
}
