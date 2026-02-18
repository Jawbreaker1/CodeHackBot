package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestIntegrationZipCrackSolvable(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_INTEGRATION") != "1" {
		t.Skip("set BIRDHACKBOT_INTEGRATION=1 to run integration scenarios")
	}
	requireCommandsOrSkip(t, "zip", "unzip")

	base := t.TempDir()
	runID := "run-int-zip-solvable"
	planPath := filepath.Join(base, "plan.json")
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_integration_only"},
		SuccessCriteria: []string{"zip password recovered"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "zip-crack",
				Goal:              "crack protected zip",
				DoneWhen:          []string{"password recovered"},
				FailWhen:          []string{"budget exhausted"},
				ExpectedArtifacts: []string{"secret.zip", "wordlist.txt", "extracted/secret.txt"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     200,
					MaxToolCalls: 200,
					MaxRuntime:   30 * time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	runArgs := []string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-zip-crack",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--worker-env", "TEST_SESSIONS_DIR=" + base,
		"--tick", "20ms",
		"--startup-timeout", "2s",
		"--stale-timeout", "2s",
		"--soft-stall-grace", "2s",
		"--max-attempts", "1",
	}
	if code := run(runArgs, &out, &errOut); code != 0 {
		t.Fatalf("run failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run completed: "+runID) {
		t.Fatalf("unexpected run output: %s", out.String())
	}

	manager := orchestrator.NewManager(base)
	findings, err := manager.ListFindings(runID)
	if err != nil {
		t.Fatalf("ListFindings: %v", err)
	}
	foundRecovered := false
	for _, finding := range findings {
		if strings.Contains(strings.ToLower(finding.Title), "zip password recovered") {
			foundRecovered = true
			break
		}
	}
	if !foundRecovered {
		t.Fatalf("expected recovery finding in merged findings")
	}

	out.Reset()
	errOut.Reset()
	reportPath := filepath.Join(base, "integration-report.md")
	if code := run([]string{"report", "--sessions-dir", base, "--run", runID, "--out", reportPath}, &out, &errOut); code != 0 {
		t.Fatalf("report failed: code=%d err=%s", code, errOut.String())
	}
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(reportData)), "zip password recovered") {
		t.Fatalf("expected report to include recovered password finding")
	}
}

func TestIntegrationZipCrackUnsolvedTriggersReplan(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_INTEGRATION") != "1" {
		t.Skip("set BIRDHACKBOT_INTEGRATION=1 to run integration scenarios")
	}
	requireCommandsOrSkip(t, "zip", "unzip")

	base := t.TempDir()
	runID := "run-int-zip-unsolved"
	planPath := filepath.Join(base, "plan.json")
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_integration_only"},
		SuccessCriteria: []string{"attempted"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "zip-crack",
				Goal:              "crack protected zip",
				DoneWhen:          []string{"password recovered"},
				FailWhen:          []string{"not recovered"},
				ExpectedArtifacts: []string{"secret.zip", "wordlist.txt"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     200,
					MaxToolCalls: 200,
					MaxRuntime:   30 * time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	runArgs := []string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-zip-unsolved",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--worker-env", "TEST_SESSIONS_DIR=" + base,
		"--tick", "20ms",
		"--startup-timeout", "2s",
		"--stale-timeout", "2s",
		"--soft-stall-grace", "2s",
		"--max-attempts", "1",
	}
	if code := run(runArgs, &out, &errOut); code != 0 {
		t.Fatalf("run failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run completed: "+runID) {
		t.Fatalf("unexpected run output: %s", out.String())
	}

	events, err := orchestrator.NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasReplanTrigger(events, "missing_required_artifacts_after_retries") {
		t.Fatalf("expected replan trigger for missing required artifacts after retries")
	}
}

func requireCommandsOrSkip(t *testing.T, commands ...string) {
	t.Helper()
	for _, command := range commands {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("missing required command %q: %v", command, err)
		}
	}
}

func helperZipCrackScenario(solvable bool) error {
	manager, runID, taskID, workerID, err := helperManagerAndIDs()
	if err != nil {
		return err
	}
	signalWorker := "signal-" + workerID

	workspace := filepath.Join(os.Getenv("TEST_SESSIONS_DIR"), runID, "orchestrator", "artifact", "zip-integration", taskID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return err
	}
	secretPath := filepath.Join(workspace, "secret.txt")
	secretContent := "BirdHack{integration_zip_crack}"
	if err := os.WriteFile(secretPath, []byte(secretContent), 0o644); err != nil {
		return err
	}
	password, err := randomPasswordHex(4)
	if err != nil {
		return err
	}
	zipPath := filepath.Join(workspace, "secret.zip")
	createCmd := exec.Command("zip", "-j", "-P", password, zipPath, secretPath)
	createCmd.Dir = workspace
	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("zip create failed: %v output=%s", err, string(output))
	}

	wordlistPath := filepath.Join(workspace, "wordlist.txt")
	wordlist := []string{"admin123", "password1", "telefo01", "birdhack"}
	if solvable {
		wordlist = append(wordlist, password)
	}
	wordlist = append(wordlist, "letmein", "qwerty123")
	if err := os.WriteFile(wordlistPath, []byte(strings.Join(wordlist, "\n")+"\n"), 0o644); err != nil {
		return err
	}

	_ = manager.EmitEvent(runID, signalWorker, taskID, orchestrator.EventTypeTaskArtifact, map[string]any{
		"type":  "zip",
		"title": "integration encrypted zip",
		"path":  zipPath,
	})
	_ = manager.EmitEvent(runID, signalWorker, taskID, orchestrator.EventTypeTaskArtifact, map[string]any{
		"type":  "wordlist",
		"title": "integration wordlist",
		"path":  wordlistPath,
	})

	extractDir := filepath.Join(workspace, "extracted")
	_ = os.MkdirAll(extractDir, 0o755)
	steps := 0
	toolCalls := 0
	found := ""
	file, err := os.Open(wordlistPath)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		candidate := strings.TrimSpace(scanner.Text())
		if candidate == "" {
			continue
		}
		steps++
		toolCalls++
		_ = manager.EmitEvent(runID, signalWorker, taskID, orchestrator.EventTypeTaskProgress, map[string]any{
			"message":    "trying candidate",
			"steps":      steps,
			"tool_calls": toolCalls,
		})
		cmd := exec.Command("unzip", "-P", candidate, "-o", zipPath, "-d", extractDir)
		if err := cmd.Run(); err == nil {
			found = candidate
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if found == "" {
		_ = manager.EmitEvent(runID, signalWorker, taskID, orchestrator.EventTypeTaskFailed, map[string]any{
			"reason": "missing_required_artifacts",
		})
		return fmt.Errorf("password not found in integration wordlist")
	}

	extractedPath := filepath.Join(extractDir, "secret.txt")
	_ = manager.EmitEvent(runID, signalWorker, taskID, orchestrator.EventTypeTaskArtifact, map[string]any{
		"type":  "file",
		"title": "extracted secret",
		"path":  extractedPath,
	})
	_ = manager.EmitEvent(runID, signalWorker, taskID, orchestrator.EventTypeTaskFinding, map[string]any{
		"target":       zipPath,
		"finding_type": "zip_password_recovered",
		"title":        "zip password recovered",
		"severity":     "medium",
		"confidence":   "high",
		"source":       "integration-worker",
		"evidence": []string{
			"password:" + found,
			"extracted_file:" + extractedPath,
		},
		"metadata": map[string]any{
			"location": extractedPath,
		},
	})
	return nil
}

func randomPasswordHex(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		bytesLen = 4
	}
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
