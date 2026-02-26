package orchestrator

import (
	"compress/gzip"
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

func TestRunWorkerTaskCommandActionExecutesBrowseAndCrawlBuiltins(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		command string
	}{
		{name: "browse", command: "browse"},
		{name: "crawl", command: "crawl"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html><head><title>builtin check</title></head><body>ok</body></html>"))
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			base := t.TempDir()
			runID := "run-worker-task-" + tc.name + "-builtin"
			taskID := "t1"
			workerID := "worker-" + tc.name + "-a1"
			if _, err := EnsureRunLayout(base, runID); err != nil {
				t.Fatalf("EnsureRunLayout: %v", err)
			}
			writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

			task := TaskSpec{
				TaskID:            taskID,
				Goal:              "fetch target web page summary",
				Targets:           []string{"127.0.0.1"},
				DoneWhen:          []string{"page summary captured"},
				FailWhen:          []string{"page summary failed"},
				ExpectedArtifacts: []string{"command log"},
				RiskLevel:         string(RiskReconReadonly),
				Action: TaskAction{
					Type:    "command",
					Command: tc.command,
					Args:    []string{server.URL},
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
			if hasEventType(events, EventTypeTaskFailed) {
				t.Fatalf("did not expect task_failed event for builtin command %s", tc.command)
			}
			artifactEvent, ok := firstEventByType(events, EventTypeTaskArtifact)
			if !ok {
				t.Fatalf("expected task_artifact event")
			}

			payload := map[string]any{}
			if len(artifactEvent.Payload) > 0 {
				_ = json.Unmarshal(artifactEvent.Payload, &payload)
			}
			if got := strings.TrimSpace(toString(payload["command"])); got != tc.command {
				t.Fatalf("expected command %q in artifact payload, got %q", tc.command, got)
			}
			logPath := strings.TrimSpace(toString(payload["path"]))
			if logPath == "" {
				t.Fatalf("task_artifact payload missing path")
			}
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read artifact log: %v", err)
			}
			content := string(data)
			if !strings.Contains(content, "Status: 200") || !strings.Contains(content, "Title: builtin check") {
				t.Fatalf("expected browse/crawl summary output, got: %q", content)
			}
		})
	}
}

