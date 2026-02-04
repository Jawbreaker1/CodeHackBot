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

func TestCLISummarizeManual(t *testing.T) {
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
		"network": map[string]any{
			"assume_offline": true,
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	input := "/init no-inventory\n/run echo 10.0.0.1\n/summarize manual-test\n/stop\n/exit\n"
	cmd := exec.Command(cliPath, "--config", configPath, "--permissions", "all")
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("cli run error: %v\nOutput:\n%s", err, out.String())
	}

	sessionDir := findSingleSessionDir(t, sessionsDir)
	summaryPath := filepath.Join(sessionDir, "summary.md")
	data, err = os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(data), "Summary refreshed (manual-test).") {
		t.Fatalf("summary refresh missing:\n%s", string(data))
	}
	factsPath := filepath.Join(sessionDir, "known_facts.md")
	data, err = os.ReadFile(factsPath)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if !strings.Contains(string(data), "10.0.0.1") {
		t.Fatalf("expected fact for IP:\n%s", string(data))
	}
}

func TestCLIAutoSummarize(t *testing.T) {
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
		"context": map[string]any{
			"max_recent_outputs":    1,
			"summarize_every_steps": 1,
			"summarize_at_percent":  50,
		},
		"network": map[string]any{
			"assume_offline": true,
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

	sessionDir := findSingleSessionDir(t, sessionsDir)
	summaryPath := filepath.Join(sessionDir, "summary.md")
	data, err = os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(data), "Recent logs:") {
		t.Fatalf("expected recent logs in summary:\n%s", string(data))
	}
}

func TestCLIPlanAuto(t *testing.T) {
	temp := t.TempDir()
	sessionsDir := filepath.Join(temp, "sessions")
	configPath := filepath.Join(temp, "config.json")

	cfg := map[string]any{
		"session": map[string]any{
			"log_dir": sessionsDir,
		},
		"permissions": map[string]any{
			"level":            "default",
			"require_approval": false,
		},
		"network": map[string]any{
			"assume_offline": true,
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	input := "/init no-inventory\n/plan auto sprint8\n/stop\n/exit\n"
	cmd := exec.Command(cliPath, "--config", configPath, "--permissions", "all")
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("cli run error: %v\nOutput:\n%s", err, out.String())
	}

	sessionDir := findSingleSessionDir(t, sessionsDir)
	planPath := filepath.Join(sessionDir, "plan.md")
	data, err = os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if !strings.Contains(string(data), "Auto Plan (sprint8)") {
		t.Fatalf("expected auto plan content:\n%s", string(data))
	}
}

func TestCLIAssistFallback(t *testing.T) {
	temp := t.TempDir()
	sessionsDir := filepath.Join(temp, "sessions")
	configPath := filepath.Join(temp, "config.json")

	cfg := map[string]any{
		"session": map[string]any{
			"log_dir": sessionsDir,
		},
		"permissions": map[string]any{
			"level":            "default",
			"require_approval": false,
		},
		"scope": map[string]any{
			"targets": []string{"10.0.0.5"},
		},
		"network": map[string]any{
			"assume_offline": true,
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	input := "/init no-inventory\n/assist dry\n/stop\n/exit\n"
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
	if !strings.Contains(output, "Suggested command: nmap -sV 10.0.0.5") {
		t.Fatalf("expected fallback assist suggestion:\n%s", output)
	}
	if strings.Contains(output, "execution not approved") {
		t.Fatalf("did not expect approval prompt:\n%s", output)
	}
}

func findSingleSessionDir(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(root, entry.Name())
		}
	}
	t.Fatalf("no session directory found in %s", root)
	return ""
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q\nOutput:\n%s", needle, haystack)
	}
}
