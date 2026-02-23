package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWorkerTaskCommandAction(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-ok"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	cmd, args := testEchoCommand("worker-action-ok")
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "run command",
		DoneWhen:          []string{"command completed"},
		FailWhen:          []string{"command failed"},
		ExpectedArtifacts: []string{"command log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: cmd,
			Args:    args,
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	manager := NewManager(base)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskStarted) {
		t.Fatalf("expected task_started event")
	}
	if !hasEventType(events, EventTypeTaskProgress) {
		t.Fatalf("expected task_progress event")
	}
	artifactEvent, ok := firstEventByType(events, EventTypeTaskArtifact)
	if !ok {
		t.Fatalf("expected task_artifact event")
	}
	if !hasEventType(events, EventTypeTaskFinding) {
		t.Fatalf("expected task_finding event")
	}
	completedEvent, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed event")
	}

	payload := map[string]any{}
	if len(artifactEvent.Payload) > 0 {
		_ = json.Unmarshal(artifactEvent.Payload, &payload)
	}
	logPath, _ := payload["path"].(string)
	if strings.TrimSpace(logPath) == "" {
		t.Fatalf("task_artifact payload missing path")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read artifact log: %v", err)
	}
	if !strings.Contains(string(data), "worker-action-ok") {
		t.Fatalf("expected command output in artifact log, got: %q", string(data))
	}
	completedPayload := map[string]any{}
	if len(completedEvent.Payload) > 0 {
		_ = json.Unmarshal(completedEvent.Payload, &completedPayload)
	}
	contract, ok := completedPayload["completion_contract"].(map[string]any)
	if !ok {
		t.Fatalf("expected completion_contract payload")
	}
	if status, _ := contract["verification_status"].(string); status != "reported_by_worker" {
		t.Fatalf("expected verification_status reported_by_worker, got %q", status)
	}
}

func TestRunWorkerTaskCommandActionMaterializesExpectedArtifacts(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-materialized-artifacts"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	cmd, args := testEchoCommand("worker-artifact-materialize")
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "run command with expected artifacts",
		DoneWhen:          []string{"command completed"},
		FailWhen:          []string{"command failed"},
		ExpectedArtifacts: []string{"port_scan_output.txt", "service_version_output.txt"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: cmd,
			Args:    args,
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	manager := NewManager(base)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	completedEvent, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed event")
	}

	artifactPaths := []string{}
	for _, event := range events {
		if event.Type != EventTypeTaskArtifact {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		path := strings.TrimSpace(toString(payload["path"]))
		if path == "" {
			continue
		}
		artifactPaths = append(artifactPaths, path)
	}
	if len(artifactPaths) < 3 {
		t.Fatalf("expected command log + derived expected artifacts, got %d paths: %#v", len(artifactPaths), artifactPaths)
	}
	for _, expected := range []string{"port_scan_output.txt", "service_version_output.txt"} {
		path := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, taskID, expected)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read derived artifact %q: %v", expected, readErr)
		}
		if !strings.Contains(string(data), "worker-artifact-materialize") {
			t.Fatalf("expected derived artifact %q to contain command output", expected)
		}
	}

	completedPayload := map[string]any{}
	if len(completedEvent.Payload) > 0 {
		_ = json.Unmarshal(completedEvent.Payload, &completedPayload)
	}
	contract, ok := completedPayload["completion_contract"].(map[string]any)
	if !ok {
		t.Fatalf("expected completion_contract payload")
	}
	produced := sliceFromAny(contract["produced_artifacts"])
	if len(produced) < 3 {
		t.Fatalf("expected produced_artifacts to include derived artifacts, got %#v", produced)
	}
}

func TestRunWorkerTaskCommandActionMaterializesProducedArtifactFiles(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-produced-artifact-files"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	cmd, args := testWriteExpectedArtifactsCommand()
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "run command producing expected artifacts as files",
		DoneWhen:          []string{"command completed"},
		FailWhen:          []string{"command failed"},
		ExpectedArtifacts: []string{"port_scan_output.txt", "service_version_output.txt"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: cmd,
			Args:    args,
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	for expected, contains := range map[string]string{
		"port_scan_output.txt":       "scan-file",
		"service_version_output.txt": "service-file",
	} {
		path := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, taskID, expected)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read derived artifact %q: %v", expected, readErr)
		}
		got := string(data)
		if !strings.Contains(got, contains) {
			t.Fatalf("expected derived artifact %q to include %q, got %q", expected, contains, got)
		}
		if strings.Contains(got, "no command output captured") {
			t.Fatalf("expected derived artifact %q to contain produced file content, got fallback placeholder", expected)
		}
	}
}

