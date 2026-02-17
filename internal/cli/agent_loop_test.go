package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func TestAssistInputIncludesRecentObservations(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Context.ChatHistoryLines = 5
	r := NewRunner(cfg, "session-obs", "", "")

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	_ = sessionDir

	r.recordObservationWithCommand("run", "echo", []string{"hello"}, filepath.Join(cfg.Session.LogDir, r.sessionID, "logs", "demo.log"), "hello", "", 0)

	input, err := r.assistInput(filepath.Join(cfg.Session.LogDir, r.sessionID), "continue", "")
	if err != nil {
		t.Fatalf("assistInput: %v", err)
	}
	if !strings.Contains(input.RecentLog, "run: echo hello") {
		t.Fatalf("expected observation in RecentLog, got:\n%s", input.RecentLog)
	}
	if !strings.Contains(input.RecentLog, "exit=0") {
		t.Fatalf("expected exit code in RecentLog, got:\n%s", input.RecentLog)
	}
	if !strings.Contains(input.RecentLog, "out: hello") {
		t.Fatalf("expected output excerpt in RecentLog, got:\n%s", input.RecentLog)
	}
}

func TestGetAssistSuggestionCarriesObservationsForward(t *testing.T) {
	var call int32
	var secondBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		n := atomic.AddInt32(&call, 1)
		if n == 2 {
			secondBody = string(body)
		}
		// Return a minimal valid assistant JSON suggestion.
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"type":"noop","summary":"noop"}`,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	r := NewRunner(cfg, "session-carry", "", "")

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	// First call populates baseline prompt. We then record a new observation and expect
	// it to appear in the next LLM prompt.
	if _, err := r.getAssistSuggestion("first", ""); err != nil {
		t.Fatalf("first suggest: %v", err)
	}
	r.recordObservationWithCommand("run", "echo", []string{"hi"}, filepath.Join(sessionDir, "logs", "demo.log"), "hi", "", 0)

	if _, err := r.getAssistSuggestion("second", "execute-step"); err != nil {
		t.Fatalf("second suggest: %v", err)
	}

	if !strings.Contains(secondBody, "Recent observations") || !strings.Contains(secondBody, "run: echo hi") {
		t.Fatalf("expected second LLM request to include observation; body:\n%s", secondBody)
	}
}

func TestAssistCompleteFinalizesReportArtifact(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"type":"complete","final":"done"}`,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	cfg.Tools.Shell.Enabled = true
	r := NewRunner(cfg, "session-report", "", "")

	// Capture stdout because complete prints directly.
	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() {
		os.Stdout = orig
	})

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		t.Fatalf("EnsureArtifacts: %v", err)
	}
	_ = os.WriteFile(artifacts.FactsPath, []byte("- Finding: demo\n"), 0o644)

	goal := "create a security report for demo.local"
	if err := r.handleAssistAgentic(goal, false, ""); err != nil {
		t.Fatalf("handleAssistAgentic: %v", err)
	}
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	if !strings.Contains(buf.String(), "done") {
		t.Fatalf("expected complete final output, got:\n%s", buf.String())
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", calls)
	}
	reportPath := filepath.Join(sessionDir, "report.md")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("report missing: %v", err)
	}
	if !strings.Contains(string(data), "# Security Assessment Report") {
		t.Fatalf("unexpected report content:\n%s", string(data))
	}

	archives, err := os.ReadDir(filepath.Join(sessionDir, "reports"))
	if err != nil {
		t.Fatalf("expected reports archive dir: %v", err)
	}
	if len(archives) == 0 {
		t.Fatalf("expected archived report copy")
	}
}

