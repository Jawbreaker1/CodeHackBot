package cli

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestToolForgeWritesFilesRunsAndUpdatesManifest(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "all"
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Shell.TimeoutSeconds = 5

	r := NewRunner(cfg, "session-tool", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	scriptRel := "demo/run.sh"
	scriptPath := filepath.Join(sessionDir, "artifacts", "tools", scriptRel)
	tool := assist.ToolSpec{
		Language: "bash",
		Name:     "demo",
		Purpose:  "print a marker",
		Files: []assist.ToolFile{
			{Path: scriptRel, Content: "#!/bin/sh\necho tool-ok\n"},
		},
		Run: assist.ToolRun{
			Command: "sh",
			Args:    []string{scriptPath},
		},
	}

	if err := r.executeToolSuggestion(tool, false); err != nil {
		t.Fatalf("executeToolSuggestion: %v", err)
	}

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("script missing: %v", err)
	}
	if !strings.Contains(string(data), "tool-ok") {
		t.Fatalf("unexpected script content: %s", string(data))
	}

	manifestPath := filepath.Join(sessionDir, "artifacts", "tools", "manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if !strings.Contains(string(manifest), "\"name\": \"demo\"") {
		t.Fatalf("manifest missing tool entry: %s", string(manifest))
	}

	// Confirm the run produced a cmd log with output.
	logDir := filepath.Join(sessionDir, "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	found := false
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "cmd-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(logDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(content), "tool-ok") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cmd log to contain tool output")
	}
}

func TestToolForgeRejectsOutOfBoundsPaths(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "all"
	cfg.Tools.Shell.Enabled = true
	r := NewRunner(cfg, "session-tool-bad", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	tool := assist.ToolSpec{
		Language: "bash",
		Name:     "bad",
		Files:    []assist.ToolFile{{Path: "../escape.sh", Content: "echo nope\n"}},
		Run:      assist.ToolRun{Command: "sh", Args: []string{"-c", "echo ok"}},
	}
	if err := r.executeToolSuggestion(tool, false); err == nil {
		t.Fatalf("expected out-of-bounds error")
	}
}

func TestToolForgeAutoFixesWithLLMRecovery(t *testing.T) {
	// LLM server returns a corrected tool spec when the first run fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"type":"tool","tool":{"language":"bash","name":"demo","purpose":"fixed","files":[{"path":"demo/run.sh","content":"#!/bin/sh\necho fixed\nexit 0\n"}],"run":{"command":"sh","args":["demo/run.sh"]}}}`,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "all"
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Shell.TimeoutSeconds = 5
	cfg.LLM.BaseURL = srv.URL
	r := NewRunner(cfg, "session-tool-fix", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	tool := assist.ToolSpec{
		Language: "bash",
		Name:     "demo",
		Purpose:  "broken",
		Files: []assist.ToolFile{
			{Path: "demo/run.sh", Content: "#!/bin/sh\necho broken\nexit 1\n"},
		},
		Run: assist.ToolRun{
			Command: "sh",
			Args:    []string{"demo/run.sh"},
		},
	}

	if err := r.executeToolSuggestion(tool, false); err != nil {
		t.Fatalf("executeToolSuggestion: %v", err)
	}

	// The tool file should now contain the fixed content.
	scriptPath := filepath.Join(sessionDir, "artifacts", "tools", "demo", "run.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read tool: %v", err)
	}
	if !strings.Contains(string(data), "fixed") {
		t.Fatalf("expected fixed script content, got:\n%s", string(data))
	}

	manifestPath := filepath.Join(sessionDir, "artifacts", "tools", "manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	// We expect at least 2 entries: initial broken build + recovery build.
	if strings.Count(string(manifest), "\"name\": \"demo\"") < 2 {
		t.Fatalf("expected multiple manifest entries, got:\n%s", string(manifest))
	}
}

func TestToolForgeApprovalsWriteThenRun(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "default"
	cfg.Permissions.RequireApproval = true
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Shell.TimeoutSeconds = 5

	r := NewRunner(cfg, "session-tool-approve", "", "")
	r.reader = bufio.NewReader(strings.NewReader("y\nn\n")) // approve write, deny run
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	tool := assist.ToolSpec{
		Language: "bash",
		Name:     "demo",
		Files:    []assist.ToolFile{{Path: "demo/run.sh", Content: "#!/bin/sh\necho ok\n"}},
		Run:      assist.ToolRun{Command: "sh", Args: []string{"demo/run.sh"}},
	}
	err = r.executeToolSuggestion(tool, false)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not approved") {
		t.Fatalf("expected not approved error, got: %v", err)
	}

	// File should be written, but no cmd log should exist for the run.
	scriptPath := filepath.Join(sessionDir, "artifacts", "tools", "demo", "run.sh")
	if _, statErr := os.Stat(scriptPath); statErr != nil {
		t.Fatalf("expected script written: %v", statErr)
	}
	logDir := filepath.Join(sessionDir, "logs")
	entries, _ := os.ReadDir(logDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "cmd-") {
			t.Fatalf("expected no cmd logs when run denied")
		}
	}
}

