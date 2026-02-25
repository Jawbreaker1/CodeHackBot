package orchestrator

import (
	"testing"
	"time"
)

func TestScheduler_DependencyOrderingAndMaxWorkers(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-1",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  2,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
			task("t2", nil, 1),
			task("t3", []string{"t1"}, 1),
		},
	}
	s, err := NewScheduler(plan, 2)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	ready := s.NextLeasable()
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready tasks, got %d", len(ready))
	}
	if ready[0].TaskID != "t1" || ready[1].TaskID != "t2" {
		t.Fatalf("unexpected ready order: %s, %s", ready[0].TaskID, ready[1].TaskID)
	}
	if err := s.MarkLeased("t1"); err != nil {
		t.Fatalf("MarkLeased t1: %v", err)
	}
	if err := s.MarkLeased("t2"); err != nil {
		t.Fatalf("MarkLeased t2: %v", err)
	}
	if ready := s.NextLeasable(); len(ready) != 0 {
		t.Fatalf("expected no capacity, got %d tasks", len(ready))
	}
	if err := s.MarkRunning("t1"); err != nil {
		t.Fatalf("MarkRunning t1: %v", err)
	}
	if err := s.MarkRunning("t2"); err != nil {
		t.Fatalf("MarkRunning t2: %v", err)
	}
	if err := s.MarkCompleted("t1"); err != nil {
		t.Fatalf("MarkCompleted t1: %v", err)
	}
	if ready := s.NextLeasable(); len(ready) != 1 || ready[0].TaskID != "t3" {
		t.Fatalf("expected t3 to become ready after t1 completion")
	}
}

func TestScheduler_RetryAndBlockedMapping(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-2",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	s, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	s.SetRetryBackoff([]time.Duration{5 * time.Second, 15 * time.Second})

	if err := s.MarkLeased("t1"); err != nil {
		t.Fatalf("MarkLeased: %v", err)
	}
	if err := s.MarkRunning("t1"); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	if err := s.MarkFailed("t1", "exit_status_1", true, 2); err != nil {
		t.Fatalf("MarkFailed retryable: %v", err)
	}
	if st, _ := s.State("t1"); st != TaskStateQueued {
		t.Fatalf("expected queued after retry, got %s", st)
	}
	if attempt, _ := s.Attempt("t1"); attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", attempt)
	}
	if ready := s.NextLeasable(); len(ready) != 0 {
		t.Fatalf("expected backoff to block immediate retry")
	}
	now = now.Add(6 * time.Second)
	if ready := s.NextLeasable(); len(ready) != 1 || ready[0].TaskID != "t1" {
		t.Fatalf("expected task t1 to be leasable after backoff")
	}

	if err := s.MarkLeased("t1"); err != nil {
		t.Fatalf("MarkLeased #2: %v", err)
	}
	if err := s.MarkRunning("t1"); err != nil {
		t.Fatalf("MarkRunning #2: %v", err)
	}
	if err := s.MarkFailed("t1", "approval_timeout", false, 2); err != nil {
		t.Fatalf("MarkFailed blocked reason: %v", err)
	}
	if st, _ := s.State("t1"); st != TaskStateBlocked {
		t.Fatalf("expected blocked state, got %s", st)
	}
}

func TestScheduler_StopPropagation(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-3",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  2,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
			task("t2", nil, 1),
		},
	}
	s, err := NewScheduler(plan, 2)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	_ = s.MarkLeased("t1")
	_ = s.MarkRunning("t1")
	s.StopAll()
	for _, id := range []string{"t1", "t2"} {
		st, ok := s.State(id)
		if !ok {
			t.Fatalf("missing state for %s", id)
		}
		if st != TaskStateCanceled {
			t.Fatalf("expected canceled state for %s, got %s", id, st)
		}
	}
}

func TestScheduler_InvalidGraph(t *testing.T) {
	t.Parallel()

	planUnknownDep := RunPlan{
		RunID:           "run-4",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", []string{"missing"}, 1),
		},
	}
	if _, err := NewScheduler(planUnknownDep, 1); err == nil {
		t.Fatalf("expected unknown dep error")
	}

	planCycle := RunPlan{
		RunID:           "run-5",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", []string{"t2"}, 1),
			task("t2", []string{"t1"}, 1),
		},
	}
	if _, err := NewScheduler(planCycle, 1); err == nil {
		t.Fatalf("expected cycle error")
	}
}

func TestScheduler_AddTaskDynamicGraphMutation(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-dynamic",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  2,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	s, err := NewScheduler(plan, 2)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := s.AddTask(task("t2", []string{"t1"}, 2)); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := s.MarkLeased("t1"); err != nil {
		t.Fatalf("MarkLeased t1: %v", err)
	}
	if err := s.MarkRunning("t1"); err != nil {
		t.Fatalf("MarkRunning t1: %v", err)
	}
	if err := s.MarkCompleted("t1"); err != nil {
		t.Fatalf("MarkCompleted t1: %v", err)
	}
	ready := s.NextLeasable()
	if len(ready) != 1 || ready[0].TaskID != "t2" {
		t.Fatalf("expected dynamic task t2 to become leasable, got %#v", ready)
	}
}

func TestScheduler_IsDoneWhenQueuedTaskHasTerminalDependency(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-terminal-deps",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
			task("t2", []string{"t1"}, 1),
		},
	}
	s, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := s.MarkLeased("t1"); err != nil {
		t.Fatalf("MarkLeased t1: %v", err)
	}
	if err := s.MarkRunning("t1"); err != nil {
		t.Fatalf("MarkRunning t1: %v", err)
	}
	if err := s.MarkFailed("t1", "command_failed", false, 1); err != nil {
		t.Fatalf("MarkFailed t1: %v", err)
	}
	if !s.IsDone() {
		t.Fatalf("expected scheduler done when remaining queued task depends on terminal failure")
	}
}

func TestScheduler_IsDoneWhenQueuedTaskHasTransitiveTerminalDependency(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-terminal-deps-transitive",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
			task("t2", []string{"t1"}, 1),
			task("t3", []string{"t2"}, 1),
		},
	}
	s, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := s.MarkLeased("t1"); err != nil {
		t.Fatalf("MarkLeased t1: %v", err)
	}
	if err := s.MarkRunning("t1"); err != nil {
		t.Fatalf("MarkRunning t1: %v", err)
	}
	if err := s.MarkFailed("t1", "command_failed", false, 1); err != nil {
		t.Fatalf("MarkFailed t1: %v", err)
	}
	if !s.IsDone() {
		t.Fatalf("expected scheduler done when queued tasks only have transitive terminal dependency failures")
	}
}

func TestScheduler_IsNotDoneWhenQueuedTaskCanStillRun(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-pending-deps",
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
			task("t2", []string{"t1"}, 1),
		},
	}
	s, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := s.MarkLeased("t1"); err != nil {
		t.Fatalf("MarkLeased t1: %v", err)
	}
	if err := s.MarkRunning("t1"); err != nil {
		t.Fatalf("MarkRunning t1: %v", err)
	}
	if s.IsDone() {
		t.Fatalf("expected scheduler not done while dependency is still running")
	}
}

func task(id string, deps []string, priority int) TaskSpec {
	return TaskSpec{
		TaskID:            id,
		Goal:              "goal",
		DependsOn:         deps,
		Priority:          priority,
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"fail"},
		ExpectedArtifacts: []string{"artifact"},
		RiskLevel:         "recon_readonly",
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   time.Minute,
		},
	}
}
