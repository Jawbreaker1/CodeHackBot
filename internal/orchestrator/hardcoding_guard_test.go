package orchestrator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAdaptationPathsAvoidScenarioLiterals(t *testing.T) {
	root := repoRootFromTest(t)
	files := []string{
		"internal/cli/assist_command_contract.go",
		"internal/cli/assist_recovery.go",
		"internal/cli/cmd_exec.go",
		"internal/cli/tool_forge.go",
		"internal/orchestrator/command_adaptation.go",
		"internal/orchestrator/runtime_archive_workflow.go",
		"internal/orchestrator/runtime_command_prepare.go",
		"internal/orchestrator/runtime_command_utils.go",
		"internal/orchestrator/runtime_input_repair.go",
		"internal/orchestrator/runtime_vulnerability_evidence.go",
		"internal/orchestrator/worker_runtime_command_repair.go",
	}
	blocked := []string{
		"secret.zip",
		"192.168.50.1",
		"johans-iphone",
		"systemverification.com",
		"scanme.nmap.org",
	}

	var violations []string
	for _, rel := range files {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := strings.ToLower(string(data))
		for _, token := range blocked {
			if strings.Contains(text, token) {
				violations = append(violations, rel+" contains "+token)
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("scenario-literal guard failed:\n%s", strings.Join(violations, "\n"))
	}
}

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
