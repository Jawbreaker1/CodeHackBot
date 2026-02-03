package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var cliPath string
var projectRoot string

func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	projectRoot = filepath.Clean(filepath.Join(wd, "..", ".."))

	temp, err := os.MkdirTemp("", "birdhackbot-e2e-")
	if err != nil {
		os.Exit(1)
	}
	cliPath = filepath.Join(temp, "birdhackbot")

	cmd := exec.Command("go", "build", "-o", cliPath, "./cmd/birdhackbot")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		_, _ = os.Stderr.Write(output)
		os.Exit(1)
	}

	exitCode := m.Run()
	_ = os.RemoveAll(temp)
	os.Exit(exitCode)
}

func TestCLIInteractiveFlow(t *testing.T) {
	temp := t.TempDir()
	sessionsDir := filepath.Join(temp, "sessions")
	configPath := filepath.Join(temp, "config.json")

	cfg := map[string]any{
		"tools": map[string]any{
			"shell": map[string]any{
				"enabled":         true,
				"timeout_seconds": 5,
			},
		},
		"session": map[string]any{
			"log_dir": sessionsDir,
		},
		"permissions": map[string]any{
			"level":            "default",
			"require_approval": false,
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	input := "/init no-inventory\n/run echo hello\n/stop\n/exit\n"
	cmd := exec.Command(cliPath, "--config", configPath, "--permissions", "all")
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("cli run error: %v\nOutput:\n%s", err, out.String())
	}

	output := out.String()
	assertContains(t, output, "BirdHackBot interactive mode")
	assertContains(t, output, "Init Summary")
	assertContains(t, output, "Execution Summary")
	assertContains(t, output, "Command: echo hello")
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q\nOutput:\n%s", needle, haystack)
	}
}