func TestToolForgeReuseSkipsWriteApproval(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "default"
	cfg.Permissions.RequireApproval = true
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Shell.TimeoutSeconds = 5

	r := NewRunner(cfg, "session-tool-reuse", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	tool := assist.ToolSpec{
		Language: "bash",
		Name:     "demo",
		Files:    []assist.ToolFile{{Path: "demo/run.sh", Content: "#!/bin/sh\necho ok\n"}},
		Run:      assist.ToolRun{Command: "sh", Args: []string{"demo/run.sh"}},
	}

	// First run: approve write+run.
	r.reader = bufio.NewReader(strings.NewReader("y\ny\n"))
	if err := r.executeToolSuggestion(tool, false); err != nil {
		t.Fatalf("first executeToolSuggestion: %v", err)
	}

	// Second run: deny run. If it incorrectly asks for write approval again, we'd get "write not approved".
	r.reader = bufio.NewReader(strings.NewReader("n\n"))
	err = r.executeToolSuggestion(tool, false)
	if err == nil {
		t.Fatalf("expected not approved error")
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "write not approved") {
		t.Fatalf("unexpected write approval prompt on reuse; err=%v", err)
	}
	if !strings.Contains(lower, "not approved") {
		t.Fatalf("expected not approved error, got: %v", err)
	}

	// Sanity: file still exists.
	scriptPath := filepath.Join(sessionDir, "artifacts", "tools", "demo", "run.sh")
	if _, statErr := os.Stat(scriptPath); statErr != nil {
		t.Fatalf("expected script: %v", statErr)
	}
}

func TestNormalizeToolRunAddsPythonUnbufferedFlag(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-tool-norm", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	cmd, args := r.normalizeToolRun("python3", []string{"tool.py", "--target", "secret.zip"})
	if cmd != "python3" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if len(args) == 0 || args[0] != "-u" {
		t.Fatalf("expected -u as first arg, got %v", args)
	}
}

func TestNormalizeToolRunKeepsExistingPythonUnbufferedFlag(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-tool-norm-existing", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	_, args := r.normalizeToolRun("python3", []string{"-u", "tool.py"})
	count := 0
	for _, arg := range args {
		if arg == "-u" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one -u flag, got %d in %v", count, args)
	}
}

func TestRewriteMSFConsoleExecuteFlag(t *testing.T) {
	input := "#!/usr/bin/env bash\nmsfconsole -q -e \"db_status; exit\"\necho done\n"
	got, changed := rewriteMSFConsoleExecuteFlag(input)
	if !changed {
		t.Fatalf("expected rewrite to change content")
	}
	if strings.Contains(got, " -e ") {
		t.Fatalf("expected -e to be replaced, got:\n%s", got)
	}
	if !strings.Contains(got, "msfconsole -q -x \"db_status; exit\"") {
		t.Fatalf("expected msfconsole -x command, got:\n%s", got)
	}
}

func TestMaybePatchMSFConsoleScript(t *testing.T) {
	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "check_msf_db.sh")
	content := "#!/usr/bin/env bash\nmsfconsole -q -e \"db_status; exit\"\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	path, patched, err := maybePatchMSFConsoleScript("bash", []string{scriptPath})
	if err != nil {
		t.Fatalf("maybePatchMSFConsoleScript: %v", err)
	}
	if !patched {
		t.Fatalf("expected patch=true")
	}
	if path != scriptPath {
		t.Fatalf("unexpected path: %q", path)
	}
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if strings.Contains(string(data), " -e ") {
		t.Fatalf("expected patched script, got:\n%s", string(data))
	}
}
