package webapp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func (s *Server) start(current *run, continuation ...string) error {
	current.mu.Lock()
	if current.deleted {
		current.mu.Unlock()
		return fmt.Errorf("session has been deleted")
	}
	if current.started {
		current.mu.Unlock()
		return fmt.Errorf("assessment has already been started")
	}
	if strings.TrimSpace(current.client.BaseURL) == "" || strings.TrimSpace(current.client.Model) == "" {
		current.mu.Unlock()
		return fmt.Errorf("model endpoint and model are required; configure the web server first")
	}
	request := ""
	if len(continuation) > 0 {
		request = continuation[0]
	}
	previousState, previousResume, previousPostRunUsage, previousPostRunBudget := current.state, current.resume, current.postRunUsage, current.postRunBudget
	if request != "" {
		prepared, err := assessment.PrepareContinuation(current.state, request, s.config.Limits)
		if err != nil {
			current.mu.Unlock()
			return err
		}
		current.state = prepared
		current.resume = true
		current.postRunUsage = assessment.Usage{}
		current.postRunBudget = nil
	} else if current.state.Status == "completed" || current.state.Status == "completed_with_gaps" {
		current.mu.Unlock()
		return fmt.Errorf("assessment has already been finalized")
	}
	ctx, cancelCause := context.WithCancelCause(context.Background())
	cancel := func() { cancelCause(context.Canceled) }
	previousStatus, previousUpdatedAt, previousDone, previousBudget := current.status, current.updatedAt, current.done, current.budget
	limits := current.state.Limits
	if limits == (assessment.Limits{}) {
		limits = s.config.Limits
	}
	if limits == (assessment.Limits{}) {
		limits = assessment.DefaultLimits()
	}
	preparedState := current.state
	current.budget = assessment.NewModelBudget(limits.ModelCalls, current.state.Usage)
	current.started, current.status, current.runCtx, current.cancel, current.failCancel, current.done, current.updatedAt = true, "starting", ctx, cancel, cancelCause, make(chan struct{}), time.Now().UTC()
	current.mu.Unlock()
	if request != "" {
		if err := atomicWriteJSON(filepath.Join(current.root, "assessment.json"), preparedState); err != nil {
			cancel()
			current.mu.Lock()
			current.state, current.resume, current.postRunUsage, current.postRunBudget = previousState, previousResume, previousPostRunUsage, previousPostRunBudget
			current.started, current.status, current.runCtx, current.cancel, current.failCancel, current.done, current.updatedAt, current.budget = false, previousStatus, nil, nil, nil, previousDone, previousUpdatedAt, previousBudget
			current.mu.Unlock()
			return fmt.Errorf("save continued assessment: %w", err)
		}
	}
	if err := current.persist(); err != nil {
		cancel()
		current.mu.Lock()
		current.state, current.resume, current.postRunUsage, current.postRunBudget = previousState, previousResume, previousPostRunUsage, previousPostRunBudget
		current.started, current.status, current.runCtx, current.cancel, current.failCancel, current.done, current.updatedAt, current.budget = false, previousStatus, nil, nil, nil, previousDone, previousUpdatedAt, previousBudget
		current.mu.Unlock()
		if request != "" {
			_ = atomicWriteJSON(filepath.Join(current.root, "assessment.json"), previousState)
		}
		return fmt.Errorf("save assessment session: %w", err)
	}
	current.emit(assessment.Event{Kind: "assessment_started", Message: "Assessment accepted; the coordinator is preparing its first plan."})
	go s.runAssessment(ctx, current)
	return nil
}