func TestRunWorkerTaskCommandActionReportLiteralRewritesToSynthesis(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-task-report-literal"
	taskID := "t-report"
	workerID := "worker-report-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})

	depTaskID := "t-scan"
	depDir := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, depTaskID)
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("MkdirAll depDir: %v", err)
	}
	depArtifact := filepath.Join(depDir, "service_scan.txt")
	depOutput := "" +
		"Nmap scan report for 127.0.0.1\n" +
		"PORT   STATE SERVICE VERSION\n" +
		"80/tcp open  http    Apache httpd 2.2.14\n" +
		"|_http-vuln-cve2010-0738: VULNERABLE\n"
	if err := os.WriteFile(depArtifact, []byte(depOutput), 0o644); err != nil {
		t.Fatalf("WriteFile depArtifact: %v", err)
	}

	task := TaskSpec{
		TaskID:            taskID,
		Title:             "Compile OWASP report",
		Goal:              "Generate final OWASP report from prior scan artifacts",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{depTaskID},
		DoneWhen:          []string{"OWASP report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "report",
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
	if hasEventType(events, EventTypeTaskFailed) {
		t.Fatalf("did not expect task_failed event")
	}

	rewrote := false
	for _, event := range events {
		if event.Type != EventTypeTaskProgress {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		msg := strings.ToLower(strings.TrimSpace(toString(payload["message"])))
		if strings.Contains(msg, "rewrote weak report command (report)") {
			rewrote = true
			break
		}
	}
	if !rewrote {
		t.Fatalf("expected progress note for report command rewrite")
	}

	materializedReport := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, taskID, "owasp_report.md")
	data, err := os.ReadFile(materializedReport)
	if err != nil {
		t.Fatalf("read materialized report: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# OWASP-Style Security Assessment Report") {
		t.Fatalf("expected synthesized report header, got: %q", content)
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

	nextArgs, notes, repaired := repairMissingCommandInputPathsForShellWrapper(command, args, candidates, map[string]string{})
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

func TestRepairMissingCommandInputPathsRepairsFromLocalWorkspaceForLocalFileWorkflow(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	localArchive := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(localArchive, []byte("zip"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir}
	task := TaskSpec{
		TaskID:            "T-001",
		Title:             "Verify archive exists",
		Goal:              "Confirm archive path and scope",
		Targets:           []string{"127.0.0.1"},
		ExpectedArtifacts: []string{"archive_check.log"},
	}
	args := []string{"-l", "/tmp/secret.zip"}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "list_dir", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected local workspace repair")
	}
	if len(nextArgs) != 2 || nextArgs[1] != localArchive {
		t.Fatalf("expected repaired archive path %q, got %#v", localArchive, nextArgs)
	}
	if len(notes) == 0 || !strings.Contains(strings.ToLower(notes[0]), "local workspace") {
		t.Fatalf("expected local workspace note, got %v", notes)
	}
}

func TestRepairMissingCommandInputPathsRepairsBareFilenameFromLocalWorkspace(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	localArchive := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(localArchive, []byte("zip"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir}
	task := TaskSpec{
		TaskID:   "T-001",
		Title:    "Verify archive exists",
		Goal:     "Confirm archive path and scope",
		Targets:  []string{"127.0.0.1"},
		Strategy: "local_file_check",
	}
	args := []string{"-v", "secret.zip"}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "zipinfo", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected bare filename repair")
	}
	if len(nextArgs) != 2 || nextArgs[1] != localArchive {
		t.Fatalf("expected repaired archive path %q, got %#v", localArchive, nextArgs)
	}
	if len(notes) == 0 {
		t.Fatalf("expected repair notes")
	}
}

func TestRepairMissingCommandInputPathsRepairsReadFileBareFilenameFromLocalWorkspace(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	localArchive := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(localArchive, []byte("zip"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir}
	task := TaskSpec{
		TaskID:   "task-recon-seed",
		Title:    "Seed reconnaissance",
		Goal:     "Establish baseline evidence for local archive task",
		Targets:  []string{"127.0.0.1"},
		Strategy: "recon_seed",
	}
	args := []string{"secret.zip"}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "read_file", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected bare filename repair for read_file")
	}
	if len(nextArgs) != 1 || nextArgs[0] != localArchive {
		t.Fatalf("expected repaired archive path %q, got %#v", localArchive, nextArgs)
	}
	if len(notes) == 0 || !strings.Contains(strings.ToLower(notes[0]), "local workspace") {
		t.Fatalf("expected local workspace repair note, got %v", notes)
	}
}

func TestRepairMissingCommandInputPathsSkipsWorkspaceRepairForNonLocalTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "secret.zip"), []byte("zip"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir}
	task := TaskSpec{
		TaskID:            "T-001",
		Title:             "Verify archive exists",
		Goal:              "Confirm archive path and scope",
		Targets:           []string{"192.168.50.1"},
		ExpectedArtifacts: []string{"archive_check.log"},
	}
	args := []string{"-l", "/tmp/secret.zip"}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "list_dir", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if repaired {
		t.Fatalf("expected no workspace repair for non-local targets, got args=%#v notes=%v", nextArgs, notes)
	}
}

func TestAdaptArchiveJohnArgsRewritesZipFormatToPKZipAndPinsPot(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	hashPath := filepath.Join(workDir, "zip.hash")
	hash := "secret.zip/secret.txt:$pkzip$1*1*2*0*1*1*abcd*0*0*0*0*$/pkzip$:secret.txt:secret.zip::secret.zip\n"
	if err := os.WriteFile(hashPath, []byte(hash), 0o644); err != nil {
		t.Fatalf("WriteFile hash: %v", err)
	}

	args := []string{"--wordlist=/tmp/rockyou.txt", "--format=zip", hashPath}
	nextArgs, notes, changed, err := adaptArchiveJohnArgs(args, workDir)
	if err != nil {
		t.Fatalf("adaptArchiveJohnArgs: %v", err)
	}
	if !changed {
		t.Fatalf("expected john args adaptation")
	}
	if !containsArg(nextArgs, "--format=pkzip") {
		t.Fatalf("expected --format=pkzip, got %v", nextArgs)
	}
	if !containsArgPrefix(nextArgs, "--pot=") {
		t.Fatalf("expected --pot override, got %v", nextArgs)
	}
	if len(notes) == 0 {
		t.Fatalf("expected adaptation notes")
	}
}

func TestValidateCommandOutputEvidenceRejectsJohnNoHashesLoaded(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:   "T-003",
		Title:    "Run Baseline Wordlist Crack",
		Goal:     "Recover password for secret.zip in local lab",
		Targets:  []string{"127.0.0.1"},
		Strategy: "baseline_crack",
	}
	output := []byte("Using default input encoding: UTF-8\nNo password hashes loaded (see FAQ)\n")
	if err := validateCommandOutputEvidence(task, "john", []string{"--format=zip", "zip.hash"}, output); err == nil {
		t.Fatalf("expected john no-hashes-loaded evidence failure")
	}
}

func TestValidateCommandOutputEvidenceRejectsBareUnzipForArchiveWorkflow(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:   "T-007",
		Title:    "Validate access with proof-of-access token",
		Goal:     "Validate access with minimal proof-of-access",
		Targets:  []string{"127.0.0.1"},
		Strategy: "validation",
	}
	if err := validateCommandOutputEvidence(task, "unzip", nil, []byte("Usage: unzip\n")); err == nil {
		t.Fatalf("expected bare unzip validation failure")
	}
}

