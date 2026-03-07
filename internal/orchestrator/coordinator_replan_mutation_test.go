package orchestrator

import (
	"strings"
	"testing"
	"time"
)

func TestMaybePromoteExecutionFailureRetryTaskMutatesUnavailableVulnWrapper(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-replan-mutate-vuln-wrapper"
	manager := NewManager(base)
	taskSpec := TaskSpec{
		TaskID:            "T-03-vuln-mapping",
		Goal:              "Map discovered vulnerabilities to CVEs with source validation",
		Targets:           []string{"192.168.50.1"},
		Priority:          90,
		Strategy:          "vulnerability_mapping",
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"fail"},
		ExpectedArtifacts: []string{"cve-map.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "nmap-vuln-checker",
			Args:    []string{"-i", "nmap_vuln_scan_192.168.50.1.xml"},
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   5 * time.Minute,
		},
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.1"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{taskSpec},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := scheduler.SetAttempt(taskSpec.TaskID, 2); err != nil {
		t.Fatalf("SetAttempt: %v", err)
	}
	coord := &Coordinator{
		runID:     runID,
		manager:   manager,
		scheduler: scheduler,
	}
	payload := map[string]any{
		"attempt": 1,
		"reason":  WorkerFailureCommandFailed,
		"error":   `exec: "nmap-vuln-checker": executable file not found in $PATH`,
		"command": "nmap-vuln-checker",
		"args":    []any{"-i", "nmap_vuln_scan_192.168.50.1.xml"},
	}

	meta := coord.maybePromoteExecutionFailureRetryTask(EventEnvelope{TaskID: taskSpec.TaskID}, payload)
	if got, _ := meta["source_task_mutated"].(bool); !got {
		t.Fatalf("expected source task mutation metadata, got %#v", meta)
	}
	mutated, ok := scheduler.Task(taskSpec.TaskID)
	if !ok {
		t.Fatalf("expected mutated task in scheduler")
	}
	if mutated.Action.Type != "assist" {
		t.Fatalf("expected scheduler action type assist, got %q", mutated.Action.Type)
	}
	stored, err := manager.ReadTask(runID, taskSpec.TaskID)
	if err != nil {
		t.Fatalf("ReadTask: %v", err)
	}
	if stored.Action.Type != "assist" {
		t.Fatalf("expected persisted action type assist, got %q", stored.Action.Type)
	}
}

func TestMaybePromoteExecutionFailureRetryTaskMutatesUnavailableVulnersWrapperScript(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-replan-mutate-vulners-wrapper-script"
	manager := NewManager(base)
	taskSpec := TaskSpec{
		TaskID:            "T-04-vuln-mapping",
		Goal:              "Map discovered services to CVEs with source validation",
		Targets:           []string{"192.168.50.1"},
		Priority:          90,
		Strategy:          "vulnerability_mapping",
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"fail"},
		ExpectedArtifacts: []string{"cve-map.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "/home/johan/birdhackbot/CodeHackBot/tools/nmap_vulners_wrapper.sh",
			Args:    []string{"-i", "nmap_vuln_scan_192.168.50.1.xml"},
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   5 * time.Minute,
		},
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.1"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{taskSpec},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := scheduler.SetAttempt(taskSpec.TaskID, 2); err != nil {
		t.Fatalf("SetAttempt: %v", err)
	}
	coord := &Coordinator{
		runID:     runID,
		manager:   manager,
		scheduler: scheduler,
	}
	payload := map[string]any{
		"attempt": 1,
		"reason":  WorkerFailureCommandFailed,
		"error":   `fork/exec /home/johan/birdhackbot/CodeHackBot/tools/nmap_vulners_wrapper.sh: executable file not found in $PATH`,
		"command": "/home/johan/birdhackbot/CodeHackBot/tools/nmap_vulners_wrapper.sh",
		"args":    []any{"-i", "nmap_vuln_scan_192.168.50.1.xml"},
	}

	meta := coord.maybePromoteExecutionFailureRetryTask(EventEnvelope{TaskID: taskSpec.TaskID}, payload)
	if got, _ := meta["source_task_mutated"].(bool); !got {
		t.Fatalf("expected source task mutation metadata, got %#v", meta)
	}
	mutated, ok := scheduler.Task(taskSpec.TaskID)
	if !ok {
		t.Fatalf("expected mutated task in scheduler")
	}
	if mutated.Action.Type != "assist" {
		t.Fatalf("expected scheduler action type assist, got %q", mutated.Action.Type)
	}
}

