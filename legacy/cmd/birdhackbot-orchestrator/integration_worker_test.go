package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestIntegrationProductionWorkerNmapApprovalFlow(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_INTEGRATION") != "1" {
		t.Skip("set BIRDHACKBOT_INTEGRATION=1 to run integration scenarios")
	}
	requireCommandsOrSkip(t, "go", "nmap")

	base := t.TempDir()
	runID := "run-int-worker-nmap"
	planPath := filepath.Join(base, "plan.json")
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_integration_only"},
		SuccessCriteria: []string{"nmap completed"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "nmap-local",
				Goal:              "run safe nmap ping scan on localhost",
				Targets:           []string{"127.0.0.1"},
				DoneWhen:          []string{"scan complete"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"command log"},
				RiskLevel:         string(orchestrator.RiskActiveProbe),
				Action: orchestrator.TaskAction{
					Type:    "command",
					Command: "nmap",
					Args:    []string{"-sn", "127.0.0.1"},
				},
				Budget: orchestrator.TaskBudget{
					MaxSteps:     2,
					MaxToolCalls: 2,
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

	repoRoot := testRepoRoot(t)
	binPath := buildBirdHackBotBinary(t, repoRoot, base)

	runArgs := []string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
		"--worker-cmd", binPath,
		"--worker-arg", "worker",
		"--permissions", "default",
		"--tick", "20ms",
		"--startup-timeout", "2s",
		"--stale-timeout", "2s",
		"--soft-stall-grace", "2s",
		"--max-attempts", "1",
	}
	var runOut bytes.Buffer
	var runErr bytes.Buffer
	codeCh := make(chan int, 1)
	go func() {
		codeCh <- run(runArgs, &runOut, &runErr)
	}()

	manager := orchestrator.NewManager(base)
	approvalID := waitForApprovalID(t, manager, runID, 10*time.Second)

	var approveOut bytes.Buffer
	var approveErr bytes.Buffer
	if code := run([]string{
		"approve",
		"--sessions-dir", base,
		"--run", runID,
		"--approval", approvalID,
		"--scope", "task",
		"--actor", "integration",
		"--reason", "allow localhost nmap",
	}, &approveOut, &approveErr); code != 0 {
		t.Fatalf("approve failed: code=%d err=%s", code, approveErr.String())
	}

	select {
	case code := <-codeCh:
		if code != 0 {
			t.Fatalf("run failed: code=%d err=%s", code, runErr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for integration run completion")
	}

	reportPath := filepath.Join(base, "integration-worker-report.md")
	var reportOut bytes.Buffer
	var reportErr bytes.Buffer
	if code := run([]string{"report", "--sessions-dir", base, "--run", runID, "--out", reportPath}, &reportOut, &reportErr); code != 0 {
		t.Fatalf("report failed: code=%d err=%s", code, reportErr.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := strings.ToLower(string(data))
	if !strings.Contains(content, "task action completed") {
		t.Fatalf("expected report to include completion finding")
	}
	if !strings.Contains(content, "127.0.0.1") {
		t.Fatalf("expected report to include localhost target")
	}
}
