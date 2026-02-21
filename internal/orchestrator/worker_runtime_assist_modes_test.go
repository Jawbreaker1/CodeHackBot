package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWorkerTaskAssistStrictModeFailsWithoutFallback(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-strict"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "assist strict mode",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Hello what can you help me with?",
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   10 * time.Second,
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"I cannot help with that request."}}]}`))
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err == nil {
		t.Fatalf("expected strict mode assist failure")
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("did not expect task_completed in strict mode parse failure")
	}
	failEvent, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failEvent.Payload) > 0 {
		_ = json.Unmarshal(failEvent.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != WorkerFailureAssistUnavailable {
		t.Fatalf("expected %s reason, got %q", WorkerFailureAssistUnavailable, got)
	}
}

func TestRunWorkerTaskAssistDegradedModeEmitsFallbackMetadata(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-degraded"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "assist degraded mode",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Hello what can you help me with?",
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   10 * time.Second,
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"I cannot help with that request."}}]}`))
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "degraded")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask assist degraded: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed in degraded mode")
	}

	foundFallbackMeta := false
	for _, event := range events {
		if event.Type != EventTypeTaskProgress {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		fallbackUsed, _ := payload["fallback_used"].(bool)
		if !fallbackUsed {
			continue
		}
		foundFallbackMeta = true
		if got, _ := payload["assist_mode"].(string); got != "degraded" {
			t.Fatalf("expected assist_mode degraded, got %q", got)
		}
		if got, _ := payload["fallback_reason"].(string); got != "parse_failure" {
			t.Fatalf("expected fallback_reason parse_failure, got %q", got)
		}
		if got, _ := payload["assistant_model"].(string); got != "test-model" {
			t.Fatalf("expected assistant_model test-model, got %q", got)
		}
		parseRepairUsed, _ := payload["parse_repair_used"].(bool)
		if !parseRepairUsed {
			t.Fatalf("expected parse_repair_used=true when fallback reason is parse_failure")
		}
		timeoutSeconds, _ := payload["llm_timeout_seconds"].(float64)
		if int(timeoutSeconds) <= 0 {
			t.Fatalf("expected llm_timeout_seconds > 0, got %v", payload["llm_timeout_seconds"])
		}
	}
	if !foundFallbackMeta {
		t.Fatalf("expected at least one progress event with fallback metadata")
	}
}

func TestRunWorkerTaskAssistLoopGuardDetectsSemanticRepeats(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-semantic-loop"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "loop detection",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "List the directory repeatedly.",
		},
		Budget: TaskBudget{
			MaxSteps:     20,
			MaxToolCalls: 20,
			MaxRuntime:   20 * time.Second,
		},
	})

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		seq := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if seq%2 == 0 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"list_dir\",\"args\":[\".\",\"-la\"],\"summary\":\"repeat list\"}"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"ls\",\"args\":[\"-la\",\".\"],\"summary\":\"repeat list\"}"}}]}`))
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err == nil {
		t.Fatalf("expected semantic repeat loop to fail")
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
	if got, _ := payload["reason"].(string); got != WorkerFailureAssistLoopDetected {
		t.Fatalf("expected %s reason, got %q", WorkerFailureAssistLoopDetected, got)
	}
}

func writeAssistTask(t *testing.T, base, runID string, task TaskSpec) {
	t.Helper()
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, task.TaskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}
}