func TestAdaptArchiveExtractionShellArgsUsesRecoveredPasswordArtifact(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-archive-extraction-adapt"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	artifactDir := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, "T-003")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	recoveredPath := filepath.Join(artifactDir, "recovered_password.txt")
	if err := os.WriteFile(recoveredPath, []byte("telefo01\n"), 0o644); err != nil {
		t.Fatalf("WriteFile recovered password: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: base, RunID: runID}
	task := TaskSpec{
		TaskID:    "T-006",
		Title:     "Extract Archive Contents",
		Goal:      "Extract contents using recovered password",
		Targets:   []string{"127.0.0.1"},
		DependsOn: []string{"T-003"},
	}
	args := []string{"-lc", "unzip -P $(cat /tmp/john_output.txt | grep 'password' | cut -d' ' -f1) /tmp/secret.zip"}
	nextArgs, notes, changed, err := adaptArchiveExtractionShellArgs(cfg, task, args)
	if err != nil {
		t.Fatalf("adaptArchiveExtractionShellArgs: %v", err)
	}
	if !changed {
		t.Fatalf("expected extraction command adaptation")
	}
	if len(nextArgs) < 2 || !strings.Contains(nextArgs[1], recoveredPath) {
		t.Fatalf("expected rewritten shell command to use recovered password path %q, got %q", recoveredPath, nextArgs)
	}
	if len(notes) == 0 {
		t.Fatalf("expected adaptation note")
	}
}

func TestAdaptArchiveWorkflowCommandInjectsZipInputWhenMissing(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-archive-inject-zip"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	// Simulate a misleading dependency artifact with the expected filename.
	depDir := filepath.Join(paths.ArtifactDir, "T-001")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("MkdirAll depDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "secret.zip"), []byte("not a zip"), 0o644); err != nil {
		t.Fatalf("WriteFile fake secret.zip: %v", err)
	}

	realZip := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realZip, []byte("PK\x03\x04archive-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile real secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:            "T-003",
		Title:             "Extract hash material for secret.zip",
		Goal:              "Generate crackable hash material from secret.zip",
		Strategy:          "hash_extraction",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-001"},
		ExpectedArtifacts: []string{"zip.hash"},
	}
	workDir := filepath.Join(paths.ArtifactDir, "T-003")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}

	cmd, args, notes, changed, err := adaptArchiveWorkflowCommand(cfg, task, "zip2john", nil, workDir)
	if err != nil {
		t.Fatalf("adaptArchiveWorkflowCommand: %v", err)
	}
	if !changed {
		t.Fatalf("expected zip input adaptation")
	}
	if cmd != "zip2john" {
		t.Fatalf("expected command zip2john, got %q", cmd)
	}
	if len(args) != 1 || args[0] != realZip {
		t.Fatalf("expected injected real zip path %q, got %#v", realZip, args)
	}
	if len(notes) == 0 || !strings.Contains(strings.ToLower(notes[0]), "injected zip2john input") {
		t.Fatalf("expected zip injection note, got %v", notes)
	}
}

func TestAdaptArchiveWorkflowCommandInjectsJohnInputsWhenMissing(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-archive-inject-john"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	hashDir := filepath.Join(paths.ArtifactDir, "T-003")
	if err := os.MkdirAll(hashDir, 0o755); err != nil {
		t.Fatalf("MkdirAll hashDir: %v", err)
	}
	hashPath := filepath.Join(hashDir, "zip.hash")
	hash := "secret.zip/secret.txt:$pkzip$1*1*2*0*1*1*abcd*0*0*0*0*$/pkzip$:secret.txt:secret.zip::secret.zip\n"
	if err := os.WriteFile(hashPath, []byte(hash), 0o644); err != nil {
		t.Fatalf("WriteFile hash: %v", err)
	}

	wordlistDir := filepath.Join(paths.ArtifactDir, "T-002")
	if err := os.MkdirAll(wordlistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wordlistDir: %v", err)
	}
	wordlistPath := filepath.Join(wordlistDir, "wordlist.txt")
	if err := os.WriteFile(wordlistPath, []byte("password\ntelefo01\n"), 0o644); err != nil {
		t.Fatalf("WriteFile wordlist: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:            "T-004",
		Title:             "Crack archive password",
		Goal:              "Recover password from zip.hash",
		Strategy:          "wordlist_crack",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-003", "T-002"},
		ExpectedArtifacts: []string{"john_output.txt"},
	}
	workDir := filepath.Join(paths.ArtifactDir, "T-004")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}

	cmd, args, notes, changed, err := adaptArchiveWorkflowCommand(cfg, task, "john", nil, workDir)
	if err != nil {
		t.Fatalf("adaptArchiveWorkflowCommand: %v", err)
	}
	if !changed {
		t.Fatalf("expected john input adaptation")
	}
	if cmd != "john" {
		t.Fatalf("expected command john, got %q", cmd)
	}
	if !containsArg(args, hashPath) {
		t.Fatalf("expected injected hash path %q, got %v", hashPath, args)
	}
	if !containsArg(args, "--wordlist="+wordlistPath) {
		t.Fatalf("expected injected wordlist path %q, got %v", wordlistPath, args)
	}
	if !containsArgPrefix(args, "--pot=") {
		t.Fatalf("expected injected --pot argument, got %v", args)
	}
	if len(notes) == 0 {
		t.Fatalf("expected adaptation notes")
	}
}

func TestAdaptArchiveWorkflowCommandInjectsBoundedFcrackzipStrategy(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-archive-fcrackzip-bounded"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	realZip := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realZip, []byte("PK\x03\x04archive-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile real zip: %v", err)
	}
	wordlistDir := filepath.Join(paths.ArtifactDir, "T-002")
	if err := os.MkdirAll(wordlistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wordlistDir: %v", err)
	}
	wordlistPath := filepath.Join(wordlistDir, "wordlist.txt")
	if err := os.WriteFile(wordlistPath, []byte("telefo01\n"), 0o644); err != nil {
		t.Fatalf("WriteFile wordlist: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:            "T-006",
		Title:             "Run targeted mask/hybrid cracking strategy",
		Goal:              "Attempt bounded cracking strategy for secret.zip",
		Strategy:          "fallback_crack",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-002"},
		ExpectedArtifacts: []string{"fcrackzip_output.txt"},
	}
	workDir := filepath.Join(paths.ArtifactDir, "T-006")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}

	cmd, args, notes, changed, err := adaptArchiveWorkflowCommand(cfg, task, "fcrackzip", nil, workDir)
	if err != nil {
		t.Fatalf("adaptArchiveWorkflowCommand: %v", err)
	}
	if !changed {
		t.Fatalf("expected fcrackzip adaptation")
	}
	if cmd != "fcrackzip" {
		t.Fatalf("expected command fcrackzip, got %q", cmd)
	}
	for _, expected := range []string{"-D", "-u", "-p", wordlistPath, "-v", realZip} {
		if !containsArg(args, expected) {
			t.Fatalf("expected %q in adapted args, got %v", expected, args)
		}
	}
	if len(notes) == 0 {
		t.Fatalf("expected adaptation notes")
	}
}

