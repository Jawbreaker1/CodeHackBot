package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestHandlePlanSuggestionContinuesAfterRecoveredStepFailure(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.ReadAll(r.Body)
		n := atomic.AddInt32(&calls, 1)
		content := `{"type":"command","command":"echo","args":["step3"]}`
		switch n {
		case 1:
			content = `{"type":"command","command":"echo","args":["step1"]}`
		case 2:
			content = `{"type":"command","command":"zip2john","args":["./secret.zip",">","zip.hash"],"summary":"extract hash"}`
		case 3:
			content = `{"type":"command","command":"pwd"}`
		case 4:
			content = `{"type":"command","command":"echo","args":["step3"]}`
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
	cfg.Tools.Shell.Enabled = true
	r := NewRunner(cfg, "session-plan-continue", "", "")

	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.handlePlanSuggestion(assist.Suggestion{
		Type:  "plan",
		Steps: []string{"first", "second", "third"},
	}, false)
	if err != nil {
		t.Fatalf("handlePlanSuggestion: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got < 4 {
		t.Fatalf("expected at least 4 llm calls (step1, step2, recover, step3), got %d", got)
	}

	obs, ok := r.latestObservation()
	if !ok {
		t.Fatalf("expected latest observation")
	}
	if strings.TrimSpace(obs.Command) != "echo" {
		t.Fatalf("expected final observation command echo, got %q", obs.Command)
	}
	if len(obs.Args) == 0 || obs.Args[0] != "step3" {
		t.Fatalf("expected final observation args to include step3, got %#v", obs.Args)
	}
}

func TestHandlePlanSuggestionRunsBoundedRecoveryWhenObjectiveUnverified(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.ReadAll(r.Body)
		n := atomic.AddInt32(&calls, 1)
		content := `{"type":"command","command":"echo","args":["recover-2"]}`
		switch n {
		case 1:
			content = `{"type":"command","command":"echo","args":["plan-step-1"]}`
		case 2:
			content = `{"type":"command","command":"echo","args":["recover-1"]}`
		case 3:
			content = `{"type":"command","command":"echo","args":["recover-2"]}`
		case 4:
			content = `{"type":"noop","summary":"done"}`
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
	cfg.Tools.Shell.Enabled = true
	r := NewRunner(cfg, "session-plan-post-recover", "", "")
	r.assistRuntime.Goal = "perform local recon task"

	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.handlePlanSuggestion(assist.Suggestion{
		Type:  "plan",
		Steps: []string{"first"},
	}, false)
	if err != nil {
		t.Fatalf("handlePlanSuggestion: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got < 3 {
		t.Fatalf("expected at least 3 llm calls (step + bounded recover), got %d", got)
	}

	obs, ok := r.latestObservation()
	if !ok {
		t.Fatalf("expected latest observation")
	}
	if strings.TrimSpace(obs.Command) != "echo" {
		t.Fatalf("expected final observation command echo, got %q", obs.Command)
	}
	if len(obs.Args) == 0 || obs.Args[0] != "recover-2" {
		t.Fatalf("expected bounded recover step to execute, got %#v", obs.Args)
	}
}