func TestAssistRepeatedCommandGuardRequestsAlternativeAndContinues(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		content := `{"type":"complete","final":"done"}`
		switch n {
		case 1, 2, 3:
			content = `{"type":"command","command":"ls","args":["-la"],"summary":"list files"}`
		case 4:
			content = `{"type":"complete","final":"done"}`
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-repeat-guard", "", "")

	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() {
		os.Stdout = orig
	})

	if err := r.handleAssistAgentic("perform local recon task", false, ""); err != nil {
		t.Fatalf("handleAssistAgentic: %v", err)
	}
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	out := buf.String()
	if !strings.Contains(out, "Repeated step detected; asking assistant for an alternative action.") &&
		!strings.Contains(out, "done") {
		t.Fatalf("expected recovery progress or completion, got:\n%s", out)
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Fatalf("expected at least 3 llm calls, got %d", calls)
	}
}

func TestAssistFollowUpCarriesPreviousQuestionAndAnswer(t *testing.T) {
	var call int32
	var secondBody string
	const question = "Should I proceed with a default wordlist?"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		n := atomic.AddInt32(&call, 1)
		if n == 2 {
			secondBody = string(body)
		}
		content := `{"type":"complete","final":"done"}`
		if n == 1 {
			content = `{"type":"question","question":"` + question + `"}`
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-followup-context", "", "")

	if err := r.handleAssistAgentic("crack the zip in this folder", false, ""); err != nil {
		t.Fatalf("initial assist: %v", err)
	}
	if err := r.handleAssistFollowUp("go ahead with default"); err != nil {
		t.Fatalf("follow-up assist: %v", err)
	}

	if !strings.Contains(secondBody, "Assistant previous question: "+question) {
		t.Fatalf("expected previous question in follow-up prompt body:\n%s", secondBody)
	}
	if !strings.Contains(secondBody, "User answer to previous assistant question: go ahead with default") {
		t.Fatalf("expected user follow-up answer in prompt body:\n%s", secondBody)
	}
	if !strings.Contains(secondBody, "User explicitly chose the default option.") {
		t.Fatalf("expected default-choice hint in prompt body:\n%s", secondBody)
	}
}

func TestAssistAutoContinuesDefaultQuestionWithoutUserInput(t *testing.T) {
	var calls int32
	var secondBody string
	const question = "Should I proceed with a default wordlist?"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		n := atomic.AddInt32(&calls, 1)
		if n == 2 {
			secondBody = string(body)
		}
		content := `{"type":"complete","final":"done"}`
		if n == 1 {
			content = `{"type":"question","question":"` + question + `"}`
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-followup-autocontinue", "", "")

	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() {
		os.Stdout = orig
	})

	if err := r.handleAssistAgentic("crack the zip in this folder", false, ""); err != nil {
		t.Fatalf("assist error: %v", err)
	}
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	out := buf.String()
	if !strings.Contains(out, "done") {
		t.Fatalf("expected completion output, got:\n%s", out)
	}
	if strings.TrimSpace(r.pendingAssistGoal) != "" {
		t.Fatalf("did not expect paused pending goal")
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected auto-follow-up call, got %d", calls)
	}
	if !strings.Contains(secondBody, "User answer to previous assistant question: go ahead with default") {
		t.Fatalf("expected synthesized follow-up answer in prompt body:\n%s", secondBody)
	}
}

func TestAssistQuestionPausesWhenSpecificInputRequired(t *testing.T) {
	var calls int32
	const question = "Which file path should I use?"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"type":"question","question":"` + question + `"}`,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-followup-pause", "", "")

	if err := r.handleAssistAgentic("read that file", false, ""); err != nil {
		t.Fatalf("assist error: %v", err)
	}
	if strings.TrimSpace(r.pendingAssistGoal) == "" {
		t.Fatalf("expected pending goal for user follow-up")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected single llm call with pause, got %d", calls)
	}
}

func TestAssistRepeatedQuestionGuardRequestsAlternativeAndContinues(t *testing.T) {
	var calls int32
	const question = "Should I proceed with a default wordlist?"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		content := `{"type":"complete","final":"done"}`
		switch n {
		case 1, 2:
			content = `{"type":"question","question":"` + question + `"}`
		case 3:
			content = `{"type":"complete","final":"done"}`
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-repeat-question-guard", "", "")

	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() {
		os.Stdout = orig
	})

	if err := r.handleAssistAgentic("crack the zip in this folder", false, ""); err != nil {
		t.Fatalf("initial assist: %v", err)
	}
	if err := r.handleAssistFollowUp("go ahead with default"); err != nil {
		t.Fatalf("follow-up assist: %v", err)
	}

	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	out := buf.String()
	if !strings.Contains(out, "Repeated step detected; asking assistant for an alternative action.") {
		t.Fatalf("expected repeated-step recovery message, got:\n%s", out)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("expected completion after repeated-question recovery, got:\n%s", out)
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Fatalf("expected at least 3 llm calls, got %d", calls)
	}
}

func TestAssistConcludesZipPresenceFromArtifactImmediately(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		content := `{"done":true,"answer":"Yes, secret.zip exists in the directory.","confidence":"high"}`
		if n == 1 {
			content = `{"type":"command","command":"ls","args":["-la"],"summary":"check files"}`
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-max-steps-zip", "", "")

	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "secret.zip"), []byte("demo"), 0o644); err != nil {
		t.Fatalf("write secret.zip: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() {
		os.Stdout = orig
	})

	if err := r.handleAssistAgentic("check if there is a .zip file in this folder", false, ""); err != nil {
		t.Fatalf("assist error: %v", err)
	}
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	out := buf.String()
	if !strings.Contains(out, "Yes, secret.zip exists in the directory.") {
		t.Fatalf("expected evaluator conclusion, got:\n%s", out)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 llm calls (assist + eval), got %d", calls)
	}
}

func TestAssistRepeatedCommandLoopPausesForGuidance(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"type":"command","command":"ls","args":["-la"],"summary":"check files"}`,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	cfg.Agent.MaxSteps = 8
	r := NewRunner(cfg, "session-repeat-loop-pause", "", "")

	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() {
		os.Stdout = orig
	})

	if err := r.handleAssistAgentic("perform local recon task and keep going", false, ""); err != nil {
		t.Fatalf("assist error: %v", err)
	}
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	out := buf.String()
	if !strings.Contains(out, "Repeated step loop detected. Pausing for guidance") {
		t.Fatalf("expected repeated-loop pause, got:\n%s", out)
	}
	if strings.TrimSpace(r.pendingAssistGoal) == "" {
		t.Fatalf("expected pendingAssistGoal to be set for follow-up")
	}
}