func TestRunWorkerTaskRepairsMissingDependencyInputPaths(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-repair-inputs"
	taskID := "T-04"
	workerID := "worker-t4-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	artifactRoot := BuildRunPaths(base, runID).ArtifactDir
	serviceArtifact := filepath.Join(artifactRoot, "T-02", "nmap service scan output log")
	if err := os.MkdirAll(filepath.Dir(serviceArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll service artifact dir: %v", err)
	}
	if err := os.WriteFile(serviceArtifact, []byte("service evidence\n"), 0o644); err != nil {
		t.Fatalf("WriteFile service artifact: %v", err)
	}
	vulnArtifact := filepath.Join(artifactRoot, "T-03", "nmap vulnerability scan output log")
	if err := os.MkdirAll(filepath.Dir(vulnArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll vuln artifact dir: %v", err)
	}
	if err := os.WriteFile(vulnArtifact, []byte("vulnerability evidence\n"), 0o644); err != nil {
		t.Fatalf("WriteFile vuln artifact: %v", err)
	}

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "assemble report inputs",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-02", "T-03"},
		DoneWhen:          []string{"report inputs assembled"},
		FailWhen:          []string{"report input command failed"},
		ExpectedArtifacts: []string{"report_input.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "cat",
			Args:    []string{"/tmp/nmap_scan_results.log", "/tmp/vuln_scan_results.log"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	manager := NewManager(base)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	repairNotes := 0
	for _, event := range events {
		if event.Type != EventTypeTaskProgress {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		msg := strings.TrimSpace(toString(payload["message"]))
		if strings.Contains(msg, "runtime input repair: replaced missing path") {
			repairNotes++
		}
	}
	if repairNotes < 2 {
		t.Fatalf("expected at least two runtime input repair notes, got %d", repairNotes)
	}

	completedEvent, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed event")
	}
	completedPayload := map[string]any{}
	if len(completedEvent.Payload) > 0 {
		_ = json.Unmarshal(completedEvent.Payload, &completedPayload)
	}
	logPath := strings.TrimSpace(toString(completedPayload["log_path"]))
	if logPath == "" {
		t.Fatalf("expected completed payload log_path")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile command log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "service evidence") || !strings.Contains(content, "vulnerability evidence") {
		t.Fatalf("expected repaired command output to include dependency artifacts, got: %q", content)
	}
}

func TestRunWorkerTaskRepairsMissingDependencyInputPathsForShellWrapper(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-repair-inputs-shell"
	taskID := "T-04"
	workerID := "worker-t4-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	artifactRoot := BuildRunPaths(base, runID).ArtifactDir
	serviceArtifact := filepath.Join(artifactRoot, "T-02", "nmap service scan output log")
	if err := os.MkdirAll(filepath.Dir(serviceArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll service artifact dir: %v", err)
	}
	if err := os.WriteFile(serviceArtifact, []byte("service evidence\n"), 0o644); err != nil {
		t.Fatalf("WriteFile service artifact: %v", err)
	}
	vulnArtifact := filepath.Join(artifactRoot, "T-03", "nmap vulnerability scan output log")
	if err := os.MkdirAll(filepath.Dir(vulnArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll vuln artifact dir: %v", err)
	}
	if err := os.WriteFile(vulnArtifact, []byte("vulnerability evidence\n"), 0o644); err != nil {
		t.Fatalf("WriteFile vuln artifact: %v", err)
	}

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "assemble report inputs through shell wrapper",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-02", "T-03"},
		DoneWhen:          []string{"report inputs assembled"},
		FailWhen:          []string{"report input command failed"},
		ExpectedArtifacts: []string{"report_input.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "bash",
			Args: []string{
				"-lc",
				"cat /tmp/nmap_scan_results.log /tmp/vuln_scan_results.log > report_input.log",
			},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	repaired := false
	for _, event := range events {
		if event.Type != EventTypeTaskProgress {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		message := strings.TrimSpace(toString(payload["message"]))
		if strings.Contains(message, "runtime input repair: replaced missing path /tmp/nmap_scan_results.log") ||
			strings.Contains(message, "runtime input repair: replaced missing path /tmp/vuln_scan_results.log") {
			repaired = true
			break
		}
	}
	if !repaired {
		t.Fatalf("expected runtime input repair note for shell wrapper command")
	}

	reportInputPath := filepath.Join(artifactRoot, taskID, "report_input.log")
	data, err := os.ReadFile(reportInputPath)
	if err != nil {
		t.Fatalf("ReadFile report_input.log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "service evidence") || !strings.Contains(content, "vulnerability evidence") {
		t.Fatalf("expected repaired shell command output to include dependency evidence, got %q", content)
	}
}

func TestRepairMissingCommandInputPathsForShellWrapperSkipsEmbeddedRelativeSegments(t *testing.T) {
	t.Parallel()

	command := "bash"
	args := []string{
		"-lc",
		"printf 'extracted_secret/secret_text.txt\\n' > extracted_files.txt",
	}
	candidates := []string{
		"/tmp/run/artifact/T-02/john_secret.pot",
		"/tmp/run/artifact/T-03/john_show.txt",
	}

	nextArgs, notes, repaired := repairMissingCommandInputPathsForShellWrapper(command, args, candidates)
	if repaired {
		t.Fatalf("expected no shell-wrapper repair, got args=%q notes=%q", nextArgs, notes)
	}
	if !reflect.DeepEqual(nextArgs, args) {
		t.Fatalf("expected args unchanged, got %q", nextArgs)
	}
	if len(notes) != 0 {
		t.Fatalf("expected no repair notes, got %q", notes)
	}
}

func TestBestArtifactCandidateForMissingPathPrefersSpecificArtifactOverWorkerLog(t *testing.T) {
	t.Parallel()

	missingPath := "/tmp/nmap_scan_results.log"
	candidates := []string{
		"/tmp/run/orchestrator/artifact/T-03/worker-T-03-a1-a1.log",
		"/tmp/run/orchestrator/artifact/T-03/service configuration logs",
		"/tmp/run/orchestrator/artifact/T-03/vulnerability mapping report",
	}

	got := bestArtifactCandidateForMissingPath(missingPath, candidates, map[string]struct{}{})
	want := "/tmp/run/orchestrator/artifact/T-03/service configuration logs"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBestArtifactCandidateForMissingPathFallsBackToExactBaseMatch(t *testing.T) {
	t.Parallel()

	missingPath := "/tmp/worker-T-03-a1-a1.log"
	candidates := []string{
		"/tmp/run/orchestrator/artifact/T-03/worker-T-03-a1-a1.log",
		"/tmp/run/orchestrator/artifact/T-03/service configuration logs",
	}

	got := bestArtifactCandidateForMissingPath(missingPath, candidates, map[string]struct{}{})
	want := "/tmp/run/orchestrator/artifact/T-03/worker-T-03-a1-a1.log"
	if got != want {
		t.Fatalf("expected exact basename match %q, got %q", want, got)
	}
}

func TestRunWorkerTaskRetriesNmapOnceOnHostTimeout(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-nmap-retry"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	binDir := t.TempDir()
	stateFile := filepath.Join(binDir, "nmap_calls")
	nmapPath := filepath.Join(binDir, "nmap")
	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"state_file=\"" + stateFile + "\"\n" +
		"count=0\n" +
		"if [[ -f \"$state_file\" ]]; then count=$(cat \"$state_file\"); fi\n" +
		"count=$((count + 1))\n" +
		"echo \"$count\" > \"$state_file\"\n" +
		"if [[ \"$count\" -eq 1 ]]; then\n" +
		"  echo \"Nmap scan report for 127.0.0.1\"\n" +
		"  echo \"Skipping host 127.0.0.1 due to host timeout\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"Nmap scan report for 127.0.0.1\"\n" +
		"echo \"PORT   STATE SERVICE\"\n" +
		"echo \"80/tcp open  http\"\n"
	if err := os.WriteFile(nmapPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile nmap script: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "scan service versions",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"scan done"},
		FailWhen:          []string{"scan failed"},
		ExpectedArtifacts: []string{"scan.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "nmap",
			Args:    []string{"-sV", "-Pn", "127.0.0.1"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   2 * time.Minute,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	manager := NewManager(base)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	foundRetryNote := false
	for _, event := range events {
		if event.Type != EventTypeTaskProgress {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		msg := strings.TrimSpace(toString(payload["message"]))
		if strings.Contains(msg, "retrying once with relaxed evidence profile") {
			foundRetryNote = true
			break
		}
	}
	if !foundRetryNote {
		t.Fatalf("expected nmap retry progress note")
	}
}

func TestRunWorkerTaskFailsNmapWhenHostTimeoutHasNoEvidence(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-nmap-timeout-no-evidence"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	binDir := t.TempDir()
	nmapPath := filepath.Join(binDir, "nmap")
	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"echo \"Nmap scan report for 127.0.0.1\"\n" +
		"echo \"Skipping host 127.0.0.1 due to host timeout\"\n"
	if err := os.WriteFile(nmapPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile nmap script: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "scan service versions",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"scan done"},
		FailWhen:          []string{"scan failed"},
		ExpectedArtifacts: []string{"scan.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "nmap",
			Args:    []string{"-sV", "-Pn", "127.0.0.1"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   2 * time.Minute,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err == nil {
		t.Fatalf("expected RunWorkerTask failure for timeout without evidence")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "host timeout") {
		t.Fatalf("unexpected error: %v", err)
	}

	manager := NewManager(base)
	events, eventsErr := manager.Events(runID, 0)
	if eventsErr != nil {
		t.Fatalf("Events: %v", eventsErr)
	}
	failedEvent, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failedEvent.Payload) > 0 {
		_ = json.Unmarshal(failedEvent.Payload, &payload)
	}
	if got := strings.TrimSpace(toString(payload["reason"])); got != WorkerFailureInsufficientEvidence {
		t.Fatalf("expected failure reason %q, got %q", WorkerFailureInsufficientEvidence, got)
	}
}

func TestValidateVulnerabilityEvidenceAuthenticityRejectsPlaceholder(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-vuln",
		Goal:              "Map discovered service versions to known CVEs",
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"No evidence"},
		ExpectedArtifacts: []string{"vulnerability report"},
		RiskLevel:         string(RiskReconReadonly),
	}
	output := []byte("Mapping service versions to CVEs...\nExample: SSH 7.6p1 may be vulnerable to CVE-2019-6111\n")
	err := validateVulnerabilityEvidenceAuthenticity(task, "python3", []string{"-c", "print('Example: ...')"}, output)
	if err == nil {
		t.Fatalf("expected placeholder vulnerability evidence rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "placeholder") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveTaskTargetAttributionFromDependencyResolvedTarget(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-target-attribution-from-dep"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	artifactDir := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, "T-02")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stored := persistedTargetAttribution{
		targetAttribution: targetAttribution{
			Target:     "192.168.50.185",
			Confidence: "high",
			Source:     "resolver_task",
		},
		RunID:  runID,
		TaskID: "T-02",
	}
	if err := WriteJSONAtomic(filepath.Join(artifactDir, "resolved_target.json"), stored); err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}

	task := TaskSpec{
		TaskID:            "T-04",
		Goal:              "Map discovered service versions to CVEs",
		Targets:           []string{"192.168.50.0/24"},
		DependsOn:         []string{"T-02"},
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability_map.log"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"192.168.50.0/24"}})
	attr, err := resolveTaskTargetAttribution(WorkerRunConfig{SessionsDir: base, RunID: runID}, task, scopePolicy)
	if err != nil {
		t.Fatalf("resolveTaskTargetAttribution: %v", err)
	}
	if attr.Target != "192.168.50.185" || attr.Confidence != "high" {
		t.Fatalf("unexpected attribution: %#v", attr)
	}
}

func TestResolveTaskTargetAttributionFailsWithoutEvidence(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-target-attribution-missing"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	task := TaskSpec{
		TaskID:            "T-04",
		Goal:              "Map discovered service versions to CVEs",
		Targets:           []string{"192.168.50.0/24"},
		DependsOn:         []string{"T-02"},
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability_map.log"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"192.168.50.0/24"}})
	_, err := resolveTaskTargetAttribution(WorkerRunConfig{SessionsDir: base, RunID: runID}, task, scopePolicy)
	if err == nil {
		t.Fatalf("expected attribution resolution failure")
	}
}

func TestEnforceAttributedCommandTargetRewritesNmapCIDRTarget(t *testing.T) {
	t.Parallel()

	args, _, rewritten := enforceAttributedCommandTarget("nmap", []string{
		"-n", "--top-ports", "20", "-oA", "/tmp/scan", "192.168.50.0/24",
	}, "192.168.50.185")
	if !rewritten {
		t.Fatalf("expected attributed target rewrite")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "192.168.50.0/24") || !strings.Contains(joined, "192.168.50.185") {
		t.Fatalf("expected rewritten target, got args=%#v", args)
	}
	if !strings.Contains(joined, "-oA /tmp/scan") {
		t.Fatalf("expected output path preserved, got args=%#v", args)
	}
}

func TestRunWorkerTaskVulnTaskFailsWhenTargetAttributionUnresolved(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-vuln-target-attribution-fail"
	taskID := "t-vuln"
	workerID := "worker-t-vuln-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"192.168.50.0/24"}})

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "Map discovered services to CVEs",
		Targets:           []string{"192.168.50.0/24"},
		DependsOn:         []string{"T-02"},
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability mapping"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "nmap",
			Args:    []string{"--script", "vuln and safe", "192.168.50.0/24"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   30 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err == nil {
		t.Fatalf("expected RunWorkerTask failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "attribution") {
		t.Fatalf("unexpected error: %v", err)
	}

	manager := NewManager(base)
	events, eventsErr := manager.Events(runID, 0)
	if eventsErr != nil {
		t.Fatalf("Events: %v", eventsErr)
	}
	failedEvent, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failedEvent.Payload) > 0 {
		_ = json.Unmarshal(failedEvent.Payload, &payload)
	}
	if got := strings.TrimSpace(toString(payload["reason"])); got != WorkerFailureInsufficientEvidence {
		t.Fatalf("expected failure reason %q, got %q", WorkerFailureInsufficientEvidence, got)
	}
}

func TestEnsureVulnerabilityEvidenceActionAddsVulnScriptForNmapVulnTask(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-vuln",
		Goal:              "Map discovered service versions to known vulnerabilities and CVEs",
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability mapping"},
		RiskLevel:         string(RiskReconReadonly),
	}
	args, note, rewritten := ensureVulnerabilityEvidenceAction(task, "nmap", []string{"-sV", "-Pn", "192.168.50.1"})
	if !rewritten {
		t.Fatalf("expected vulnerability evidence rewrite")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--script vuln and safe") || !strings.Contains(joined, "--script-timeout 20s") {
		t.Fatalf("expected vuln script + timeout in args, got %#v", args)
	}
	if !strings.Contains(strings.ToLower(note), "enforced vulnerability evidence profile") {
		t.Fatalf("unexpected note: %q", note)
	}
}

func TestEnsureVulnerabilityEvidenceActionReplacesSafeScriptWithoutDuplicates(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-vuln",
		Goal:              "Map discovered service versions to known vulnerabilities and CVEs",
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability mapping"},
		RiskLevel:         string(RiskReconReadonly),
	}
	args, _, rewritten := ensureVulnerabilityEvidenceAction(task, "nmap", []string{
		"-sV", "-Pn", "--script", "safe", "--script-timeout", "35s", "192.168.50.185",
	})
	if !rewritten {
		t.Fatalf("expected vulnerability evidence rewrite")
	}
	joined := strings.Join(args, " ")
	scriptOptionCount := 0
	for _, arg := range args {
		if strings.TrimSpace(arg) == "--script" {
			scriptOptionCount++
		}
	}
	if scriptOptionCount != 1 {
		t.Fatalf("expected exactly one --script option, got args=%#v", args)
	}
	if !strings.Contains(joined, "--script vuln and safe") {
		t.Fatalf("expected rewritten vuln script token, got args=%#v", args)
	}
	if !strings.Contains(joined, "--script-timeout 20s") {
		t.Fatalf("expected rewritten script-timeout 20s, got args=%#v", args)
	}
}

func TestEnsureVulnerabilityEvidenceActionSkipsReportTasks(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-report",
		Goal:              "Generate final OWASP report from vulnerability scan artifacts",
		DependsOn:         []string{"T-02", "T-03"},
		DoneWhen:          []string{"OWASP report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
	}
	args, note, rewritten := ensureVulnerabilityEvidenceAction(task, "nmap", []string{"-sV", "-Pn", "192.168.50.1"})
	if rewritten {
		t.Fatalf("did not expect vuln evidence rewrite for report synthesis task")
	}
	if len(args) != 3 || note != "" {
		t.Fatalf("unexpected output: args=%#v note=%q", args, note)
	}
}

func TestTaskRequiresReportSynthesisDoesNotMatchCVEMappingTask(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:    "T-03",
		Title:     "Vulnerability Mapping Using CVE Database",
		Goal:      "Map discovered services and versions to known CVEs using safe tools.",
		Strategy:  "Use cve-search or similar tooling to match service versions with CVEs",
		DependsOn: []string{"T-02"},
		DoneWhen: []string{
			"CVE mapping completed for all identified services",
		},
		ExpectedArtifacts: []string{
			"CVE mapping report per service",
			"Vulnerability summary table",
		},
	}
	if taskRequiresReportSynthesis(task) {
		t.Fatalf("did not expect CVE mapping task to be classified as report synthesis")
	}
}

func TestTaskRequiresReportSynthesisDoesNotMatchLocalArchiveReportTask(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:    "T-05",
		Title:     "Write ZIP Recovery Report",
		Goal:      "Write a report summarizing password recovery and extracted file contents.",
		Strategy:  "recon_readonly",
		DependsOn: []string{"T-04"},
		DoneWhen: []string{
			"ZIP report generated",
		},
		ExpectedArtifacts: []string{
			"zip_crack_report.md",
		},
	}
	if taskRequiresReportSynthesis(task) {
		t.Fatalf("did not expect local archive report task to be classified as OWASP report synthesis")
	}
}

func TestAdaptWeakReportActionRewritesCatToLocalSynthesis(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-report-rewrite"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
	}
	task := TaskSpec{
		TaskID:            "T-04",
		Goal:              "Produce a final OWASP assessment report with findings, evidence, and remediation guidance",
		Targets:           []string{"192.168.50.1"},
		DependsOn:         []string{"T-02", "T-03"},
		DoneWhen:          []string{"OWASP report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"192.168.50.1"}})
	cmd, args, note, rewritten := adaptWeakReportAction(cfg, task, scopePolicy, "cat", []string{"/tmp/service_scan.log", "/tmp/vuln_scan.log"}, targetAttribution{})
	if !rewritten {
		t.Fatalf("expected weak report command rewrite")
	}
	if cmd != "python3" {
		t.Fatalf("expected python3 rewrite command, got %q", cmd)
	}
	if len(args) < 10 || args[0] != "-c" {
		t.Fatalf("unexpected rewrite args: %#v", args)
	}
	if !strings.Contains(args[1], "OWASP-Style Security Assessment Report") {
		t.Fatalf("expected synthesis script in args[1]")
	}
	if args[2] != runID || args[3] != task.TaskID {
		t.Fatalf("expected run/task identifiers in args, got %#v", args[2:4])
	}
	if gotRoot := args[7]; gotRoot != BuildRunPaths(base, runID).ArtifactDir {
		t.Fatalf("expected artifact root %q, got %q", BuildRunPaths(base, runID).ArtifactDir, gotRoot)
	}
	if args[8] != "T-02" || args[9] != "T-03" {
		t.Fatalf("expected dependency task ids in args, got %#v", args[8:])
	}
	if !strings.Contains(strings.ToLower(note), "rewrote weak report command") {
		t.Fatalf("expected rewrite note, got %q", note)
	}
}

func TestAdaptWeakReportActionRewritesPythonScriptCommand(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-report-rewrite-python"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
	}
	task := TaskSpec{
		TaskID:            "T-04",
		Goal:              "Generate final OWASP report from prior artifacts",
		Targets:           []string{"192.168.50.1"},
		DependsOn:         []string{"T-02", "T-03"},
		DoneWhen:          []string{"OWASP report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"192.168.50.1"}})
	cmd, args, _, rewritten := adaptWeakReportAction(cfg, task, scopePolicy, "python3", []string{"generate_owasp_report.py", "--input", "scan.json"}, targetAttribution{})
	if !rewritten {
		t.Fatalf("expected python report command rewrite")
	}
	if cmd != "python3" || len(args) < 2 || args[0] != "-c" {
		t.Fatalf("unexpected rewrite output: cmd=%q args=%#v", cmd, args)
	}
}

func TestAdaptWeakReportActionSkipsNonSecurityLocalReportTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-report-no-rewrite-local"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
	}
	task := TaskSpec{
		TaskID:            "T-05",
		Title:             "Write ZIP Recovery Report",
		Goal:              "Write a final report for recovered password and extracted files",
		Strategy:          "recon_readonly",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-04"},
		DoneWhen:          []string{"zip report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"zip_crack_report.md"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"127.0.0.1"}})
	cmd, args, note, rewritten := adaptWeakReportAction(cfg, task, scopePolicy, "cat", []string{"recovered_password.txt", "extracted_files.txt"}, targetAttribution{})
	if rewritten {
		t.Fatalf("did not expect non-security local report task to be rewritten")
	}
	if cmd != "cat" || len(args) != 2 || note != "" {
		t.Fatalf("unexpected output when rewrite should be skipped: cmd=%q args=%#v note=%q", cmd, args, note)
	}
}

func TestRunWorkerTaskRewritesWeakReportActionAndSynthesizesEvidence(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-report-synthesis"
	taskID := "T-04"
	workerID := "worker-t4-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"192.168.50.1"}})

	artifactRoot := BuildRunPaths(base, runID).ArtifactDir
	serviceArtifact := filepath.Join(artifactRoot, "T-02", "service_scan.log")
	if err := os.MkdirAll(filepath.Dir(serviceArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll service artifact dir: %v", err)
	}
	if err := os.WriteFile(serviceArtifact, []byte("Nmap scan report for 192.168.50.1\nPORT STATE SERVICE\n80/tcp open http\n"), 0o644); err != nil {
		t.Fatalf("WriteFile service artifact: %v", err)
	}
	vulnArtifact := filepath.Join(artifactRoot, "T-03", "vuln_scan.log")
	if err := os.MkdirAll(filepath.Dir(vulnArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll vuln artifact dir: %v", err)
	}
	if err := os.WriteFile(vulnArtifact, []byte("NSE: [http-vuln-cve2010-0738] host appears vulnerable to CVE-2010-0738\n"), 0o644); err != nil {
		t.Fatalf("WriteFile vuln artifact: %v", err)
	}

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "Produce a final OWASP report from prior artifacts and include remediation guidance",
		Targets:           []string{"192.168.50.1"},
		DependsOn:         []string{"T-02", "T-03"},
		DoneWhen:          []string{"OWASP report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "cat",
			Args:    []string{"/tmp/service_scan_output.txt", "/tmp/vuln_scan_output.txt"},
		},
		Budget: TaskBudget{
			MaxSteps:     3,
			MaxToolCalls: 3,
			MaxRuntime:   30 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	manager := NewManager(base)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}

	foundRewriteNote := false
	foundPythonExec := false
	for _, event := range events {
		if event.Type != EventTypeTaskProgress {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		msg := strings.ToLower(strings.TrimSpace(toString(payload["message"])))
		cmd := strings.TrimSpace(toString(payload["command"]))
		if strings.Contains(msg, "rewrote weak report command") {
			foundRewriteNote = true
		}
		if strings.Contains(msg, "executing action command") && cmd == "python3" {
			foundPythonExec = true
		}
	}
	if !foundRewriteNote {
		t.Fatalf("expected report rewrite progress note")
	}
	if !foundPythonExec {
		t.Fatalf("expected rewritten report task to execute with python3")
	}

	completedEvent, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed event")
	}
	completedPayload := map[string]any{}
	if len(completedEvent.Payload) > 0 {
		_ = json.Unmarshal(completedEvent.Payload, &completedPayload)
	}
	logPath := strings.TrimSpace(toString(completedPayload["log_path"]))
	if logPath == "" {
		t.Fatalf("expected completed payload log_path")
	}
	reportOutput, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile command log: %v", err)
	}
	content := string(reportOutput)
	if !strings.Contains(content, "# OWASP-Style Security Assessment Report") {
		t.Fatalf("expected OWASP report header, got: %q", content)
	}
	if !strings.Contains(content, "## Findings") || !strings.Contains(content, "CVE-2010-0738") {
		t.Fatalf("expected findings with CVE evidence, got: %q", content)
	}
	if !strings.Contains(content, serviceArtifact) || !strings.Contains(content, vulnArtifact) {
		t.Fatalf("expected synthesized evidence paths in output, got: %q", content)
	}
}

func TestRunWorkerTaskReportSynthesisNoFindingsIncludesClearSummary(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-report-no-findings"
	taskID := "T-04"
	workerID := "worker-t4-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"192.168.50.185"}})

	artifactRoot := BuildRunPaths(base, runID).ArtifactDir
	serviceArtifact := filepath.Join(artifactRoot, "T-02", "service_scan.log")
	if err := os.MkdirAll(filepath.Dir(serviceArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll service artifact dir: %v", err)
	}
	if err := os.WriteFile(serviceArtifact, []byte("Nmap scan report for 192.168.50.185\nPORT STATE SERVICE\n80/tcp closed http\n"), 0o644); err != nil {
		t.Fatalf("WriteFile service artifact: %v", err)
	}
	vulnArtifact := filepath.Join(artifactRoot, "T-03", "vuln_scan.log")
	if err := os.MkdirAll(filepath.Dir(vulnArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll vuln artifact dir: %v", err)
	}
	if err := os.WriteFile(vulnArtifact, []byte("Nmap done: 1 IP address (1 host up) scanned in 0.20 seconds\n"), 0o644); err != nil {
		t.Fatalf("WriteFile vuln artifact: %v", err)
	}

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "Produce a final OWASP report from prior artifacts and include remediation guidance",
		Targets:           []string{"192.168.50.185"},
		DependsOn:         []string{"T-02", "T-03"},
		DoneWhen:          []string{"OWASP report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "cat",
			Args:    []string{"/tmp/service_scan_output.txt", "/tmp/vuln_scan_output.txt"},
		},
		Budget: TaskBudget{
			MaxSteps:     3,
			MaxToolCalls: 3,
			MaxRuntime:   30 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	completedEvent, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed event")
	}
	completedPayload := map[string]any{}
	if len(completedEvent.Payload) > 0 {
		_ = json.Unmarshal(completedEvent.Payload, &completedPayload)
	}
	logPath := strings.TrimSpace(toString(completedPayload["log_path"]))
	if logPath == "" {
		t.Fatalf("expected completed payload log_path")
	}
	reportOutput, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile command log: %v", err)
	}
	content := string(reportOutput)
	if !strings.Contains(content, "## Executive Summary") {
		t.Fatalf("expected executive summary section, got: %q", content)
	}
	if !strings.Contains(content, "No evidence-backed vulnerabilities identified in this run.") {
		t.Fatalf("expected explicit no-findings summary, got: %q", content)
	}
	if !strings.Contains(content, "## Test Execution Summary") {
		t.Fatalf("expected test execution summary section, got: %q", content)
	}
	if !strings.Contains(content, "No finding-specific remediation generated because no evidence-backed vulnerabilities were identified.") {
		t.Fatalf("expected no-findings remediation guidance, got: %q", content)
	}
}

func TestLocalArchiveWorkflowArtifactsAndReportNoRewrite(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-local-archive-workflow"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	workspace := filepath.Join(base, "archive-workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	secretFile := filepath.Join(workspace, "extracted_secret", "secret_text.txt")
	if err := os.MkdirAll(filepath.Dir(secretFile), 0o755); err != nil {
		t.Fatalf("MkdirAll secret dir: %v", err)
	}
	if err := os.WriteFile(secretFile, []byte("This is a very very secret file about a cow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile secret text: %v", err)
	}

	t2 := TaskSpec{
		TaskID:            "t2",
		Goal:              "dictionary recovery via john output synthesis",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"john outputs available"},
		FailWhen:          []string{"john flow failed"},
		ExpectedArtifacts: []string{"john_run.log", "john_show.txt", "john_status.txt", "john_secret.pot"},
		RiskLevel:         string(RiskActiveProbe),
		Action: TaskAction{
			Type:       "command",
			Command:    "bash",
			Args:       []string{"-lc", "set -euo pipefail; printf 'secret.zip/secret_text.txt:telefo01:secret_text.txt:secret.zip::secret.zip\\n\\n1 password hash cracked, 0 left\\n' > john_show.txt; printf 'source=default_pot\\n' > john_status.txt; : > john_run.log; printf 'fake-pot\\n' > john_secret.pot"},
			WorkingDir: workspace,
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   20 * time.Second,
		},
	}
	t3 := TaskSpec{
		TaskID:            "t3",
		Goal:              "dictionary recovery via fcrackzip output synthesis",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"fcrackzip outputs available"},
		FailWhen:          []string{"fcrackzip flow failed"},
		ExpectedArtifacts: []string{"fcrackzip_output.txt", "fcrackzip_status.txt"},
		RiskLevel:         string(RiskActiveProbe),
		Action: TaskAction{
			Type:       "command",
			Command:    "bash",
			Args:       []string{"-lc", "set -euo pipefail; : > fcrackzip_output.txt; printf 'dictionary=/usr/share/john/password.lst\\n' > fcrackzip_status.txt"},
			WorkingDir: workspace,
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   20 * time.Second,
		},
	}
	t4 := TaskSpec{
		TaskID:            "t4",
		Goal:              "resolve password and summarize extracted content",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"t2", "t3"},
		DoneWhen:          []string{"recovery outputs available"},
		FailWhen:          []string{"recovery aggregation failed"},
		ExpectedArtifacts: []string{"candidate_passwords.txt", "recovered_password.txt", "extraction_status.txt", "extract.log", "extracted_files.txt", "extracted_preview.txt"},
		RiskLevel:         string(RiskActiveProbe),
		Action: TaskAction{
			Type:       "command",
			Command:    "bash",
			Args:       []string{"-lc", "set -euo pipefail; : > candidate_passwords.txt; awk -F: 'NF>=2 && length($2)>0 {print $2; exit}' john_show.txt > recovered_password.txt; printf 'password_recovered_and_extracted\\n' > extraction_status.txt; : > extract.log; printf 'extracted_secret/secret_text.txt\\n' > extracted_files.txt; : > extracted_preview.txt; while IFS= read -r f; do printf '===== %s\\n' \"$f\" >> extracted_preview.txt; sed -n '1,120p' \"$f\" >> extracted_preview.txt || true; printf '\\n' >> extracted_preview.txt; done < extracted_files.txt"},
			WorkingDir: workspace,
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   20 * time.Second,
		},
	}
	t5 := TaskSpec{
		TaskID:            "t5",
		Title:             "Write ZIP Recovery Report",
		Goal:              "Write a final report for recovered password and extracted files",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"t4"},
		DoneWhen:          []string{"zip report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"zip_crack_report.md"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:       "command",
			Command:    "bash",
			Args:       []string{"-lc", "set -euo pipefail; PASS=not_recovered; if [ -s recovered_password.txt ]; then read -r PASS < recovered_password.txt; fi; STATUS=unknown; if [ -f extraction_status.txt ]; then read -r STATUS < extraction_status.txt; fi; { echo '# ZIP Recovery Report'; echo; echo '## Outcome'; printf -- '- Recovered password: %s\\n' \"$PASS\"; printf -- '- Extraction status: %s\\n' \"$STATUS\"; echo; echo '## Extracted Files'; if [ -f extracted_files.txt ]; then sed 's/^/- /' extracted_files.txt; else echo '- none'; fi; } > zip_crack_report.md"},
			WorkingDir: workspace,
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   20 * time.Second,
		},
	}

	for _, task := range []TaskSpec{t2, t3, t4, t5} {
		taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, task.TaskID+".json")
		if err := WriteJSONAtomic(taskPath, task); err != nil {
			t.Fatalf("WriteJSONAtomic task %s: %v", task.TaskID, err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i, taskID := range []string{"t2", "t3"} {
		wg.Add(1)
		go func(id string, idx int) {
			defer wg.Done()
			errCh <- RunWorkerTask(WorkerRunConfig{
				SessionsDir: base,
				RunID:       runID,
				TaskID:      id,
				WorkerID:    "worker-" + id + "-a1",
				Attempt:     idx + 1,
			})
		}(taskID, i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("RunWorkerTask parallel crack task failed: %v", err)
		}
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      "t4",
		WorkerID:    "worker-t4-a1",
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask t4: %v", err)
	}
	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      "t5",
		WorkerID:    "worker-t5-a1",
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask t5: %v", err)
	}

	artifactRoot := BuildRunPaths(base, runID).ArtifactDir
	checkContains := func(path, contains string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", path, err)
		}
		text := string(data)
		if !strings.Contains(text, contains) {
			t.Fatalf("expected %s to contain %q, got %q", path, contains, text)
		}
		if strings.Contains(text, "no command output captured") {
			t.Fatalf("expected %s to contain real content, got fallback placeholder", path)
		}
	}

	checkContains(filepath.Join(artifactRoot, "t2", "john_show.txt"), "telefo01")
	checkContains(filepath.Join(artifactRoot, "t4", "recovered_password.txt"), "telefo01")
	checkContains(filepath.Join(artifactRoot, "t4", "extraction_status.txt"), "password_recovered_and_extracted")
	checkContains(filepath.Join(artifactRoot, "t4", "extracted_preview.txt"), "This is a very very secret file about a cow")
	checkContains(filepath.Join(artifactRoot, "t5", "zip_crack_report.md"), "# ZIP Recovery Report")
	checkContains(filepath.Join(artifactRoot, "t5", "zip_crack_report.md"), "Recovered password: telefo01")

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	for _, event := range events {
		if event.Type != EventTypeTaskProgress || event.TaskID != "t5" {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		msg := strings.ToLower(strings.TrimSpace(toString(payload["message"])))
		if strings.Contains(msg, "rewrote weak report command") {
			t.Fatalf("did not expect report rewrite note for local archive report task")
		}
	}
}

func TestRunWorkerTaskEnforcesVulnScriptForNmapVulnTask(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-vuln-enforce-script"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	binDir := t.TempDir()
	nmapPath := filepath.Join(binDir, "nmap")
	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"joined=\"$*\"\n" +
		"if [[ \"$joined\" != *\"--script vuln and safe\"* ]]; then\n" +
		"  echo \"missing vuln script\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"Nmap scan report for 127.0.0.1\"\n" +
		"echo \"PORT   STATE SERVICE\"\n" +
		"echo \"80/tcp open  http\"\n" +
		"echo \"NSE: [http-vuln-cve2010-0738] host appears vulnerable to CVE-2010-0738\"\n"
	if err := os.WriteFile(nmapPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile nmap script: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "map discovered service versions to known CVEs",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability_map.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "nmap",
			Args:    []string{"-sV", "-Pn", "127.0.0.1"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   30 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask: %v", err)
	}

	manager := NewManager(base)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	foundEnforcementNote := false
	completedEvent, _ := firstEventByType(events, EventTypeTaskCompleted)
	for _, event := range events {
		if event.Type != EventTypeTaskProgress {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		msg := strings.ToLower(strings.TrimSpace(toString(payload["message"])))
		if strings.Contains(msg, "enforced vulnerability evidence profile") {
			foundEnforcementNote = true
			break
		}
	}
	if !foundEnforcementNote {
		t.Fatalf("expected vulnerability evidence enforcement note")
	}
	completedPayload := map[string]any{}
	if len(completedEvent.Payload) > 0 {
		_ = json.Unmarshal(completedEvent.Payload, &completedPayload)
	}
	logPath := strings.TrimSpace(toString(completedPayload["log_path"]))
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile command log: %v", err)
	}
	if !strings.Contains(string(logData), "CVE-2010-0738") {
		t.Fatalf("expected CVE evidence in command log, got: %q", string(logData))
	}
}

func TestAdaptWeakVulnerabilityActionRewritesCatToNmap(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-vuln",
		Goal:              "Map discovered services to known vulnerabilities and CVEs",
		Targets:           []string{"192.168.50.1"},
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability mapping"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"192.168.50.1"}})
	cmd, args, note, rewritten := adaptWeakVulnerabilityAction(task, scopePolicy, "cat", []string{"/tmp/nmap_output.xml"})
	if !rewritten {
		t.Fatalf("expected weak vulnerability command rewrite")
	}
	if cmd != "nmap" {
		t.Fatalf("expected nmap rewrite command, got %q", cmd)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--script vuln and safe") ||
		!strings.Contains(joined, "--script-timeout 20s") ||
		!strings.Contains(joined, "--top-ports 20") ||
		!strings.Contains(joined, "192.168.50.1") {
		t.Fatalf("unexpected rewrite args: %#v", args)
	}
	if !strings.Contains(strings.ToLower(note), "rewrote weak vulnerability-mapping command") {
		t.Fatalf("expected rewrite note, got %q", note)
	}
}

func TestAdaptWeakVulnerabilityActionRewritesPythonWrapperToNmap(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-vuln",
		Goal:              "Map discovered services to known vulnerabilities and CVEs",
		Targets:           []string{"192.168.50.1"},
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability mapping"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"192.168.50.1"}})
	cmd, args, _, rewritten := adaptWeakVulnerabilityAction(task, scopePolicy, "python3", []string{"-c", "import subprocess; subprocess.run(['cve-search'])"})
	if !rewritten {
		t.Fatalf("expected python vuln wrapper rewrite")
	}
	if cmd != "nmap" || !strings.Contains(strings.Join(args, " "), "--script vuln and safe") {
		t.Fatalf("unexpected rewrite output: cmd=%q args=%#v", cmd, args)
	}
}

func TestAdaptWeakVulnerabilityActionSkipsReportSynthesisTasks(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-report",
		Goal:              "Produce final OWASP report and map CVEs to remediation evidence",
		Targets:           []string{"192.168.50.1"},
		DependsOn:         []string{"T-02", "T-03"},
		DoneWhen:          []string{"OWASP report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"192.168.50.1"}})
	cmd, args, note, rewritten := adaptWeakVulnerabilityAction(task, scopePolicy, "cat", []string{"/tmp/vuln_scan_output.log"})
	if rewritten {
		t.Fatalf("did not expect vulnerability rewrite for report synthesis task")
	}
	if cmd != "cat" || len(args) != 1 || note != "" {
		t.Fatalf("unexpected rewrite output: cmd=%q args=%#v note=%q", cmd, args, note)
	}
}

func TestAdaptWeakVulnerabilityActionKeepsConcreteTooling(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-vuln",
		Goal:              "Identify CVEs for exposed services",
		Targets:           []string{"192.168.50.1"},
		DoneWhen:          []string{"CVEs identified"},
		FailWhen:          []string{"mapping failed"},
		ExpectedArtifacts: []string{"vulnerability mapping"},
		RiskLevel:         string(RiskReconReadonly),
	}
	scopePolicy := NewScopePolicy(Scope{Targets: []string{"192.168.50.1"}})
	cmd, args, note, rewritten := adaptWeakVulnerabilityAction(task, scopePolicy, "nmap", []string{"--script", "vuln and safe", "192.168.50.1"})
	if rewritten {
		t.Fatalf("did not expect rewrite for concrete vuln tooling command")
	}
	if cmd != "nmap" || len(args) != 3 || note != "" {
		t.Fatalf("unexpected rewrite output: cmd=%q args=%#v note=%q", cmd, args, note)
	}
}

func TestRequiresCommandScopeValidation(t *testing.T) {
	t.Parallel()

	if requiresCommandScopeValidation("bash", []string{"-lc", "grep -E 'ssh|http' port_service_scan.xml | grep -v '#'"}) {
		t.Fatalf("expected local shell parsing command to skip scope validation")
	}
	if !requiresCommandScopeValidation("bash", []string{"-lc", "nmap -sV 192.168.50.1"}) {
		t.Fatalf("expected network shell command to require scope validation")
	}
	if !requiresCommandScopeValidation("nmap", []string{"-sV", "192.168.50.1"}) {
		t.Fatalf("expected direct network command to require scope validation")
	}
}

func TestNormalizeTaskActionSplitsCommandLine(t *testing.T) {
	t.Parallel()

	action, err := normalizeTaskAction(TaskAction{
		Type:    "command",
		Command: "nmap -sV -Pn 192.168.50.1",
	})
	if err != nil {
		t.Fatalf("normalizeTaskAction: %v", err)
	}
	if action.Command != "nmap" {
		t.Fatalf("expected command nmap, got %q", action.Command)
	}
	if len(action.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(action.Args))
	}
	if action.Args[0] != "-sV" || action.Args[1] != "-Pn" || action.Args[2] != "192.168.50.1" {
		t.Fatalf("unexpected args: %#v", action.Args)
	}
}

func TestNormalizeTaskActionWrapsShellExpressions(t *testing.T) {
	t.Parallel()

	action, err := normalizeTaskAction(TaskAction{
		Type:    "command",
		Command: "grep -E 'router|cisco' /tmp/nmap_results.txt || echo 'No router identified'",
	})
	if err != nil {
		t.Fatalf("normalizeTaskAction: %v", err)
	}
	if action.Command != "bash" {
		t.Fatalf("expected command bash, got %q", action.Command)
	}
	if len(action.Args) != 2 || action.Args[0] != "-lc" {
		t.Fatalf("unexpected bash args: %#v", action.Args)
	}
	if action.Args[1] != "grep -E 'router|cisco' /tmp/nmap_results.txt || echo 'No router identified'" {
		t.Fatalf("unexpected shell payload: %q", action.Args[1])
	}
}

func TestRunWorkerTaskAssistAction(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "use llm reasoning",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "List current directory and then complete.",
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   15 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		seq := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		switch seq {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"list_dir\",\"args\":[\".\"],\"summary\":\"Inspect workspace\"}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"Assist workflow complete\"}"}}]}`))
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err != nil {
		t.Fatalf("RunWorkerTask assist: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	if !hasEventType(events, EventTypeTaskArtifact) {
		t.Fatalf("expected task_artifact event")
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least 2 llm calls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestRunWorkerTaskAssistQuestionAutoRecoversInNonInteractiveMode(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-question"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "use llm reasoning",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "List current directory and then complete.",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   20 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		seq := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		switch seq {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Which directory should I inspect?\"}"}}]}`))
		case 2:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"list_dir\",\"args\":[\".\"],\"summary\":\"Inspect workspace\"}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"Assist workflow complete\"}"}}]}`))
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err != nil {
		t.Fatalf("RunWorkerTask assist: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	for _, event := range events {
		if event.Type != EventTypeTaskFailed {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if got, _ := payload["reason"].(string); got == WorkerFailureAssistNeedsInput {
			t.Fatalf("did not expect %s failure: %#v", WorkerFailureAssistNeedsInput, payload)
		}
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Fatalf("expected at least 3 llm calls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestRunWorkerTaskAssistInvalidToolSpecRecovers(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-tool-recover"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "use llm reasoning",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Inspect files and complete.",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   20 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		seq := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		switch seq {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"tool\",\"tool\":{\"language\":\"python\",\"name\":\"x\",\"purpose\":\"broken\",\"files\":[],\"run\":{\"command\":\"python3\",\"args\":[\"x.py\"]}}}"}}]}`))
		case 2:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"list_dir\",\"args\":[\".\"],\"summary\":\"Inspect workspace\"}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"Assist workflow complete\"}"}}]}`))
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err != nil {
		t.Fatalf("RunWorkerTask assist: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	for _, event := range events {
		if event.Type != EventTypeTaskFailed {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if got, _ := payload["reason"].(string); got == WorkerFailureAssistNoAction {
			t.Fatalf("did not expect %s failure: %#v", WorkerFailureAssistNoAction, payload)
		}
	}
}

func TestRunWorkerTaskAssistToolCanUseReadFileBuiltinWithoutScopeCollision(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-tool-read-file"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "use tool helper and read local artifacts",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Write and read a local helper artifact.",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   20 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	var callCount int32
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch atomic.AddInt32(&callCount, 1) {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"tool\",\"summary\":\"read helper file\",\"tool\":{\"language\":\"bash\",\"name\":\"helper\",\"purpose\":\"read local helper output\",\"files\":[{\"path\":\"notes.log\",\"content\":\"hello\\n\"}],\"run\":{\"command\":\"read_file\",\"args\":[\"tools/notes.log\"]}}}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"done\"}"}}]}`))
		}
	}))
	defer llmServer.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, llmServer.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err != nil {
		t.Fatalf("RunWorkerTask assist: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	for _, event := range events {
		if event.Type != EventTypeTaskFailed {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if got, _ := payload["reason"].(string); got == WorkerFailureScopeDenied {
			t.Fatalf("did not expect %s failure: %#v", WorkerFailureScopeDenied, payload)
		}
	}
}

func TestRunWorkerTaskInvalidActionEmitsFailure(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-invalid"
	taskID := "t1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "invalid action",
		DoneWhen:          []string{"never"},
		FailWhen:          []string{"invalid"},
		ExpectedArtifacts: []string{"none"},
		RiskLevel:         string(RiskReconReadonly),
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   5 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    "worker-t1-a1",
		Attempt:     1,
	})
	if err == nil {
		t.Fatalf("expected error for invalid action")
	}

	manager := NewManager(base)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	failEvent, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failEvent.Payload) > 0 {
		_ = json.Unmarshal(failEvent.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != WorkerFailureInvalidTaskAction {
		t.Fatalf("expected %s reason, got %q", WorkerFailureInvalidTaskAction, got)
	}
	if !hasEventType(events, EventTypeTaskFinding) {
		t.Fatalf("expected failure task_finding event")
	}
}

func TestRunWorkerTaskIdempotentByAttempt(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-idempotent"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	cmd, args := testEchoCommand("worker-idempotent-ok")
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "run once",
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"failed"},
		ExpectedArtifacts: []string{"log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: cmd,
			Args:    args,
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}
	if err := RunWorkerTask(cfg); err != nil {
		t.Fatalf("RunWorkerTask first run: %v", err)
	}
	if err := RunWorkerTask(cfg); err != nil {
		t.Fatalf("RunWorkerTask second run: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	completedCount := 0
	for _, event := range events {
		if event.Type == EventTypeTaskCompleted {
			completedCount++
		}
	}
	if completedCount != 1 {
		t.Fatalf("expected exactly one task_completed event, got %d", completedCount)
	}
}

func TestRunWorkerTaskScopeDenied(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-scope-denied"
	taskID := "t1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Networks: []string{"192.168.50.0/24"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{},
	}
	planPath := filepath.Join(BuildRunPaths(base, runID).PlanDir, "plan.json")
	if err := WriteJSONAtomic(planPath, plan); err != nil {
		t.Fatalf("WriteJSONAtomic plan: %v", err)
	}
	cmd, args := testEchoCommand("scope-denied")
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "scope denied",
		Targets:           []string{"10.0.0.5"},
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"failed"},
		ExpectedArtifacts: []string{"log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: cmd,
			Args:    args,
		},
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   5 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}
	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    "worker-t1-a1",
		Attempt:     1,
		Permission:  PermissionDefault,
	})
	if err == nil {
		t.Fatalf("expected scope denied error")
	}
	events, evErr := NewManager(base).Events(runID, 0)
	if evErr != nil {
		t.Fatalf("Events: %v", evErr)
	}
	failEvent, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failEvent.Payload) > 0 {
		_ = json.Unmarshal(failEvent.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != WorkerFailureScopeDenied {
		t.Fatalf("expected %s reason, got %q", WorkerFailureScopeDenied, got)
	}
}

func TestRunWorkerTaskNetworkCommandInjectsTaskTargetBeforeScopeCheck(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-network-fallback"
	taskID := "t1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{},
	}
	planPath := filepath.Join(BuildRunPaths(base, runID).PlanDir, "plan.json")
	if err := WriteJSONAtomic(planPath, plan); err != nil {
		t.Fatalf("WriteJSONAtomic plan: %v", err)
	}
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "network command fallback",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"failed"},
		ExpectedArtifacts: []string{"log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:           "command",
			Command:        "nmap",
			TimeoutSeconds: 1,
		},
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   2 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	_ = RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    "worker-t1-a1",
		Attempt:     1,
		Permission:  PermissionDefault,
	})

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskStarted) {
		t.Fatalf("expected task_started event (scope check should pass with injected target)")
	}
	for _, event := range events {
		if event.Type != EventTypeTaskFailed {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if got, _ := payload["reason"].(string); got == WorkerFailureScopeDenied {
			t.Fatalf("did not expect scope_denied after target injection: %#v", payload)
		}
	}
}