func TestAdaptArchiveWorkflowCommandRewritesBareUnzipWithRecoveredPassword(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-archive-unzip-rewrite"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	realZip := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realZip, []byte("PK\x03\x04archive-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile real zip: %v", err)
	}
	passDir := filepath.Join(paths.ArtifactDir, "T-004")
	if err := os.MkdirAll(passDir, 0o755); err != nil {
		t.Fatalf("MkdirAll passDir: %v", err)
	}
	passPath := filepath.Join(passDir, "recovered_password.txt")
	if err := os.WriteFile(passPath, []byte("telefo01\n"), 0o644); err != nil {
		t.Fatalf("WriteFile recovered_password.txt: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:            "T-007",
		Title:             "Validate proof of access",
		Goal:              "Use recovered password to validate minimal archive access",
		Targets:           []string{"127.0.0.1"},
		DependsOn:         []string{"T-004"},
		ExpectedArtifacts: []string{"proof_token.txt"},
	}
	workDir := filepath.Join(paths.ArtifactDir, "T-007")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}

	cmd, args, notes, changed, err := adaptArchiveWorkflowCommand(cfg, task, "unzip", nil, workDir)
	if err != nil {
		t.Fatalf("adaptArchiveWorkflowCommand: %v", err)
	}
	if !changed {
		t.Fatalf("expected unzip adaptation")
	}
	if cmd != "bash" {
		t.Fatalf("expected rewritten unzip command bash, got %q", cmd)
	}
	if len(args) < 2 || args[0] != "-lc" {
		t.Fatalf("expected shell args [-lc <script>], got %v", args)
	}
	if !strings.Contains(args[1], passPath) || !strings.Contains(args[1], realZip) {
		t.Fatalf("expected script to reference password and zip paths, got %q", args[1])
	}
	if len(notes) < 2 {
		t.Fatalf("expected adaptation notes, got %v", notes)
	}

	cmdZip, argsZip, _, changedZip, err := adaptArchiveWorkflowCommand(cfg, task, "zip", nil, workDir)
	if err != nil {
		t.Fatalf("adaptArchiveWorkflowCommand zip: %v", err)
	}
	if !changedZip || cmdZip != "bash" || len(argsZip) < 2 || argsZip[0] != "-lc" {
		t.Fatalf("expected bare zip rewrite to bash proof extraction, got cmd=%q args=%v changed=%v", cmdZip, argsZip, changedZip)
	}
}

func TestAdaptArchiveWorkflowCommandConvertsBareZipToUnzipListingWithoutPassword(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-archive-zip-fallback"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	realZip := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realZip, []byte("PK\x03\x04archive-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile real zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:            "T-007",
		Title:             "Validate minimal proof-of-access using extracted token",
		Goal:              "Validate minimal proof-of-access using extracted token",
		Targets:           []string{"127.0.0.1"},
		ExpectedArtifacts: []string{"proof_token.txt"},
	}
	workDir := filepath.Join(paths.ArtifactDir, "T-007")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}

	cmd, args, notes, changed, err := adaptArchiveWorkflowCommand(cfg, task, "zip", nil, workDir)
	if err != nil {
		t.Fatalf("adaptArchiveWorkflowCommand: %v", err)
	}
	if !changed {
		t.Fatalf("expected adaptation")
	}
	if cmd != "unzip" {
		t.Fatalf("expected command unzip, got %q", cmd)
	}
	if len(args) != 2 || args[0] != "-l" || args[1] != realZip {
		t.Fatalf("expected unzip listing args with %q, got %v", realZip, args)
	}
	if len(notes) == 0 {
		t.Fatalf("expected adaptation note")
	}
}

func containsArg(args []string, expected string) bool {
	for _, arg := range args {
		if strings.TrimSpace(arg) == expected {
			return true
		}
	}
	return false
}

func containsArgPrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(strings.TrimSpace(arg), prefix) {
			return true
		}
	}
	return false
}

func TestRepairMissingCommandInputPathsUsesReferencedDependencyPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-repair-dependency-reference"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	depDir := filepath.Join(paths.ArtifactDir, "T-001")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("MkdirAll depDir: %v", err)
	}
	realArchive := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realArchive, []byte("PK\x03\x04test-zip-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}
	summaryPath := filepath.Join(depDir, "secret.zip")
	summary := "Path: " + realArchive + "\nType: file\nName: secret.zip\nBytes: 18\n"
	if err := os.WriteFile(summaryPath, []byte(summary), 0o644); err != nil {
		t.Fatalf("WriteFile summary artifact: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:    "T-002",
		Title:     "Extract Archive Metadata",
		Goal:      "Determine encryption/hash type before selecting cracking method",
		Strategy:  "metadata_discovery",
		Targets:   []string{"127.0.0.1"},
		DependsOn: []string{"T-001"},
		RiskLevel: string(RiskReconReadonly),
		DoneWhen:  []string{"metadata extracted"},
		FailWhen:  []string{"metadata extraction failed"},
		Budget:    TaskBudget{MaxSteps: 4, MaxToolCalls: 2, MaxRuntime: 20 * time.Second},
	}
	args := []string{"-v", "/tmp/secret.zip"}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "zipinfo", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected dependency path repair")
	}
	if len(nextArgs) != 2 || nextArgs[1] != realArchive {
		t.Fatalf("expected repaired archive path %q, got %#v", realArchive, nextArgs)
	}
	if len(notes) == 0 {
		t.Fatalf("expected repair note, got none")
	}
}

