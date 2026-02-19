package orchestrator

import (
	"fmt"
	"sort"
	"time"
)

type Scheduler struct {
	tasks      map[string]TaskSpec
	state      map[string]TaskState
	attempts   map[string]int
	maxWorkers int
	now        func() time.Time
	retryAfter map[string]time.Time
	backoff    []time.Duration
}

func NewScheduler(plan RunPlan, maxWorkers int) (*Scheduler, error) {
	if err := ValidateRunPlan(plan); err != nil {
		return nil, err
	}
	if maxWorkers <= 0 {
		return nil, fmt.Errorf("max workers must be > 0")
	}
	tasks := make(map[string]TaskSpec, len(plan.Tasks))
	state := make(map[string]TaskState, len(plan.Tasks))
	attempts := make(map[string]int, len(plan.Tasks))
	for _, task := range plan.Tasks {
		tasks[task.TaskID] = task
		state[task.TaskID] = TaskStateQueued
		attempts[task.TaskID] = 1
	}
	for _, task := range plan.Tasks {
		for _, dep := range task.DependsOn {
			if _, ok := tasks[dep]; !ok {
				return nil, fmt.Errorf("task %s depends on unknown task %s", task.TaskID, dep)
			}
		}
	}
	if err := validateAcyclic(tasks); err != nil {
		return nil, err
	}
	return &Scheduler{
		tasks:      tasks,
		state:      state,
		attempts:   attempts,
		maxWorkers: maxWorkers,
		now:        func() time.Time { return time.Now().UTC() },
		retryAfter: map[string]time.Time{},
		backoff:    []time.Duration{5 * time.Second, 15 * time.Second},
	}, nil
}

func (s *Scheduler) NextLeasable() []TaskSpec {
	capacity := s.maxWorkers - s.activeSlots()
	if capacity <= 0 {
		return nil
	}

	candidates := make([]TaskSpec, 0)
	for id, task := range s.tasks {
		if s.state[id] != TaskStateQueued {
			continue
		}
		if until, ok := s.retryAfter[id]; ok && s.now().Before(until) {
			continue
		}
		if !s.dependenciesMet(task) {
			continue
		}
		candidates = append(candidates, task)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority == candidates[j].Priority {
			return candidates[i].TaskID < candidates[j].TaskID
		}
		// Higher priority first.
		return candidates[i].Priority > candidates[j].Priority
	})
	if len(candidates) > capacity {
		return candidates[:capacity]
	}
	return candidates
}

func (s *Scheduler) MarkLeased(taskID string) error {
	return s.transition(taskID, TaskStateLeased)
}

func (s *Scheduler) MarkRunning(taskID string) error {
	return s.transition(taskID, TaskStateRunning)
}

func (s *Scheduler) MarkCompleted(taskID string) error {
	return s.transition(taskID, TaskStateCompleted)
}

func (s *Scheduler) MarkBlocked(taskID string) error {
	return s.transition(taskID, TaskStateBlocked)
}

func (s *Scheduler) MarkCanceled(taskID string) error {
	return s.transition(taskID, TaskStateCanceled)
}

func (s *Scheduler) MarkAwaitingApproval(taskID string) error {
	return s.transition(taskID, TaskStateAwaitingApproval)
}

func (s *Scheduler) MarkApprovalResumed(taskID string) error {
	if s.state[taskID] != TaskStateAwaitingApproval {
		return fmt.Errorf("task %s is not awaiting approval", taskID)
	}
	return s.transition(taskID, TaskStateQueued)
}

func (s *Scheduler) MarkFailed(taskID, reason string, retryable bool, maxAttempts int) error {
	from, ok := s.state[taskID]
	if !ok {
		return fmt.Errorf("unknown task: %s", taskID)
	}
	target := MapFailureReasonToState(reason)
	if target == TaskStateBlocked {
		return s.transition(taskID, TaskStateBlocked)
	}
	if err := ValidateTransition(from, TaskStateFailed); err != nil {
		return err
	}
	s.state[taskID] = TaskStateFailed
	if !retryable {
		return nil
	}
	next, nextAttempt, err := RetryFromFailed(s.attempts[taskID], maxAttempts, true)
	if err != nil {
		return nil
	}
	if err := ValidateTransition(TaskStateFailed, next); err != nil {
		return err
	}
	s.state[taskID] = next
	s.attempts[taskID] = nextAttempt
	s.retryAfter[taskID] = s.now().Add(s.backoffForAttempt(nextAttempt - 1))
	return nil
}