func TestRunWorkerTaskPolicyDenied(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-policy-denied"
	taskID := "t1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{},
	}
	planPath := filepath.Join(BuildRunPaths(base, runID).PlanDir, "plan.json")
	if err := WriteJSONAtomic(planPath, plan); err != nil {
		t.Fatalf("WriteJSONAtomic plan: %v", err)
	}
	cmd, args := testEchoCommand("policy-denied")
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "policy denied",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"failed"},
		ExpectedArtifacts: []string{"log"},
		RiskLevel:         string(RiskExploitControlled),
		Action: TaskAction{
			Type:    "command",
			Command: cmd,
			Args:    args,
		},
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   5 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}
	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    "worker-t1-a1",
		Attempt:     1,
		Permission:  PermissionReadonly,
	})
	if err == nil {
		t.Fatalf("expected policy denied error")
	}
	events, evErr := NewManager(base).Events(runID, 0)
	if evErr != nil {
		t.Fatalf("Events: %v", evErr)
	}
	failEvent, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failEvent.Payload) > 0 {
		_ = json.Unmarshal(failEvent.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != WorkerFailurePolicyDenied {
		t.Fatalf("expected %s reason, got %q", WorkerFailurePolicyDenied, got)
	}
}

func TestRunWorkerTaskTimeoutEmitsFailure(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-timeout"
	taskID := "t1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	cmd, args := longSleepCommand(4)
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "timeout",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"timeout"},
		ExpectedArtifacts: []string{"log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:           "command",
			Command:        cmd,
			Args:           args,
			TimeoutSeconds: 1,
		},
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}
	start := time.Now()
	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    "worker-t1-a1",
		Attempt:     1,
		Permission:  PermissionDefault,
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("expected timeout cleanup quickly, elapsed=%s", elapsed)
	}
	events, evErr := NewManager(base).Events(runID, 0)
	if evErr != nil {
		t.Fatalf("Events: %v", evErr)
	}
	failEvent, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failEvent.Payload) > 0 {
		_ = json.Unmarshal(failEvent.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != WorkerFailureCommandTimeout {
		t.Fatalf("expected %s reason, got %q", WorkerFailureCommandTimeout, got)
	}
	if hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("did not expect task_completed event after timeout")
	}
}

