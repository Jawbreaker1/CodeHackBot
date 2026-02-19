package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	if status, _ := contract["verification_status"].(string); status != "satisfied" {
		t.Fatalf("expected verification_status satisfied, got %q", status)
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