func (s *Scheduler) StopAll() {
	for id, st := range s.state {
		switch st {
		case TaskStateCompleted, TaskStateCanceled:
			continue
		default:
			s.state[id] = TaskStateCanceled
		}
	}
}

func (s *Scheduler) State(taskID string) (TaskState, bool) {
	st, ok := s.state[taskID]
	return st, ok
}

func (s *Scheduler) Task(taskID string) (TaskSpec, bool) {
	task, ok := s.tasks[taskID]
	return task, ok
}

func (s *Scheduler) Attempt(taskID string) (int, bool) {
	attempt, ok := s.attempts[taskID]
	return attempt, ok
}

func (s *Scheduler) SetAttempt(taskID string, attempt int) error {
	if attempt <= 0 {
		return fmt.Errorf("attempt must be > 0")
	}
	if _, ok := s.tasks[taskID]; !ok {
		return fmt.Errorf("unknown task: %s", taskID)
	}
	s.attempts[taskID] = attempt
	return nil
}

func (s *Scheduler) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Scheduler) SetRetryBackoff(backoff []time.Duration) {
	if len(backoff) == 0 {
		return
	}
	s.backoff = append([]time.Duration(nil), backoff...)
}

func (s *Scheduler) AddTask(task TaskSpec) error {
	if err := ValidateTaskSpec(task); err != nil {
		return err
	}
	if _, exists := s.tasks[task.TaskID]; exists {
		return fmt.Errorf("task %s already exists", task.TaskID)
	}
	for _, dep := range task.DependsOn {
		if _, ok := s.tasks[dep]; !ok {
			return fmt.Errorf("task %s depends on unknown task %s", task.TaskID, dep)
		}
	}
	tasks := make(map[string]TaskSpec, len(s.tasks)+1)
	for id, existing := range s.tasks {
		tasks[id] = existing
	}
	tasks[task.TaskID] = task
	if err := validateAcyclic(tasks); err != nil {
		return err
	}
	s.tasks[task.TaskID] = task
	s.state[task.TaskID] = TaskStateQueued
	s.attempts[task.TaskID] = 1
	delete(s.retryAfter, task.TaskID)
	return nil
}

func (s *Scheduler) IsDone() bool {
	for _, st := range s.state {
		switch st {
		case TaskStateCompleted, TaskStateBlocked, TaskStateCanceled, TaskStateFailed:
			continue
		default:
			return false
		}
	}
	return true
}

func (s *Scheduler) Summary() map[TaskState]int {
	out := map[TaskState]int{}
	for _, st := range s.state {
		out[st]++
	}
	return out
}

func (s *Scheduler) ForceState(taskID string, state TaskState) error {
	if err := ValidateTaskState(state); err != nil {
		return err
	}
	if _, ok := s.tasks[taskID]; !ok {
		return fmt.Errorf("unknown task: %s", taskID)
	}
	s.state[taskID] = state
	if state != TaskStateQueued {
		delete(s.retryAfter, taskID)
	}
	return nil
}

func (s *Scheduler) activeSlots() int {
	active := 0
	for _, st := range s.state {
		if st == TaskStateLeased || st == TaskStateRunning || st == TaskStateAwaitingApproval {
			active++
		}
	}
	return active
}

func (s *Scheduler) dependenciesMet(task TaskSpec) bool {
	for _, dep := range task.DependsOn {
		if s.state[dep] != TaskStateCompleted {
			return false
		}
	}
	return true
}

func (s *Scheduler) transition(taskID string, to TaskState) error {
	from, ok := s.state[taskID]
	if !ok {
		return fmt.Errorf("unknown task: %s", taskID)
	}
	if err := ValidateTransition(from, to); err != nil {
		return err
	}
	s.state[taskID] = to
	if to != TaskStateQueued {
		delete(s.retryAfter, taskID)
	}
	return nil
}

func (s *Scheduler) backoffForAttempt(retryNumber int) time.Duration {
	if retryNumber <= 0 {
		return 0
	}
	idx := retryNumber - 1
	if idx < 0 {
		return 0
	}
	if idx >= len(s.backoff) {
		return s.backoff[len(s.backoff)-1]
	}
	return s.backoff[idx]
}

func validateAcyclic(tasks map[string]TaskSpec) error {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var dfs func(string) error
	dfs = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("cycle detected at task %s", id)
		}
		visiting[id] = true
		for _, dep := range tasks[id].DependsOn {
			if err := dfs(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range tasks {
		if err := dfs(id); err != nil {
			return err
		}
	}
	return nil
}
