package webapp

import (
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"path/filepath"
	"time"
)

// workerView projects runtime observations. It never plans or executes work.
// Keeping it separate from the bounded event feed lets a newly opened browser
// see a worker's full current state even after earlier events have rolled off.
type workerView struct {
	ExecutionLog        string                    `json:"execution_log,omitempty"`
	ExpectedArtifacts   []string                  `json:"expected_artifacts,omitempty"`
	ID                  string                    `json:"id"`
	Goal                string                    `json:"goal"`
	DoneWhen            string                    `json:"done_when"`
	DependsOn           []string                  `json:"depends_on"`
	Phase               string                    `json:"phase"`
	Detail              string                    `json:"detail"`
	Step                int                       `json:"step"`
	ActiveStep          string                    `json:"active_step"`
	PlanSteps           []string                  `json:"plan_steps"`
	Action              string                    `json:"action"`
	Rationale           string                    `json:"rationale,omitempty"`
	ExitStatus          string                    `json:"exit_status"`
	EvidenceCount       int                       `json:"evidence_count"`
	Evidence            []assessment.EvidenceView `json:"evidence"`
	RemainingBudget     string                    `json:"remaining_budget"`
	ContextUsage        string                    `json:"context_usage"`
	ContextUsedBytes    int                       `json:"context_used_bytes,omitempty"`
	ContextLimitBytes   int                       `json:"context_limit_bytes,omitempty"`
	ContextUsagePercent int                       `json:"context_usage_percent,omitempty"`
	ModelCalls          int                       `json:"model_calls"`
	UpdatedAt           time.Time                 `json:"updated_at"`
}

// Called under run.mu. Published slices are replaced, never modified in place.
func (r *run) updateWorker(e assessment.Event) {
	if e.TaskID == "" {
		return
	}
	if r.workers == nil {
		r.workers = make(map[string]workerView)
	}
	w := r.workers[e.TaskID]
	if e.ExecutionLog != "" {
		w.ExecutionLog = e.ExecutionLog
		w.ExpectedArtifacts = nil
		workspace := filepath.Join(r.root, "tasks", e.TaskID, "work")
		for _, ref := range e.ExpectedArtifacts {
			if !filepath.IsAbs(ref) {
				ref = filepath.Join(workspace, ref)
			}
			if pathWithin(workspace, ref) {
				w.ExpectedArtifacts = append(w.ExpectedArtifacts, ref)
			}
		}
	}
	w.ID, w.Phase, w.UpdatedAt = e.TaskID, e.Kind, time.Now().UTC()
	if e.Goal != "" {
		w.Goal = e.Goal
	}
	if e.DoneWhen != "" {
		w.DoneWhen = e.DoneWhen
	}
	if e.DependsOn != nil {
		w.DependsOn = append([]string(nil), e.DependsOn...)
	}
	if e.Message != "" {
		w.Detail = e.Message
	}
	if e.Step > 0 {
		w.Step = e.Step
	}
	if e.ActiveStep != "" {
		w.ActiveStep = e.ActiveStep
	}
	if e.PlanSteps != nil {
		w.PlanSteps = append([]string(nil), e.PlanSteps...)
	}
	if e.Action != "" {
		w.Action = e.Action
	}
	if e.Rationale != "" {
		w.Rationale = e.Rationale
	}
	if e.ExitStatus != "" {
		w.ExitStatus = e.ExitStatus
	}
	if e.RemainingBudget != "" {
		w.RemainingBudget = e.RemainingBudget
	}
	if e.ContextUsage != "" && e.ContextUsage != "(unset)" {
		w.ContextUsage = e.ContextUsage
	}
	if e.ContextUsedBytes > 0 {
		w.ContextUsedBytes = e.ContextUsedBytes
	}
	if e.ContextLimitBytes > 0 {
		w.ContextLimitBytes = e.ContextLimitBytes
	}
	if e.ContextUsagePercent > 0 {
		w.ContextUsagePercent = e.ContextUsagePercent
	}
	w.EvidenceCount = max(w.EvidenceCount, e.EvidenceCount)
	w.ModelCalls = max(w.ModelCalls, e.ModelCalls)
	if e.Evidence != nil {
		w.Evidence = append(append([]assessment.EvidenceView(nil), w.Evidence...), *e.Evidence)
	}
	r.workers[e.TaskID] = w
}
