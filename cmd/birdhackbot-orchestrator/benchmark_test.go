package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestRunBenchmarkWritesScorecardsAndSummary(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	packPath := filepath.Join(base, "scenario-pack.json")
	writeBenchmarkScenarioPack(t, packPath, benchmarkScenarioPack{
		Version: "test-pack-v1",
		Scenarios: []benchmarkScenario{
			{
				ID:              "scenario-evidence",
				Name:            "Evidence scenario",
				Goal:            "Collect evidence for benchmark validation.",
				Scope:           benchmarkScopeLocalhost(),
				Constraints:     []string{"internal_lab_only"},
				SuccessCriteria: []string{"evidence_collected"},
				StopCriteria:    []string{"max_runtime=2m"},
				Planner:         "static",
				PermissionMode:  "default",
				MaxParallelism:  1,
				MaxAttempts:     1,
			},
		},
	})

	outDir := filepath.Join(base, "bench-out")
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"benchmark",
		"--sessions-dir", base,
		"--scenario-pack", packPath,
		"--out-dir", outDir,
		"--benchmark-id", "bench-test",
		"--repeat", "1",
		"--seed", "11",
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-evidence",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--worker-env", "TEST_SESSIONS_DIR=" + base,
		"--tick", "20ms",
		"--startup-timeout", "1s",
		"--stale-timeout", "1s",
		"--soft-stall-grace", "1s",
		"--approval-timeout", "2m",
		"--stop-grace", "500ms",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("benchmark failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "benchmark complete:") {
		t.Fatalf("expected completion output, got: %q", out.String())
	}

	summaryPath := filepath.Join(outDir, "bench-test", "summary.json")
	summaryRaw, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	summary := benchmarkSummary{}
	if err := json.Unmarshal(summaryRaw, &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if summary.BenchmarkID != "bench-test" {
		t.Fatalf("unexpected benchmark id: %q", summary.BenchmarkID)
	}
	if len(summary.Scenarios) != 1 {
		t.Fatalf("expected 1 scenario summary, got %d", len(summary.Scenarios))
	}
	if len(summary.Scenarios[0].Runs) != 1 {
		t.Fatalf("expected 1 run scorecard, got %d", len(summary.Scenarios[0].Runs))
	}

	scorecard := summary.Scenarios[0].Runs[0]
	if scorecard.RunID == "" {
		t.Fatalf("expected run id in scorecard")
	}
	if _, err := os.Stat(scorecard.StdoutLogPath); err != nil {
		t.Fatalf("expected stdout log file: %v", err)
	}
	if _, err := os.Stat(scorecard.StderrLogPath); err != nil {
		t.Fatalf("expected stderr log file: %v", err)
	}
	if scorecard.Metrics.TotalTasks <= 0 {
		t.Fatalf("expected scorecard total_tasks > 0, got %d", scorecard.Metrics.TotalTasks)
	}
}

func TestRunBenchmarkLocksBaseline(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	packPath := filepath.Join(base, "scenario-pack.json")
	writeBenchmarkScenarioPack(t, packPath, benchmarkScenarioPack{
		Version: "test-pack-v1",
		Scenarios: []benchmarkScenario{
			{
				ID:              "scenario-baseline",
				Name:            "Baseline scenario",
				Goal:            "Run baseline benchmark scenario.",
				Scope:           benchmarkScopeLocalhost(),
				Constraints:     []string{"internal_lab_only"},
				SuccessCriteria: []string{"baseline_done"},
				StopCriteria:    []string{"max_runtime=2m"},
				Planner:         "static",
				PermissionMode:  "default",
				MaxParallelism:  1,
				MaxAttempts:     1,
			},
		},
	})

	outDir := filepath.Join(base, "bench-out")
	baselineOut := filepath.Join(base, "baseline.json")
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"benchmark",
		"--sessions-dir", base,
		"--scenario-pack", packPath,
		"--out-dir", outDir,
		"--benchmark-id", "bench-baseline",
		"--repeat", "2",
		"--seed", "17",
		"--lock-baseline",
		"--baseline-out", baselineOut,
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-evidence",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--worker-env", "TEST_SESSIONS_DIR=" + base,
		"--tick", "20ms",
		"--startup-timeout", "1s",
		"--stale-timeout", "1s",
		"--soft-stall-grace", "1s",
		"--approval-timeout", "2m",
		"--stop-grace", "500ms",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("benchmark failed: code=%d err=%s", code, errOut.String())
	}

	raw, err := os.ReadFile(baselineOut)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	baseline := benchmarkBaseline{}
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("unmarshal baseline: %v", err)
	}
	if baseline.Version == "" {
		t.Fatalf("expected baseline version")
	}
	if baseline.BenchmarkID != "bench-baseline" {
		t.Fatalf("unexpected baseline benchmark id: %q", baseline.BenchmarkID)
	}
	if len(baseline.Scenarios) != 1 {
		t.Fatalf("expected 1 baseline scenario, got %d", len(baseline.Scenarios))
	}
	if baseline.Scenarios[0].ScenarioID != "scenario-baseline" {
		t.Fatalf("unexpected baseline scenario id: %q", baseline.Scenarios[0].ScenarioID)
	}
	if baseline.Scenarios[0].Aggregate.Runs != 2 {
		t.Fatalf("expected baseline aggregate runs=2, got %d", baseline.Scenarios[0].Aggregate.Runs)
	}
}

func benchmarkScopeLocalhost() orchestrator.Scope {
	return orchestrator.Scope{
		Targets:  []string{"127.0.0.1"},
		Networks: []string{"127.0.0.0/8"},
	}
}

func writeBenchmarkScenarioPack(t *testing.T, path string, pack benchmarkScenarioPack) {
	t.Helper()
	data, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal scenario pack: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write scenario pack: %v", err)
	}
}
