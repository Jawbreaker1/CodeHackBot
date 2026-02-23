package orchestrator

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeWorkerAssistModeRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	if _, err := normalizeWorkerAssistMode("invalid-mode"); err == nil {
		t.Fatalf("expected invalid worker assist mode to return error")
	}
}

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
	if got, _ := payload["reason"].(string); got != WorkerFailureAssistParseFailure {
		t.Fatalf("expected %s reason, got %q", WorkerFailureAssistParseFailure, got)
	}
}

func TestRunWorkerTaskAssistInvalidModeFailsFast(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-invalid-mode"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "assist invalid mode",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Run assist mode validation.",
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   5 * time.Second,
		},
	})

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, "http://127.0.0.1:1")
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "typo-mode")

	err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	})
	if err == nil {
		t.Fatalf("expected invalid assist mode to fail")
	}
	if !strings.Contains(err.Error(), "invalid worker assist mode") {
		t.Fatalf("expected invalid mode error, got %v", err)
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

func TestRunWorkerTaskAssistConsecutiveToolChurnFallsBackToNoNewEvidence(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-tool-loop"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "detect tool churn loops",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Use tools to investigate.",
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
		content := fmt.Sprintf("{\"type\":\"tool\",\"summary\":\"tool churn\",\"tool\":{\"language\":\"bash\",\"name\":\"loop\",\"files\":[{\"path\":\"loop_%d.sh\",\"content\":\"echo loop\\n\"}],\"run\":{\"command\":\"bash\",\"args\":[\"tools/loop_%d.sh\"]}}}", seq, seq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask consecutive tool churn fallback: %v", err)
	}
	events, evErr := NewManager(base).Events(runID, 0)
	if evErr != nil {
		t.Fatalf("Events: %v", evErr)
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
		t.Fatalf("did not expect task_failed event: %#v", payload)
	}
	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_no_new_evidence" {
		t.Fatalf("expected assist_no_new_evidence completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistToolChurnCanRecoverWithCommand(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-tool-churn-recover"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover from tool churn",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Use tools, then recover to command.",
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
		switch {
		case seq <= int32(workerAssistMaxConsecutiveToolTurns+1):
			content := fmt.Sprintf("{\"type\":\"tool\",\"summary\":\"tool churn\",\"tool\":{\"language\":\"bash\",\"name\":\"loop\",\"files\":[{\"path\":\"loop_%d.sh\",\"content\":\"echo loop\\n\"}],\"run\":{\"command\":\"bash\",\"args\":[\"tools/loop_%d.sh\"]}}}", seq, seq)
			_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
		case seq == int32(workerAssistMaxConsecutiveToolTurns+2):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"list_dir\",\"args\":[\".\"],\"summary\":\"switch to command\"}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"done\"}"}}]}`))
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask tool churn recover: %v", err)
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
		if got, _ := payload["reason"].(string); got == WorkerFailureAssistLoopDetected {
			t.Fatalf("did not expect loop detection failure after command recovery: %#v", payload)
		}
	}
}

func TestRunWorkerTaskAssistRecoverToolChurnCanPivotToCommand(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-recover-tool-churn-pivot"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover mode tool churn pivot",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Force recover mode and then pivot back to command.",
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
		switch {
		case seq == 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need context\"}"}}]}`))
		case seq <= int32(workerAssistMaxConsecutiveRecoverToolTurns+2):
			content := fmt.Sprintf("{\"type\":\"tool\",\"summary\":\"recover tool churn\",\"tool\":{\"language\":\"bash\",\"name\":\"loop\",\"files\":[{\"path\":\"recover_%d.sh\",\"content\":\"echo recover\\n\"}],\"run\":{\"command\":\"bash\",\"args\":[\"tools/recover_%d.sh\"]}}}", seq, seq)
			_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
		case seq == int32(workerAssistMaxConsecutiveRecoverToolTurns+3):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"list_dir\",\"args\":[\".\"],\"summary\":\"pivot command\"}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"done\"}"}}]}`))
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask recover tool churn pivot: %v", err)
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
		if got, _ := payload["reason"].(string); got == WorkerFailureAssistLoopDetected {
			t.Fatalf("did not expect loop detection failure before recover pivot: %#v", payload)
		}
	}
}

func TestRunWorkerTaskAssistRecoverRepeatedToolFallsBackToNoNewEvidenceCompletion(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-recover-no-new-evidence"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover repeated tool fallback",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Repeat same recover tool until no-new-evidence fallback is used.",
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
		if seq == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need more context\"}"}}]}`))
			return
		}
		content := "{\"type\":\"tool\",\"summary\":\"repeat same tool\",\"tool\":{\"language\":\"bash\",\"name\":\"check-auth\",\"files\":[{\"path\":\"check_auth_paths.sh\",\"content\":\"echo ok\\n\"}],\"run\":{\"command\":\"bash\",\"args\":[\"tools/check_auth_paths.sh\"]}}}"
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask recover no-new-evidence fallback: %v", err)
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("expected task_completed event")
	}
	for _, event := range events {
		if event.Type == EventTypeTaskFailed {
			payload := map[string]any{}
			if len(event.Payload) > 0 {
				_ = json.Unmarshal(event.Payload, &payload)
			}
			t.Fatalf("did not expect task_failed event: %#v", payload)
		}
	}

	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_no_new_evidence" {
		t.Fatalf("expected assist_no_new_evidence completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistRecoverToolCallCapFallsBackToNoNewEvidenceCompletion(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-recover-tool-cap"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover tool-call cap fallback",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Enter recover mode and keep running varied tool checks.",
		},
		Budget: TaskBudget{
			MaxSteps:     30,
			MaxToolCalls: 30,
			MaxRuntime:   25 * time.Second,
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
		switch {
		case seq == 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need target context\"}"}}]}`))
			return
		case seq >= 2 && seq <= 9:
			content := fmt.Sprintf("{\"type\":\"command\",\"command\":\"bash\",\"args\":[\"-lc\",\"echo pre_%d\"],\"summary\":\"pre-cap command\"}", seq)
			_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
			return
		case seq == 10:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need additional target context\"}"}}]}`))
			return
		}
		content := fmt.Sprintf("{\"type\":\"tool\",\"summary\":\"varied recover tool\",\"tool\":{\"language\":\"bash\",\"name\":\"probe\",\"files\":[{\"path\":\"probe_%d.sh\",\"content\":\"echo probe_%d\\n\"}],\"run\":{\"command\":\"bash\",\"args\":[\"tools/probe_%d.sh\"]}}}", seq, seq, seq)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask recover tool-call cap fallback: %v", err)
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
		t.Fatalf("did not expect task_failed event: %#v", payload)
	}
	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_no_new_evidence" {
		t.Fatalf("expected assist_no_new_evidence completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistRecoverQuestionAfterToolCapFallsBackToNoNewEvidence(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-recover-question-cap"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover question fallback after tool cap",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Force recover mode, execute many tools, then ask a question.",
		},
		Budget: TaskBudget{
			MaxSteps:     30,
			MaxToolCalls: 30,
			MaxRuntime:   25 * time.Second,
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
		switch {
		case seq == 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need target context\"}"}}]}`))
			return
		case seq == 5 || seq == 9 || seq == 12:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need more context\"}"}}]}`))
			return
		case seq <= 12:
			content := fmt.Sprintf("{\"type\":\"tool\",\"summary\":\"recover tool\",\"tool\":{\"language\":\"bash\",\"name\":\"probe\",\"files\":[{\"path\":\"probe_%d.sh\",\"content\":\"echo probe\\n\"}],\"run\":{\"command\":\"bash\",\"args\":[\"tools/probe_%d.sh\"]}}}", seq, seq)
			_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
			return
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"done\"}"}}]}`))
			return
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask recover question cap fallback: %v", err)
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
		t.Fatalf("did not expect task_failed event: %#v", payload)
	}
	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_no_new_evidence" {
		t.Fatalf("expected assist_no_new_evidence completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistRecoverCommandKeepsRecoverModeForToolCapFallback(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-recover-command-cap"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover command fallback after tool cap",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Force recover mode, then keep issuing command actions.",
		},
		Budget: TaskBudget{
			MaxSteps:     30,
			MaxToolCalls: 30,
			MaxRuntime:   25 * time.Second,
		},
	})

	workerToolsDir := filepath.Join(BuildRunPaths(base, runID).Root, "workers", workerID, "tools")
	if err := os.MkdirAll(workerToolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll worker tools dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workerToolsDir, "probe.sh"), []byte("#!/usr/bin/env bash\necho probe \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile probe.sh: %v", err)
	}

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		seq := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case seq == 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need target context\"}"}}]}`))
			return
		case seq <= 15:
			content := fmt.Sprintf("{\"type\":\"command\",\"command\":\"bash\",\"args\":[\"tools/probe.sh\",\"%d\"],\"summary\":\"recover command\"}", seq)
			_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
			return
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"done\"}"}}]}`))
			return
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask recover command cap fallback: %v", err)
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
		t.Fatalf("did not expect task_failed event: %#v", payload)
	}
	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_no_new_evidence" {
		t.Fatalf("expected assist_no_new_evidence completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistRecoverPlanDoesNotFailImmediately(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-recover-plan"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover from non-action plan",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Validate recovery behavior.",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
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
		switch seq {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need more context\",\"summary\":\"asking\"}"}}]}`))
		case 2:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"plan\",\"summary\":\"non-action recover plan\",\"plan\":\"1) inspect logs\"}"}}]}`))
		case 3:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"list_dir\",\"args\":[\".\"],\"summary\":\"inspect workspace\"}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"done\"}"}}]}`))
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask assist recover-plan: %v", err)
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
			t.Fatalf("did not expect %s failure for recover plan churn: %#v", WorkerFailureAssistNoAction, payload)
		}
	}
}

