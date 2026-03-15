package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestMaybeEmitGoalSummarySkipsWhenResultSnapshotExists(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = srv.URL
	r := NewRunner(cfg, "session-summary-gate", "", "")

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	logPath := filepath.Join(sessionDir, "logs", "cmd.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	r.lastActionLogPath = logPath

	snapshot := resultSnapshotPath(sessionDir)
	if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	if err := os.WriteFile(snapshot, []byte("# Result Snapshot\n"), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	r.maybeEmitGoalSummary("tell me what is inside secret.zip", false)

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no llm calls when snapshot exists, got %d", got)
	}
}