func TestAssistRepeatedLoopDoesNotConsumeStepBudget(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"type":"command","command":"ls","args":["-la"],"summary":"check files"}`,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	cfg.Agent.MaxSteps = 3
	r := NewRunner(cfg, "session-repeat-budget", "", "")

	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() {
		os.Stdout = orig
	})

	if err := r.handleAssistAgentic("perform local recon task", false, ""); err != nil {
		t.Fatalf("assist error: %v", err)
	}
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	out := buf.String()
	if !strings.Contains(out, "Repeated step loop detected. Pausing for guidance") {
		t.Fatalf("expected loop pause, got:\n%s", out)
	}
	if strings.Contains(out, "Reached max steps") {
		t.Fatalf("expected pause before max-steps exhaustion, got:\n%s", out)
	}
}

func TestAssistNoProgressSwitchesToRecoverMode(t *testing.T) {
	var calls int32
	var requestBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(body))
		n := atomic.AddInt32(&calls, 1)
		content := `{"type":"complete","final":"done"}`
		if n <= 2 {
			content = `{"type":"command","command":"ls","args":["-la"],"summary":"check files"}`
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-recover-mode", "", "")

	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() {
		os.Stdout = orig
	})

	if err := r.handleAssistAgentic("perform local recon task and keep going", false, ""); err != nil {
		t.Fatalf("assist error: %v", err)
	}
	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	if !strings.Contains(buf.String(), "done") {
		t.Fatalf("expected completion output, got:\n%s", buf.String())
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Fatalf("expected at least 3 calls, got %d", calls)
	}
	joined := strings.Join(requestBodies, "\n---\n")
	if !strings.Contains(joined, "Mode:\\nrecover\\n") {
		t.Fatalf("expected recover mode after repeated no-progress step; requests:\n%s", joined)
	}
}
