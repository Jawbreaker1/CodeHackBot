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