func TestRepairMissingCommandInputPathsForShellWrapperUsesReferencedDependencyPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-shell-repair-dependency-reference"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	depDir := filepath.Join(paths.ArtifactDir, "T-001")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("MkdirAll depDir: %v", err)
	}
	realArchive := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realArchive, []byte("PK\x03\x04test-zip-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}
	summaryPath := filepath.Join(depDir, "secret.zip")
	summary := "Path: " + realArchive + "\nType: file\nName: secret.zip\nBytes: 18\n"
	if err := os.WriteFile(summaryPath, []byte(summary), 0o644); err != nil {
		t.Fatalf("WriteFile summary artifact: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:    "T-003",
		Title:     "Extract Hash Material",
		Goal:      "Extract crackable hash material",
		Strategy:  "hash_extraction",
		Targets:   []string{"127.0.0.1"},
		DependsOn: []string{"T-001"},
	}
	args := []string{"-lc", "zip2john /tmp/secret.zip > /tmp/zip.hash"}

	nextArgs, _, repaired, err := repairMissingCommandInputPaths(cfg, task, "bash", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected shell-wrapper dependency path repair")
	}
	if len(nextArgs) < 2 || strings.Contains(nextArgs[1], "/tmp/secret.zip") || !strings.Contains(nextArgs[1], realArchive) {
		t.Fatalf("expected repaired shell body to reference %q, got %q", realArchive, nextArgs)
	}
}

func TestRepairMissingCommandInputPathsForShellWrapperUsesWorkspaceCandidate(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	realArchive := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realArchive, []byte("PK\x03\x04test-zip-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir}
	task := TaskSpec{
		TaskID:   "T-004",
		Title:    "Extract hash material",
		Goal:     "Extract crackable hash material",
		Strategy: "hash_extraction",
		Targets:  []string{"127.0.0.1"},
	}
	args := []string{"-lc", "zip2john /tmp/secret.zip > /tmp/zip.hash"}

	nextArgs, _, repaired, err := repairMissingCommandInputPaths(cfg, task, "bash", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected shell-wrapper workspace repair")
	}
	if len(nextArgs) < 2 || strings.Contains(nextArgs[1], "/tmp/secret.zip") || !strings.Contains(nextArgs[1], realArchive) {
		t.Fatalf("expected repaired shell body to reference %q, got %q", realArchive, nextArgs)
	}
}

func TestRepairMissingCommandInputPathsForShellWrapperRepairsBareFilename(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	localArchive := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(localArchive, []byte("PK\x03\x04test-zip-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir}
	task := TaskSpec{
		TaskID:   "T-001",
		Title:    "Verify archive exists",
		Goal:     "Confirm archive path and scope",
		Targets:  []string{"127.0.0.1"},
		Strategy: "local_file_check",
	}
	args := []string{"-lc", "zipinfo -v secret.zip"}

	nextArgs, _, repaired, err := repairMissingCommandInputPaths(cfg, task, "bash", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected shell bare filename repair")
	}
	if len(nextArgs) < 2 || strings.Contains(nextArgs[1], "secret.zip") && !strings.Contains(nextArgs[1], localArchive) {
		t.Fatalf("expected repaired shell body to reference %q, got %q", localArchive, nextArgs[1])
	}
}

func TestRepairMissingCommandInputPathsPrefersWorkspaceOverMisleadingDependencyArtifact(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	runID := "run-repair-workspace-preferred"
	paths, err := EnsureRunLayout(sessionsDir, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	depDir := filepath.Join(paths.ArtifactDir, "T-003")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("MkdirAll depDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "zip.hash"), []byte("fake hash"), 0o644); err != nil {
		t.Fatalf("WriteFile zip.hash: %v", err)
	}
	realArchive := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(realArchive, []byte("PK\x03\x04test-zip-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{SessionsDir: sessionsDir, RunID: runID}
	task := TaskSpec{
		TaskID:    "T-006",
		Title:     "Run fallback crack",
		Goal:      "Run fallback tooling if available",
		Strategy:  "local_only",
		Targets:   []string{"127.0.0.1"},
		DependsOn: []string{"T-003"},
	}
	args := []string{"-u", "-D", "-p", "/usr/share/wordlists/rockyou.txt", "/tmp/secret.zip"}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "fcrackzip", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected input repair")
	}
	if got := nextArgs[len(nextArgs)-1]; got != realArchive {
		t.Fatalf("expected archive path %q, got %q", realArchive, got)
	}
	hasLocalWorkspaceNote := false
	for _, note := range notes {
		if strings.Contains(strings.ToLower(note), "local workspace") {
			hasLocalWorkspaceNote = true
			break
		}
	}
	if !hasLocalWorkspaceNote {
		t.Fatalf("expected local workspace repair note, got %v", notes)
	}
}

func TestRepairMissingCommandInputPathsBootstrapsCompressedWordlist(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	wordlistDir := filepath.Join(repoRoot, "wordlists")
	if err := os.MkdirAll(wordlistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wordlists: %v", err)
	}
	requestedWordlist := filepath.Join(wordlistDir, "mini.txt")
	if err := writeGzipFile(filepath.Join(wordlistDir, "mini.txt.gz"), []byte("secret\npassword\n")); err != nil {
		t.Fatalf("writeGzipFile: %v", err)
	}
	archivePath := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(archivePath, []byte("PK\x03\x04test-zip-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{
		SessionsDir: sessionsDir,
		RunID:       "run-wordlist-bootstrap",
		WorkerID:    "worker-T-007-a1",
	}
	task := TaskSpec{
		TaskID:   "T-007",
		Title:    "Fallback crack",
		Goal:     "Run fallback tooling",
		Targets:  []string{"127.0.0.1"},
		Strategy: "fallback_crack",
	}
	args := []string{"-u", "-D", "-p", requestedWordlist, archivePath}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "fcrackzip", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected wordlist repair")
	}
	if got := nextArgs[3]; got == requestedWordlist {
		t.Fatalf("expected replaced wordlist path, got %q", got)
	}
	if _, statErr := os.Stat(nextArgs[3]); statErr != nil {
		t.Fatalf("expected bootstrapped wordlist file at %q: %v", nextArgs[3], statErr)
	}
	if len(notes) == 0 || !strings.Contains(strings.ToLower(strings.Join(notes, " ")), "wordlist") {
		t.Fatalf("expected wordlist repair note, got %v", notes)
	}
	content, readErr := os.ReadFile(nextArgs[3])
	if readErr != nil {
		t.Fatalf("ReadFile bootstrapped wordlist: %v", readErr)
	}
	if !strings.Contains(string(content), "secret") {
		t.Fatalf("expected decompressed wordlist content, got %q", string(content))
	}
}

func TestRepairMissingCommandInputPathsRepairsWordlistEqualsArg(t *testing.T) {
	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	wordlistDir := filepath.Join(repoRoot, "wordlists")
	if err := os.MkdirAll(wordlistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wordlists: %v", err)
	}
	requestedWordlist := filepath.Join(wordlistDir, "mini.txt")
	if err := writeGzipFile(filepath.Join(wordlistDir, "mini.txt.gz"), []byte("secret\npassword\n")); err != nil {
		t.Fatalf("writeGzipFile: %v", err)
	}
	hashPath := filepath.Join(repoRoot, "zip.hash")
	if err := os.WriteFile(hashPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("WriteFile zip.hash: %v", err)
	}
	t.Setenv("BIRDHACKBOT_WORDLIST_DIR", wordlistDir)

	cfg := WorkerRunConfig{SessionsDir: sessionsDir}
	task := TaskSpec{
		TaskID:   "T-004",
		Title:    "Run baseline wordlist crack",
		Goal:     "Run baseline wordlist cracking strategy",
		Targets:  []string{"127.0.0.1"},
		Strategy: "wordlist_crack",
	}
	args := []string{"--wordlist=" + requestedWordlist, "--format=zip", hashPath}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "john", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected wordlist= path repair")
	}
	if !containsArgPrefix(nextArgs, "--wordlist=/tmp/birdhackbot-wordlists/") {
		t.Fatalf("expected repaired --wordlist= arg, got %v", nextArgs)
	}
	if len(notes) == 0 {
		t.Fatalf("expected repair note")
	}
}

func TestRepairMissingCommandInputPathsForShellWrapperBootstrapsCompressedWordlist(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(repoRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	wordlistDir := filepath.Join(repoRoot, "wordlists")
	if err := os.MkdirAll(wordlistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wordlists: %v", err)
	}
	requestedWordlist := filepath.Join(wordlistDir, "mini.txt")
	if err := writeGzipFile(filepath.Join(wordlistDir, "mini.txt.gz"), []byte("secret\npassword\n")); err != nil {
		t.Fatalf("writeGzipFile: %v", err)
	}
	archivePath := filepath.Join(repoRoot, "secret.zip")
	if err := os.WriteFile(archivePath, []byte("PK\x03\x04test-zip-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile secret.zip: %v", err)
	}

	cfg := WorkerRunConfig{
		SessionsDir: sessionsDir,
		RunID:       "run-shell-wordlist-bootstrap",
		WorkerID:    "worker-T-007-a1",
	}
	task := TaskSpec{
		TaskID:   "T-007",
		Title:    "Fallback crack",
		Goal:     "Run fallback tooling",
		Targets:  []string{"127.0.0.1"},
		Strategy: "fallback_crack",
	}
	args := []string{"-lc", "fcrackzip -u -D -p " + requestedWordlist + " " + archivePath}

	nextArgs, notes, repaired, err := repairMissingCommandInputPaths(cfg, task, "bash", args)
	if err != nil {
		t.Fatalf("repairMissingCommandInputPaths: %v", err)
	}
	if !repaired {
		t.Fatalf("expected shell-wrapper wordlist repair")
	}
	if strings.Contains(nextArgs[1], requestedWordlist) {
		t.Fatalf("expected shell body replacement, got %q", nextArgs[1])
	}
	if len(notes) == 0 || !strings.Contains(strings.ToLower(strings.Join(notes, " ")), "wordlist") {
		t.Fatalf("expected shell wordlist repair note, got %v", notes)
	}
}

func writeGzipFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(data); err != nil {
		_ = zw.Close()
		_ = f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
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

func TestEnsureVulnerabilityEvidenceActionWithGoalTriggersForServiceScanContext(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-service",
		Goal:              "Collect bounded service evidence for the target",
		Strategy:          "service_enum",
		DoneWhen:          []string{"service scan captured"},
		FailWhen:          []string{"scan failed"},
		ExpectedArtifacts: []string{"service_scan.txt"},
		RiskLevel:         string(RiskActiveProbe),
	}
	args, note, rewritten := ensureVulnerabilityEvidenceActionWithGoal(
		task,
		"Perform an OWASP security assessment and produce a report",
		"nmap",
		[]string{"-sV", "--top-ports", "20", "scanme.nmap.org"},
	)
	if !rewritten {
		t.Fatalf("expected goal-context vulnerability evidence rewrite")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--script vuln and safe") {
		t.Fatalf("expected vuln script in args, got %#v", args)
	}
	if !strings.Contains(joined, "--script-timeout 20s") {
		t.Fatalf("expected script timeout in args, got %#v", args)
	}
	if !strings.Contains(strings.ToLower(note), "goal-context trigger") {
		t.Fatalf("expected goal-context trigger note, got %q", note)
	}
}

func TestEnsureVulnerabilityEvidenceActionWithGoalSkipsDiscoveryScan(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-discovery",
		Goal:              "Host discovery pass",
		Strategy:          "recon_seed",
		DoneWhen:          []string{"hosts discovered"},
		ExpectedArtifacts: []string{"hosts.txt"},
		RiskLevel:         string(RiskReconReadonly),
	}
	args, note, rewritten := ensureVulnerabilityEvidenceActionWithGoal(
		task,
		"Perform an OWASP security assessment and produce a report",
		"nmap",
		[]string{"-sn", "192.168.50.0/24"},
	)
	if rewritten {
		t.Fatalf("did not expect rewrite for discovery scan: args=%#v note=%q", args, note)
	}
	if len(args) != 2 {
		t.Fatalf("unexpected arg mutation for discovery scan: %#v", args)
	}
}

func TestEnsureVulnerabilityEvidenceActionWithGoalRewritesShellWrappedNmap(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-service-shell",
		Goal:              "Collect bounded service evidence for the target",
		Strategy:          "service_enum",
		DoneWhen:          []string{"service scan captured"},
		FailWhen:          []string{"scan failed"},
		ExpectedArtifacts: []string{"service_scan.txt"},
		RiskLevel:         string(RiskActiveProbe),
	}
	args, note, rewritten := ensureVulnerabilityEvidenceActionWithGoal(
		task,
		"Perform an OWASP security assessment and produce a report",
		"bash",
		[]string{"-lc", "set -euo pipefail; nmap -n -sV --top-ports 20 scanme.nmap.org | tee service_scan.txt"},
	)
	if !rewritten {
		t.Fatalf("expected shell-wrapped nmap vulnerability evidence rewrite")
	}
	if len(args) < 2 || !strings.Contains(args[1], `--script "vuln and safe"`) {
		t.Fatalf("expected vuln script insertion in shell body, got %#v", args)
	}
	if !strings.Contains(args[1], "--script-timeout 20s") {
		t.Fatalf("expected script-timeout insertion in shell body, got %#v", args)
	}
	if !strings.Contains(strings.ToLower(note), "goal-context trigger") {
		t.Fatalf("expected goal-context trigger note, got %q", note)
	}
}

func TestEnsureVulnerabilityEvidenceActionWithGoalDoesNotRewriteHostnameContainingNmap(t *testing.T) {
	t.Parallel()

	task := TaskSpec{
		TaskID:            "t-dns",
		Goal:              "Collect DNS baseline evidence",
		Strategy:          "recon_readonly",
		DoneWhen:          []string{"dns captured"},
		ExpectedArtifacts: []string{"dns_records.txt"},
		RiskLevel:         string(RiskReconReadonly),
	}
	args, note, rewritten := ensureVulnerabilityEvidenceActionWithGoal(
		task,
		"Perform an OWASP security assessment and produce a report",
		"bash",
		[]string{"-lc", "set -euo pipefail; dig scanme.nmap.org A +short | tee dns_records.txt"},
	)
	if rewritten {
		t.Fatalf("did not expect rewrite when script contains only hostname token: args=%#v note=%q", args, note)
	}
	if len(args) < 2 || !strings.Contains(args[1], "scanme.nmap.org") {
		t.Fatalf("unexpected script mutation: %#v", args)
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
	if len(args) < 11 || args[0] != "-c" {
		t.Fatalf("unexpected rewrite args: %#v", args)
	}
	if !strings.Contains(args[1], "OWASP-Style Security Assessment Report") {
		t.Fatalf("expected synthesis script in args[1]")
	}
	if args[2] != runID || args[3] != task.TaskID {
		t.Fatalf("expected run/task identifiers in args, got %#v", args[2:4])
	}
	if strings.TrimSpace(args[4]) != task.Goal {
		t.Fatalf("expected task goal in args[4], got %q", args[4])
	}
	if gotRoot := args[8]; gotRoot != BuildRunPaths(base, runID).ArtifactDir {
		t.Fatalf("expected artifact root %q, got %q", BuildRunPaths(base, runID).ArtifactDir, gotRoot)
	}
	if args[9] != "T-02" || args[10] != "T-03" {
		t.Fatalf("expected dependency task ids in args, got %#v", args[9:])
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
	if !strings.Contains(content, "## Task Execution Trace") {
		t.Fatalf("expected task execution trace section, got: %q", content)
	}
	if !strings.Contains(content, "### Task T-02") || !strings.Contains(content, "### Task T-03") {
		t.Fatalf("expected task-level trace entries for dependencies, got: %q", content)
	}
	if !strings.Contains(content, "No finding-specific remediation generated because no evidence-backed vulnerabilities were identified.") {
		t.Fatalf("expected no-findings remediation guidance, got: %q", content)
	}
}

func TestRunWorkerTaskReportSynthesisPrioritizesHighConfidenceCVEEvidence(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-report-cve-priority"
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
	vulnText := "" +
		"NSE: host appears vulnerable to CVE-2010-0738 on 192.168.50.1\n" +
		"Reference feed list: CVE-2010-0738 CVE-2010-0738 CVE-2010-0738 https://nvd.nist.gov/vuln/detail/CVE-2010-0738\n" +
		"Vendor advisory references CVE-2010-0738 for legacy versions\n"
	if err := os.WriteFile(vulnArtifact, []byte(vulnText), 0o644); err != nil {
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
	if !strings.Contains(content, "### CVE-2010-0738") {
		t.Fatalf("expected CVE heading in report output, got: %q", content)
	}
	if !strings.Contains(content, "Evidence confidence: high") {
		t.Fatalf("expected high-confidence CVE evidence, got: %q", content)
	}
	if !strings.Contains(content, "host appears vulnerable to CVE-2010-0738") {
		t.Fatalf("expected target-relevant vulnerability excerpt, got: %q", content)
	}
	if strings.Contains(content, "Reference feed list:") {
		t.Fatalf("did not expect low-quality reference-feed excerpt in prioritized findings, got: %q", content)
	}
}

func TestRunWorkerTaskReportSynthesisIncludesAuthAttemptTrace(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-report-auth-trace"
	taskID := "T-04"
	workerID := "worker-t4-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"192.168.50.1"}})

	artifactRoot := BuildRunPaths(base, runID).ArtifactDir
	fingerprintArtifact := filepath.Join(artifactRoot, "T-01", "router_fingerprint.json")
	if err := os.MkdirAll(filepath.Dir(fingerprintArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll fingerprint artifact dir: %v", err)
	}
	if err := os.WriteFile(fingerprintArtifact, []byte(`{
  "title": "ASUS Wireless Router GT-BE98",
  "form_actions": ["login_v2.cgi"],
  "candidate_auth_endpoints": ["/login.cgi", "/login_v2.cgi"]
}`), 0o644); err != nil {
		t.Fatalf("WriteFile fingerprint artifact: %v", err)
	}
	attemptArtifact := filepath.Join(artifactRoot, "T-03", "admin_attempt_result.json")
	if err := os.MkdirAll(filepath.Dir(attemptArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll attempt artifact dir: %v", err)
	}
	if err := os.WriteFile(attemptArtifact, []byte(`{
  "method": "nonce_hash_login_v2",
  "success": false,
  "stop_reason": "bounded_attempts_exhausted",
  "attempts": [
    {"user": "admin", "suspected_success": false},
    {"user": "admin", "suspected_success": false}
  ]
}`), 0o644); err != nil {
		t.Fatalf("WriteFile attempt artifact: %v", err)
	}
	proofArtifact := filepath.Join(artifactRoot, "T-03", "admin_access_proof.txt")
	if err := os.WriteFile(proofArtifact, []byte("NO_SUCCESS\nbounded_attempts_exhausted\n"), 0o644); err != nil {
		t.Fatalf("WriteFile proof artifact: %v", err)
	}

	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "Generate final OWASP report with execution detail",
		Targets:           []string{"192.168.50.1"},
		DependsOn:         []string{"T-01", "T-03"},
		DoneWhen:          []string{"OWASP report generated"},
		FailWhen:          []string{"report generation failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "cat",
			Args:    []string{"/tmp/router_fingerprint.json", "/tmp/admin_attempt_result.json"},
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
	if !strings.Contains(content, "## Task Execution Trace") {
		t.Fatalf("expected task execution trace section, got: %q", content)
	}
	if !strings.Contains(content, "- Report objective: Generate final OWASP report with execution detail") {
		t.Fatalf("expected report objective in scope section, got: %q", content)
	}
	if !strings.Contains(content, "attempt workflow: method=nonce_hash_login_v2") {
		t.Fatalf("expected attempt workflow details in trace, got: %q", content)
	}
	if !strings.Contains(content, "admin access proof status: NO_SUCCESS") {
		t.Fatalf("expected admin access proof status in trace, got: %q", content)
	}
}

func TestRunWorkerTaskReportSynthesisDoesNotReuseStaleReportFile(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-report-stale-file-guard"
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
	if err := os.WriteFile(vulnArtifact, []byte("NSE: vulnerable to CVE-2010-0738\n"), 0o644); err != nil {
		t.Fatalf("WriteFile vuln artifact: %v", err)
	}

	workspace := filepath.Join(base, "report-workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	staleReportPath := filepath.Join(workspace, "owasp_report.md")
	if err := os.WriteFile(staleReportPath, []byte("STALE_REPORT\n"), 0o644); err != nil {
		t.Fatalf("WriteFile stale report: %v", err)
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
			Type:       "command",
			Command:    "cat",
			Args:       []string{"/tmp/service_scan_output.txt", "/tmp/vuln_scan_output.txt"},
			WorkingDir: workspace,
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

	materializedReport := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, taskID, "owasp_report.md")
	data, err := os.ReadFile(materializedReport)
	if err != nil {
		t.Fatalf("read materialized report: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "STALE_REPORT") {
		t.Fatalf("expected synthesized report output, but stale report content was reused: %q", content)
	}
	if !strings.Contains(content, "# OWASP-Style Security Assessment Report") {
		t.Fatalf("expected synthesized report header, got: %q", content)
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
		OrchDiagnosticEnv:  "true",
		OrchApprovalEnv:    "90s",
		OrchToolInstallEnv: string(ToolInstallPolicyNever),
	}
	cfg := ParseWorkerRunConfig(func(key string) string {
		return env[key]
	})
	if cfg.SessionsDir != "/tmp/sessions" || cfg.RunID != "run-1" || cfg.TaskID != "task-1" || cfg.WorkerID != "worker-1" || cfg.Attempt != 2 || cfg.Permission != PermissionAll || !cfg.Disruptive || !cfg.Diagnostic || cfg.ApprovalTimeout != 90*time.Second || cfg.ToolInstallPolicy != ToolInstallPolicyNever {
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