func TestMaybePromoteExecutionFailureRetryTaskSkipsNonVulnCommands(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-replan-mutate-skip-non-vuln"
	manager := NewManager(base)
	taskSpec := TaskSpec{
		TaskID:            "T-01-recon",
		Goal:              "Perform basic host discovery",
		Targets:           []string{"192.168.50.1"},
		Priority:          70,
		Strategy:          "recon_seed",
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"fail"},
		ExpectedArtifacts: []string{"recon.log"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "nmap",
			Args:    []string{"-sV", "192.168.50.1"},
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   5 * time.Minute,
		},
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.1"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{taskSpec},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := scheduler.SetAttempt(taskSpec.TaskID, 2); err != nil {
		t.Fatalf("SetAttempt: %v", err)
	}
	coord := &Coordinator{
		runID:     runID,
		manager:   manager,
		scheduler: scheduler,
	}
	payload := map[string]any{
		"attempt": 1,
		"reason":  WorkerFailureCommandFailed,
		"error":   `exec: "nmap": executable file not found in $PATH`,
		"command": "nmap",
	}

	meta := coord.maybePromoteExecutionFailureRetryTask(EventEnvelope{TaskID: taskSpec.TaskID}, payload)
	if got, _ := meta["source_task_mutated"].(bool); got {
		t.Fatalf("did not expect source task mutation metadata, got %#v", meta)
	}
	mutated, ok := scheduler.Task(taskSpec.TaskID)
	if !ok {
		t.Fatalf("expected task in scheduler")
	}
	if mutated.Action.Type != "command" {
		t.Fatalf("expected scheduler action type command, got %q", mutated.Action.Type)
	}
}

func TestBuildReplanTask_UsesCandidateCVEFollowupGoal(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-replan-candidate-cve-followup"
	manager := NewManager(base)
	taskSpec := TaskSpec{
		TaskID:            "T-04-report-generation",
		Goal:              "Generate OWASP report",
		Targets:           []string{"192.168.50.1"},
		Priority:          90,
		Strategy:          "report_generation",
		DoneWhen:          []string{"report_generated"},
		FailWhen:          []string{"report_failed"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "echo",
			Args:    []string{"ok"},
		},
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   time.Minute,
		},
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.1"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{taskSpec},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	coord := &Coordinator{
		runID:     runID,
		manager:   manager,
		scheduler: scheduler,
	}
	source := EventEnvelope{TaskID: taskSpec.TaskID}
	payload := map[string]any{
		"finding_type":   findingTypeCVECandidateClaim,
		"cve":            "CVE-2010-0738",
		"claim_reason":   "missing_phase_requirements: lookup_source",
		"missing_phases": "lookup_source",
	}
	task, err := coord.buildReplanTask("new_finding", source, payload, "mutation-key-cve-followup")
	if err != nil {
		t.Fatalf("buildReplanTask: %v", err)
	}
	if !strings.Contains(task.Goal, "CVE-2010-0738") {
		t.Fatalf("expected candidate CVE in goal, got %q", task.Goal)
	}
	if task.Action.Type != "assist" {
		t.Fatalf("expected assist action type, got %q", task.Action.Type)
	}
	if task.RiskLevel != string(RiskActiveProbe) {
		t.Fatalf("expected active probe risk for candidate follow-up, got %q", task.RiskLevel)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != taskSpec.TaskID {
		t.Fatalf("expected candidate follow-up to depend on source task %q, got %#v", taskSpec.TaskID, task.DependsOn)
	}
	if !strings.Contains(strings.ToLower(task.Strategy), "adaptive_replan") {
		t.Fatalf("expected adaptive replan strategy, got %q", task.Strategy)
	}
}