func (s *Server) runAssessment(ctx context.Context, current *run) {
	current.mu.RLock()
	client := current.client
	budget := current.budget
	resume := current.resume
	initial := current.state
	project, id := current.customer, current.id
	current.mu.RUnlock()
	target := assessmentTarget(initial)
	if target == "" {
		target = legacyHostname(initial.Scope)
	}
	if initial.ProjectHistory == nil {
		initial.ProjectHistory = s.projectHistory(project, target, id)
	}
	initial.Target = target
	runner := assessment.Coordinator{
		LLM:            client,
		Budget:         budget,
		Frame:          s.config.Frame,
		Target:         target,
		ProjectHistory: initial.ProjectHistory,
		Limits:         s.config.Limits,
		Approach:       initial.Approach,
		Emit:           current.emit,
		Approver: func(task assessment.Task) approval.Approver {
			return &runApprover{run: current, taskID: task.ID}
		},
		AskUser: func(ctx context.Context, task assessment.Task, question string) (string, error) {
			return current.ask(ctx, task.ID, question)
		},
		PlanApproval: func(ctx context.Context, plan assessment.Decision) (assessment.PlanReview, error) {
			return current.reviewPlanWait(ctx, plan)
		},
		Conversation:    current.conversation,
		Snapshot:        current.snapshot,
		ContextSnapshot: current.setCoordinatorContext,
	}
	var state assessment.State
	var err error
	if resume && initial.Goal != "" {
		state, err = runner.RunState(ctx, current.root, initial)
	} else {
		state, err = runner.Run(ctx, current.root, current.goal, current.scope)
	}
	current.mu.Lock()
	if budget != nil {
		state.Usage = budget.Usage()
	}
	current.state = state
	if err != nil {
		current.status = state.Status
		if current.status == "" {
			current.status = "incomplete"
		}
	} else {
		current.status = state.Status
	}
	current.resume = true
	done := current.done
	current.runCtx, current.cancel, current.failCancel = nil, nil, nil
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	_ = current.persistOrStop()
	current.emit(assessment.Event{Kind: "assessment_finished", Message: current.status})
	close(done)
	current.mu.Lock()
	if current.done == done {
		current.started = false
	}
	current.mu.Unlock()
}

func (r *run) snapshot(state assessment.State) {
	r.mu.Lock()
	if r.budget != nil {
		state.Usage = r.budget.Usage()
	}
	r.state = state
	r.status = state.Status
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	_ = r.persistOrStop()
}

func (r *run) setCoordinatorContext(used, limit, providerInputTokens int) {
	r.mu.Lock()
	r.coordinatorContext = contextWindowView{UsedBytes: used, LimitBytes: limit, ProviderInputTokens: providerInputTokens}
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	_ = r.persistOrStop()
}

func (r *run) conversation() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]string, 0, len(r.messages))
	for _, message := range r.messages {
		values = append(values, message.Role+": "+message.Text+attachmentSummary(message.Attachments))
	}
	return values
}

func (r *run) emit(event assessment.Event) {
	r.mu.Lock()
	r.updateWorker(event)
	r.sequence++
	r.events = append(r.events, eventRecord{Sequence: r.sequence, At: time.Now().UTC(), Event: event})
	r.updatedAt = time.Now().UTC()
	if len(r.events) > 200 {
		r.events = r.events[len(r.events)-200:]
	}
	r.mu.Unlock()
	_ = r.persistOrStop()
}

func (r *run) persistOrStop() error {
	err := r.persist()
	if err == nil {
		return nil
	}
	r.mu.Lock()
	if r.persistErr == nil {
		r.persistErr = fmt.Errorf("save web session: %w", err)
	}
	cancel := r.failCancel
	r.mu.Unlock()
	if cancel != nil {
		cancel(fmt.Errorf("web session persistence failed: %w", err))
	}
	return err
}

func (r *run) stop() error {
	r.mu.RLock()
	deleted, cancel, started, status := r.deleted, r.cancel, r.started, r.status
	r.mu.RUnlock()
	if deleted {
		return fmt.Errorf("session has been deleted")
	}
	if !started || cancel == nil {
		return fmt.Errorf("assessment is not running")
	}
	if status != "running" && status != "starting" {
		return fmt.Errorf("assessment is already %s", status)
	}
	cancel()
	r.emit(assessment.Event{Kind: "assessment_stop_requested", Message: "Stop requested; waiting for workers to finalize."})
	return nil
}

func (r *run) pause() error {
	r.mu.RLock()
	deleted, cancel, started, status := r.deleted, r.failCancel, r.started, r.status
	r.mu.RUnlock()
	if deleted {
		return fmt.Errorf("session has been deleted")
	}
	if !started || cancel == nil {
		return fmt.Errorf("assessment is not running")
	}
	if status != "running" && status != "starting" {
		return fmt.Errorf("assessment is already %s", status)
	}
	cancel(assessment.ErrPaused)
	r.emit(assessment.Event{Kind: "assessment_pause_requested", Message: "Pause requested; stopping workers and saving their evidence."})
	return nil
}