func TestParseWorkerRunConfig(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		OrchSessionsDirEnv: "/tmp/sessions",
		OrchRunIDEnv:       "run-1",
		OrchTaskIDEnv:      "task-1",
		OrchWorkerIDEnv:    "worker-1",
		OrchAttemptEnv:     "2",
		OrchPermissionEnv:  string(PermissionAll),
		OrchDisruptiveEnv:  "true",
	}
	cfg := ParseWorkerRunConfig(func(key string) string {
		return env[key]
	})
	if cfg.SessionsDir != "/tmp/sessions" || cfg.RunID != "run-1" || cfg.TaskID != "task-1" || cfg.WorkerID != "worker-1" || cfg.Attempt != 2 || cfg.Permission != PermissionAll || !cfg.Disruptive {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestCappedOutputBufferTruncates(t *testing.T) {
	t.Parallel()

	buf := newCappedOutputBuffer(12)
	_, _ = buf.Write([]byte("1234567890"))
	_, _ = buf.Write([]byte("ABCDEFGHIJ"))

	out := string(buf.Bytes())
	if len(out) != 12 {
		t.Fatalf("expected capped output length 12, got %d", len(out))
	}
	if !strings.Contains(out, "1234567890") {
		t.Fatalf("expected original prefix in output, got %q", out)
	}
}

func firstEventByType(events []EventEnvelope, eventType string) (EventEnvelope, bool) {
	for _, event := range events {
		if event.Type == eventType {
			return event, true
		}
	}
	return EventEnvelope{}, false
}

func hasEventType(events []EventEnvelope, eventType string) bool {
	_, ok := firstEventByType(events, eventType)
	return ok
}

func testEchoCommand(message string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", "echo " + message}
	}
	return "sh", []string{"-c", "printf '%s\\n' " + message}
}

func testWriteExpectedArtifactsCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", "echo scan-file>port_scan_output.txt && echo service-file>service_version_output.txt"}
	}
	return "sh", []string{"-c", "printf 'scan-file\\n' > port_scan_output.txt; printf 'service-file\\n' > service_version_output.txt"}
}

func longSleepCommand(seconds int) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", "ping -n " + strconv.Itoa(seconds+1) + " 127.0.0.1 >NUL"}
	}
	return "sh", []string{"-c", "sleep " + strconv.Itoa(seconds)}
}

func writeWorkerPlan(t *testing.T, base, runID string, scope Scope) {
	t.Helper()
	plan := RunPlan{
		RunID:           runID,
		Scope:           scope,
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{},
	}
	path := filepath.Join(BuildRunPaths(base, runID).PlanDir, "plan.json")
	if err := WriteJSONAtomic(path, plan); err != nil {
		t.Fatalf("WriteJSONAtomic plan: %v", err)
	}
}
