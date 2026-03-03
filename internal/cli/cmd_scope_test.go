package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestHandleScopeAddTargetNormalizesURLAndSavesSessionConfig(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-scope-add", "", "")
	r.reader = bufio.NewReader(strings.NewReader("y\n"))

	if err := r.handleScope([]string{"add-target", "https://www.systemverification.com/about"}); err != nil {
		t.Fatalf("add-target failed: %v", err)
	}
	if len(r.cfg.Scope.Targets) != 1 || r.cfg.Scope.Targets[0] != "www.systemverification.com" {
		t.Fatalf("unexpected scope targets: %#v", r.cfg.Scope.Targets)
	}

	sessionConfigPath := config.SessionPath(cfg.Session.LogDir, "session-scope-add")
	data, err := os.ReadFile(sessionConfigPath)
	if err != nil {
		t.Fatalf("read session config: %v", err)
	}
	if !strings.Contains(string(data), "www.systemverification.com") {
		t.Fatalf("expected saved target in session config, got:\n%s", string(data))
	}
}

func TestHandleScopeAddTargetRequiresApprovalForExternal(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-scope-deny", "", "")
	r.reader = bufio.NewReader(strings.NewReader("n\n"))

	err := r.handleScope([]string{"add-target", "www.systemverification.com"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not approved") {
		t.Fatalf("expected approval rejection, got: %v", err)
	}
}

func TestHandleScopeRemoveTarget(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Scope.Targets = []string{"www.systemverification.com", "192.168.50.1"}
	r := NewRunner(cfg, "session-scope-remove", "", "")

	if err := r.handleScope([]string{"remove-target", "https://www.systemverification.com"}); err != nil {
		t.Fatalf("remove-target failed: %v", err)
	}
	if len(r.cfg.Scope.Targets) != 1 || r.cfg.Scope.Targets[0] != "192.168.50.1" {
		t.Fatalf("unexpected scope targets after remove: %#v", r.cfg.Scope.Targets)
	}

	sessionConfigPath := config.SessionPath(cfg.Session.LogDir, "session-scope-remove")
	if _, err := os.Stat(filepath.Clean(sessionConfigPath)); err != nil {
		t.Fatalf("expected session config to be written: %v", err)
	}
}