func TestRunWorkerTaskAssistRecoverQuestionChurnDoesNotTripRecoveryLimit(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-recover-question-churn"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover from question churn",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Validate recover question handling.",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
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
		switch seq {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need summary context\"}"}}]}`))
		case 2:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"noop\",\"summary\":\"thinking\"}"}}]}`))
		case 3:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need more details\"}"}}]}`))
		case 4:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"noop\",\"summary\":\"still thinking\"}"}}]}`))
		case 5:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"command\",\"command\":\"list_dir\",\"args\":[\".\"],\"summary\":\"inspect workspace\"}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"done\"}"}}]}`))
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask assist recover-question churn: %v", err)
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
		if got, _ := payload["reason"].(string); got == WorkerFailureAssistLoopDetected {
			t.Fatalf("did not expect %s for recover question churn: %#v", WorkerFailureAssistLoopDetected, payload)
		}
	}
}

func TestRunWorkerTaskAssistSummaryTaskRecoverQuestionChurnCompletes(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-summary-question-churn"
	taskID := "task-plan-summary"
	workerID := "worker-task-plan-summary-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Title:             "Plan synthesis summary",
		Strategy:          "summarize_and_replan",
		Goal:              "Consolidate hypothesis outcomes",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"hypothesis_summary_recorded"},
		FailWhen:          []string{"summary_failed"},
		ExpectedArtifacts: []string{"plan-summary.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Consolidate hypothesis outcomes and summarize findings.",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 8,
			MaxRuntime:   20 * time.Second,
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need exact hypothesis list\"}"}}]}`))
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask summary question churn fallback: %v", err)
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
		t.Fatalf("did not expect task_failed event: %#v", payload)
	}
	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_summary_autocomplete" {
		t.Fatalf("expected assist_summary_autocomplete completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistSummaryTaskRecoverNonActionChurnCompletes(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-summary-non-action-churn"
	taskID := "task-plan-summary"
	workerID := "worker-task-plan-summary-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Title:             "Plan synthesis summary",
		Strategy:          "summarize_and_replan",
		Goal:              "Consolidate hypothesis outcomes",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"hypothesis_summary_recorded"},
		FailWhen:          []string{"summary_failed"},
		ExpectedArtifacts: []string{"plan-summary.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Consolidate hypothesis outcomes and summarize findings.",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 8,
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
		switch seq {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need exact hypothesis list\"}"}}]}`))
		case 2:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"plan\",\"summary\":\"still planning\",\"plan\":\"1) continue planning\"}"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"noop\",\"summary\":\"waiting\"}"}}]}`))
		}
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask summary non-action churn fallback: %v", err)
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
		t.Fatalf("did not expect task_failed event: %#v", payload)
	}
	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_summary_autocomplete" {
		t.Fatalf("expected assist_summary_autocomplete completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistAdaptiveReplanActionStepCapFallsBackToNoNewEvidence(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-adaptive-step-cap"
	taskID := "task-rp-step-cap"
	workerID := "worker-task-rp-step-cap-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Title:             "Adaptive recovery task",
		Strategy:          "adaptive_replan_execution_failure",
		Goal:              "Recover from execution failure",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"replan_task_completed"},
		FailWhen:          []string{"replan_task_failed"},
		ExpectedArtifacts: []string{"adaptive-recovery.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Keep probing until you can summarize progress.",
		},
		Budget: TaskBudget{
			MaxSteps:     3,
			MaxToolCalls: 10,
			MaxRuntime:   20 * time.Second,
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"tool\",\"summary\":\"collect evidence\",\"tool\":{\"language\":\"bash\",\"name\":\"probe\",\"files\":[{\"path\":\"probe.sh\",\"content\":\"echo probe\\n\"}],\"run\":{\"command\":\"bash\",\"args\":[\"tools/probe.sh\"]}}}"}}]}`))
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask adaptive step-cap fallback: %v", err)
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
		t.Fatalf("did not expect task_failed event: %#v", payload)
	}
	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_no_new_evidence" {
		t.Fatalf("expected assist_no_new_evidence completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistAdaptiveReplanToolCapFallsBackToNoNewEvidence(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-adaptive-tool-cap"
	taskID := "task-rp-tool-cap"
	workerID := "worker-task-rp-tool-cap-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Title:             "Adaptive recovery task",
		Strategy:          "adaptive_replan_worker_recovery",
		Goal:              "Recover from worker failure",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"replan_task_completed"},
		FailWhen:          []string{"replan_task_failed"},
		ExpectedArtifacts: []string{"adaptive-recovery.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Keep probing until you can summarize progress.",
		},
		Budget: TaskBudget{
			MaxSteps:     10,
			MaxToolCalls: 2,
			MaxRuntime:   20 * time.Second,
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"tool\",\"summary\":\"collect evidence\",\"tool\":{\"language\":\"bash\",\"name\":\"probe\",\"files\":[{\"path\":\"probe.sh\",\"content\":\"echo probe\\n\"}],\"run\":{\"command\":\"bash\",\"args\":[\"tools/probe.sh\"]}}}"}}]}`))
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask adaptive tool-cap fallback: %v", err)
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
		t.Fatalf("did not expect task_failed event: %#v", payload)
	}
	completed, ok := firstEventByType(events, EventTypeTaskCompleted)
	if !ok {
		t.Fatalf("expected task_completed payload")
	}
	payload := map[string]any{}
	if len(completed.Payload) > 0 {
		_ = json.Unmarshal(completed.Payload, &payload)
	}
	if got, _ := payload["reason"].(string); got != "assist_no_new_evidence" {
		t.Fatalf("expected assist_no_new_evidence completion reason, got %q", got)
	}
}