func TestBuildReplanTask_AdaptiveRecoveryDependsOnSourceTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-replan-adaptive-depends-on-source"
	manager := NewManager(base)
	taskSpec := TaskSpec{
		TaskID:            "T-02-archive-metadata",
		Goal:              "Inspect archive metadata",
		Targets:           []string{"secret.zip"},
		Priority:          80,
		Strategy:          "archive_metadata",
		DoneWhen:          []string{"metadata_ready"},
		FailWhen:          []string{"metadata_failed"},
		ExpectedArtifacts: []string{"zip_metadata.txt"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "zipinfo",
			Args:    []string{"-v", "secret.zip"},
		},
		Budget: TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   time.Minute,
		},
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"secret.zip"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{taskSpec},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	coord := &Coordinator{
		runID:     runID,
		manager:   manager,
		scheduler: scheduler,
	}
	task, err := coord.buildReplanTask(
		"execution_failure",
		EventEnvelope{TaskID: taskSpec.TaskID},
		map[string]any{
			"reason":                WorkerFailureCommandFailed,
			"latest_result_summary": "zipinfo failed",
			"latest_evidence_refs":  []any{"/tmp/run/T-02/worker.log"},
			"latest_input_refs":     []any{"/home/johan/birdhackbot/CodeHackBot/secret.zip"},
		},
		"rp:adaptive-recovery-depends-on-source",
	)
	if err != nil {
		t.Fatalf("buildReplanTask: %v", err)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != taskSpec.TaskID {
		t.Fatalf("expected adaptive recovery to depend on source task %q, got %#v", taskSpec.TaskID, task.DependsOn)
	}
}

func TestMaybeMutateTaskGraph_SuppressesRecursiveAdaptiveNoProgress(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-replan-suppress-recursive-adaptive"
	manager := NewManager(base)
	taskSpec := TaskSpec{
		TaskID:            "task-rp-seed",
		Goal:              "Adaptive recovery task",
		Targets:           []string{"192.168.50.1"},
		Priority:          80,
		Strategy:          "adaptive_replan_execution_failure",
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"fail"},
		ExpectedArtifacts: []string{"adaptive-recovery-task-rp-seed.log"},
		RiskLevel:         string(RiskActiveProbe),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "recover",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   5 * time.Minute,
		},
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.1"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{taskSpec},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	coord := &Coordinator{
		runID:                  runID,
		manager:                manager,
		scheduler:              scheduler,
		seenReplanMutationKeys: map[string]struct{}{},
	}
	out := coord.maybeMutateTaskGraph(
		"execution_failure",
		EventEnvelope{TaskID: taskSpec.TaskID},
		map[string]any{"reason": WorkerFailureNoProgress},
		"rp:suppress-adaptive-no-progress",
	)
	if got, _ := out["graph_mutation"].(bool); got {
		t.Fatalf("expected no graph mutation for recursive adaptive no_progress, got %#v", out)
	}
	if strings.TrimSpace(toString(out["mutation_status"])) != "adaptive_failure_terminal_no_progress" {
		t.Fatalf("expected adaptive_failure_terminal_no_progress, got %#v", out)
	}
}

func TestBuildReplanTask_RecursiveAdaptivePriorityIsReduced(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-replan-recursive-priority-reduced"
	manager := NewManager(base)
	taskSpec := TaskSpec{
		TaskID:            "task-rp-seed",
		Goal:              "Adaptive recovery task",
		Targets:           []string{"192.168.50.1"},
		Priority:          80,
		Strategy:          "adaptive_replan_execution_failure",
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"fail"},
		ExpectedArtifacts: []string{"adaptive-recovery-task-rp-seed.log"},
		RiskLevel:         string(RiskActiveProbe),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "recover",
		},
		Budget: TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 6,
			MaxRuntime:   5 * time.Minute,
		},
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.1"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{taskSpec},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	coord := &Coordinator{
		runID:     runID,
		manager:   manager,
		scheduler: scheduler,
	}
	task, err := coord.buildReplanTask(
		"execution_failure",
		EventEnvelope{TaskID: taskSpec.TaskID},
		map[string]any{"reason": WorkerFailureNoProgress},
		"rp:recursive-priority",
	)
	if err != nil {
		t.Fatalf("buildReplanTask: %v", err)
	}
	if task.Priority >= taskSpec.Priority {
		t.Fatalf("expected recursive adaptive priority to be reduced, base=%d got=%d", taskSpec.Priority, task.Priority)
	}
}

