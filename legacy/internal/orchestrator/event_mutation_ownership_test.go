package orchestrator

import "testing"

func TestValidateEventMutationTable(t *testing.T) {
	if err := ValidateEventMutationTable(); err != nil {
		t.Fatalf("ValidateEventMutationTable: %v", err)
	}
}

func TestEventMutatesDomainCoreExpectations(t *testing.T) {
	if !EventMutatesDomain(EventTypeTaskFailed, MutationDomainReplanGraph) {
		t.Fatalf("expected task_failed to mutate replan graph domain")
	}
	if !EventMutatesDomain(EventTypeWorkerStopRequested, MutationDomainTaskLifecycle) {
		t.Fatalf("expected worker_stop_requested to mutate task lifecycle domain")
	}
	if EventMutatesDomain(EventTypeRunWarning, MutationDomainTaskProjection) {
		t.Fatalf("did not expect run_warning to mutate task projection domain")
	}
}

func TestRequireEventMutationDomain(t *testing.T) {
	if err := RequireEventMutationDomain(EventTypeApprovalDenied, MutationDomainApprovalStatus); err != nil {
		t.Fatalf("expected approval_denied approval-status mutation: %v", err)
	}
	if err := RequireEventMutationDomain(EventTypeRunWarning, MutationDomainTaskLifecycle); err == nil {
		t.Fatalf("expected run_warning task-lifecycle mutation to be rejected")
	}
}

func TestRunEventCacheApplyEventRespectsMutationDomains(t *testing.T) {
	cache := newRunEventCache()
	cache.applyEvent(EventEnvelope{
		Type:     EventTypeRunWarning,
		TaskID:   "t-001",
		WorkerID: "worker-1",
	})
	if cache.runState != "" {
		t.Fatalf("run warning should not mutate run projection, got %q", cache.runState)
	}
	if _, ok := cache.taskState["t-001"]; ok {
		t.Fatalf("run warning should not mutate task projection")
	}
	if _, ok := cache.workerActive["worker-1"]; ok {
		t.Fatalf("run warning should not mutate worker activity projection")
	}

	cache.applyEvent(EventEnvelope{
		Type:     EventTypeTaskStarted,
		TaskID:   "t-001",
		WorkerID: "worker-1",
	})
	if got := cache.taskState["t-001"]; got != "running" {
		t.Fatalf("expected task projection running, got %q", got)
	}
}