func TestRunWorkerTaskAssistRecoverPlanLoopClassifiedAsLoopDetected(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-recover-plan-loop"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "recover plan loop classification",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Validate recover plan loop handling.",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   20 * time.Second,
		},
	})

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"question\",\"question\":\"Need more context\",\"summary\":\"asking\"}"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"plan\",\"summary\":\"still planning\",\"plan\":\"1) keep planning\"}"}}]}`))
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
		t.Fatalf("expected recover plan loop to fail")
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
	if got, _ := payload["reason"].(string); got == WorkerFailureAssistNoAction {
		t.Fatalf("did not expect %s for recover plan loop", WorkerFailureAssistNoAction)
	}
}

func TestResolveShellToolScriptPathUsesToolsDirectory(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	toolsDir := filepath.Join(workDir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll tools: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "discover_hosts.sh"), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("WriteFile discover_hosts.sh: %v", err)
	}

	args, note, adapted := resolveShellToolScriptPath(workDir, "bash", []string{"discover_hosts.sh"})
	if !adapted {
		t.Fatalf("expected shell script path adaptation")
	}
	if len(args) != 1 || args[0] != "tools/discover_hosts.sh" {
		t.Fatalf("expected tools/discover_hosts.sh, got %#v", args)
	}
	if !strings.Contains(note, "resolved shell script") {
		t.Fatalf("expected adaptation note, got %q", note)
	}
}

func TestRunWorkerTaskAssistPromptIncludesFullRunScope(t *testing.T) {
	base := t.TempDir()
	runID := "run-worker-task-assist-scope-prompt"
	taskID := "t1"
	workerID := "worker-t1-a1"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{
		Networks:    []string{"192.168.50.0/24"},
		Targets:     []string{"corp.internal"},
		DenyTargets: []string{"192.168.50.99"},
	})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "verify assist scope prompt",
		Targets:           []string{"192.168.50.10"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Summarize scope and complete.",
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	})

	seenScopePrompt := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		payload := struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}{}
		_ = json.Unmarshal(body, &payload)
		for _, msg := range payload.Messages {
			if msg.Role == "user" {
				seenScopePrompt = msg.Content
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"complete\",\"final\":\"ok\"}"}}]}`))
	}))
	defer server.Close()

	t.Setenv(workerConfigPathEnv, "")
	t.Setenv(workerLLMBaseURLEnv, server.URL)
	t.Setenv(workerLLMModelEnv, "test-model")
	t.Setenv(workerLLMTimeoutSeconds, "10")
	t.Setenv(workerAssistModeEnv, "strict")

	if err := RunWorkerTask(WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    workerID,
		Attempt:     1,
	}); err != nil {
		t.Fatalf("RunWorkerTask assist scope prompt: %v", err)
	}
	if !strings.Contains(seenScopePrompt, "Scope: 192.168.50.0/24, corp.internal, deny:192.168.50.99") {
		t.Fatalf("expected full run scope in prompt, got: %q", seenScopePrompt)
	}
	if !strings.Contains(seenScopePrompt, "Targets: 192.168.50.10") {
		t.Fatalf("expected task targets in prompt, got: %q", seenScopePrompt)
	}
}

func writeAssistTask(t *testing.T, base, runID string, task TaskSpec) {
	t.Helper()
	taskPath := filepath.Join(BuildRunPaths(base, runID).TaskDir, task.TaskID+".json")
	if err := WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}
}