func TestParseReplanBucketBudgets(t *testing.T) {
	t.Parallel()

	got := parseReplanBucketBudgets([]string{
		"max_replans_candidate_cve_followup=3",
		"replan_budget_candidate_cve=5",
	})
	if got[replanMutationBucketCandidateCVEFollow] != 5 {
		t.Fatalf("expected candidate bucket budget override=5, got %#v", got)
	}
}

func TestMaybeMutateTaskGraph_CandidateBucketBudgetExhaustionDoesNotStopRun(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-replan-candidate-bucket-budget"
	manager := NewManager(base)
	taskSpec := TaskSpec{
		TaskID:            "T-01-report",
		Goal:              "Generate report",
		Targets:           []string{"192.168.50.1"},
		Priority:          80,
		Strategy:          "report_generation",
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"fail"},
		ExpectedArtifacts: []string{"owasp_report.md"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:    "command",
			Command: "echo",
			Args:    []string{"ok"},
		},
		Budget: TaskBudget{
			MaxSteps:     1,
			MaxToolCalls: 1,
			MaxRuntime:   time.Minute,
		},
	}
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.1"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"max_replans=5", "max_replans_candidate_cve_followup=1"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{taskSpec},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	coord := &Coordinator{
		runID:                       runID,
		manager:                     manager,
		scheduler:                   scheduler,
		startupTimeout:              2 * time.Second,
		seenReplanMutationKeys:      map[string]struct{}{},
		replanMutationBucketCounts:  map[string]int{},
		replanMutationBucketBudgets: map[string]int{},
	}
	coord.ensureReplanBudget()

	source1 := EventEnvelope{EventID: "e-cve-1", TaskID: taskSpec.TaskID}
	payload1 := map[string]any{
		"finding_type": findingTypeCVECandidateClaim,
		"finding_key":  "k-cve-1",
		"cve":          "CVE-2010-0738",
	}
	out1 := coord.maybeMutateTaskGraph("new_finding", source1, payload1, "mk-cve-1")
	if got, _ := out1["graph_mutation"].(bool); !got {
		t.Fatalf("expected first candidate mutation to succeed, got %#v", out1)
	}

	source2 := EventEnvelope{EventID: "e-cve-2", TaskID: taskSpec.TaskID}
	payload2 := map[string]any{
		"finding_type": findingTypeCVECandidateClaim,
		"finding_key":  "k-cve-2",
		"cve":          "CVE-2011-2523",
	}
	out2 := coord.maybeMutateTaskGraph("new_finding", source2, payload2, "mk-cve-2")
	if got, _ := out2["graph_mutation"].(bool); got {
		t.Fatalf("expected second candidate mutation to be bucket-limited, got %#v", out2)
	}
	if status := strings.TrimSpace(toString(out2["mutation_status"])); status != "replan_bucket_budget_exhausted" {
		t.Fatalf("expected replan_bucket_budget_exhausted, got %#v", out2)
	}
	if outcome := strings.TrimSpace(toString(out2["outcome"])); outcome != string(ReplanOutcomeRefineTask) {
		t.Fatalf("expected refine_task outcome for bucket exhaustion, got %#v", out2)
	}
	runStatus, err := manager.Status(runID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if runStatus.State == "stopped" {
		t.Fatalf("expected run to keep running on bucket exhaustion, got stopped")
	}

	source3 := EventEnvelope{EventID: "e-loop-1", TaskID: taskSpec.TaskID}
	out3 := coord.maybeMutateTaskGraph("repeated_step_loop", source3, map[string]any{"reason": "repeated_step_loop"}, "mk-loop-1")
	if got, _ := out3["graph_mutation"].(bool); !got {
		t.Fatalf("expected non-candidate mutation to proceed after candidate bucket exhaustion, got %#v", out3)
	}
}
