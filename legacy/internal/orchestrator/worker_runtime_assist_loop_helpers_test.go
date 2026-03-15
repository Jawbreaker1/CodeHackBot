package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectRecoverLocalInspection(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "artifact", "T-02")
	tests := []struct {
		name    string
		command string
		args    []string
		wantOK  bool
	}{
		{
			name:    "list dir current workspace",
			command: "list_dir",
			args:    []string{"."},
			wantOK:  true,
		},
		{
			name:    "read file current workspace",
			command: "read_file",
			args:    []string{"worker.log"},
			wantOK:  true,
		},
		{
			name:    "read file outside workspace",
			command: "read_file",
			args:    []string{"../T-01/dependency.log"},
			wantOK:  false,
		},
		{
			name:    "non-local command",
			command: "cat",
			args:    []string{"worker.log"},
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := detectRecoverLocalInspection(workDir, tc.command, tc.args)
			if got != tc.wantOK {
				t.Fatalf("detectRecoverLocalInspection(%q, %v) = %v, want %v", tc.command, tc.args, got, tc.wantOK)
			}
		})
	}
}

func TestIsAssistInspectionOnlyAction(t *testing.T) {
	tests := []struct {
		command string
		args    []string
		want    bool
	}{
		{command: "read_file", args: []string{"a.log"}, want: true},
		{command: "list_dir", args: []string{"."}, want: true},
		{command: "searchsploit", args: []string{"nginx"}, want: false},
	}
	for _, tc := range tests {
		if got := isAssistInspectionOnlyAction(tc.command, tc.args); got != tc.want {
			t.Fatalf("isAssistInspectionOnlyAction(%q, %v)=%v want=%v", tc.command, tc.args, got, tc.want)
		}
	}
}

func TestDetectRecoverExecutionLogInspection(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-exec-log-inspection",
		TaskID:      "T-02",
	}
	paths, err := EnsureRunLayout(base, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	taskDir := filepath.Join(paths.ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("MkdirAll taskDir: %v", err)
	}
	logPath := filepath.Join(taskDir, "worker-T-02-a1-a1-s2-t2.log")
	if err := os.WriteFile(logPath, []byte("inspection transcript"), 0o644); err != nil {
		t.Fatalf("WriteFile logPath: %v", err)
	}
	feedback := &assistExecutionFeedback{
		Command: "nmap",
		Args:    []string{"-sV", "127.0.0.1"},
		LogPath: logPath,
	}

	gotPath, got := detectRecoverExecutionLogInspection(cfg, "read_file", []string{logPath}, feedback)
	if !got {
		t.Fatalf("expected execution-log inspection to be detected")
	}
	if gotPath != logPath {
		t.Fatalf("expected detected path %q, got %q", logPath, gotPath)
	}
	if _, got := detectRecoverExecutionLogInspection(cfg, "read_file", []string{filepath.Join(taskDir, "service_scan.txt")}, feedback); got {
		t.Fatalf("did not expect non-worker log evidence file to be treated as execution-log churn")
	}
}

func TestDetectRecoverDependencyArtifactRegression(t *testing.T) {
	dependencyArtifacts := []string{
		"/tmp/T-01/nmap_scan_192.168.50.1.xml",
	}
	feedback := &assistExecutionFeedback{
		Command:             "nmap",
		Args:                []string{"-oX", "/tmp/T-02/nmap_vuln_scan_192.168.50.1.xml", "192.168.50.1"},
		PrimaryArtifactRefs: []string{"/tmp/T-02/nmap_vuln_scan_192.168.50.1.xml"},
		LogPath:             "/tmp/T-02/worker-T-02.log",
	}

	stale, preferred, got := detectRecoverDependencyArtifactRegression(
		"read_file",
		[]string{"/tmp/T-01/nmap_scan_192.168.50.1.xml"},
		dependencyArtifacts,
		feedback,
	)
	if !got {
		t.Fatalf("expected dependency artifact regression to be detected")
	}
	if stale != "/tmp/T-01/nmap_scan_192.168.50.1.xml" {
		t.Fatalf("expected stale dependency artifact path, got %q", stale)
	}
	if preferred != "/tmp/T-02/nmap_vuln_scan_192.168.50.1.xml" {
		t.Fatalf("expected preferred primary evidence path, got %q", preferred)
	}
}

func TestShouldEnforceAssistInspectionGuards(t *testing.T) {
	if !shouldEnforceAssistInspectionGuards(TaskSpec{Strategy: "vuln_mapping_validation"}) {
		t.Fatalf("expected vuln mapping strategy to enforce inspection guards")
	}
	if !shouldEnforceAssistInspectionGuards(TaskSpec{Strategy: "adaptive_replan_execution_failure"}) {
		t.Fatalf("expected adaptive execution failure strategy to enforce inspection guards")
	}
	if !shouldEnforceAssistInspectionGuards(TaskSpec{
		Goal:     "crack secret.zip password and extract contents",
		Strategy: "local_archive_recovery",
	}) {
		t.Fatalf("expected archive proof-sensitive workflow to enforce inspection guards")
	}
	if shouldEnforceAssistInspectionGuards(TaskSpec{Strategy: "summary"}) {
		t.Fatalf("did not expect summary strategy to enforce inspection guards")
	}
}

func TestDeriveAssistLoopCaps_DefaultFloor(t *testing.T) {
	task := TaskSpec{
		Title:    "quick task",
		Goal:     "list files",
		Strategy: "summary",
		Budget: TaskBudget{
			MaxSteps:     0,
			MaxToolCalls: 0,
		},
	}

	caps := deriveAssistLoopCaps(task)
	if caps.MaxActionSteps != 6 {
		t.Fatalf("expected default action step floor 6, got %d", caps.MaxActionSteps)
	}
	if caps.MaxToolCalls != 6 {
		t.Fatalf("expected default tool call floor 6, got %d", caps.MaxToolCalls)
	}
	if caps.MaxRecoverTransitions != workerAssistMaxRecoveries {
		t.Fatalf("expected default recover transition cap %d, got %d", workerAssistMaxRecoveries, caps.MaxRecoverTransitions)
	}
	if caps.NoNewEvidenceToolCallCap != workerAssistNoNewEvidenceToolCallCap {
		t.Fatalf("expected default no-new-evidence cap %d, got %d", workerAssistNoNewEvidenceToolCallCap, caps.NoNewEvidenceToolCallCap)
	}
}

func TestDeriveAssistLoopCaps_DeepValidationFloor(t *testing.T) {
	task := TaskSpec{
		Title:    "Validate CVE mapping",
		Goal:     "validate candidate findings with source evidence and bounded checks",
		Strategy: "vuln_mapping_validation",
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 5,
		},
		Action: TaskAction{
			Type:   "assist",
			Prompt: "perform exploit validation and verify applicability",
		},
	}

	caps := deriveAssistLoopCaps(task)
	if caps.MaxActionSteps < 12 {
		t.Fatalf("expected deep validation step floor >=12, got %d", caps.MaxActionSteps)
	}
	if caps.MaxToolCalls < 16 {
		t.Fatalf("expected deep validation tool-call floor >=16, got %d", caps.MaxToolCalls)
	}
	if caps.MaxRecoverTransitions < 6 {
		t.Fatalf("expected deep validation recover transition floor >=6, got %d", caps.MaxRecoverTransitions)
	}
	if caps.NoNewEvidenceToolCallCap < 12 {
		t.Fatalf("expected deep validation no-new-evidence cap >=12, got %d", caps.NoNewEvidenceToolCallCap)
	}
}
