package orchestrator

import "fmt"

type StateMutationDomain string

const (
	MutationDomainTaskLifecycle    StateMutationDomain = "task_lifecycle"
	MutationDomainApprovalStatus   StateMutationDomain = "approval_status"
	MutationDomainReplanGraph      StateMutationDomain = "replan_graph"
	MutationDomainRunProjection    StateMutationDomain = "run_projection"
	MutationDomainTaskProjection   StateMutationDomain = "task_projection"
	MutationDomainWorkerProjection StateMutationDomain = "worker_projection"
)

type EventMutationOwner string

const (
	EventMutationOwnerCoordinator EventMutationOwner = "coordinator"
	EventMutationOwnerBroker      EventMutationOwner = "approval_broker"
	EventMutationOwnerEventCache  EventMutationOwner = "event_cache"
	EventMutationOwnerRuntime     EventMutationOwner = "runtime"
	EventMutationOwnerOperator    EventMutationOwner = "operator"
)

type EventMutationRule struct {
	Owner   EventMutationOwner
	Domains []StateMutationDomain
}

var eventMutationTable = map[string]EventMutationRule{
	EventTypeRunStarted: {
		Owner:   EventMutationOwnerCoordinator,
		Domains: []StateMutationDomain{MutationDomainRunProjection},
	},
	EventTypeRunStopped: {
		Owner:   EventMutationOwnerCoordinator,
		Domains: []StateMutationDomain{MutationDomainRunProjection},
	},
	EventTypeRunCompleted: {
		Owner:   EventMutationOwnerCoordinator,
		Domains: []StateMutationDomain{MutationDomainRunProjection},
	},
	EventTypeTaskLeased: {
		Owner:   EventMutationOwnerCoordinator,
		Domains: []StateMutationDomain{MutationDomainTaskProjection},
	},
	EventTypeTaskStarted: {
		Owner: EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{
			MutationDomainTaskProjection,
			MutationDomainWorkerProjection,
		},
	},
	EventTypeTaskProgress: {
		Owner: EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{
			MutationDomainTaskProjection,
			MutationDomainWorkerProjection,
		},
	},
	EventTypeTaskArtifact: {
		Owner: EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{
			MutationDomainTaskProjection,
			MutationDomainWorkerProjection,
		},
	},
	EventTypeTaskFinding: {
		Owner: EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{
			MutationDomainTaskProjection,
			MutationDomainWorkerProjection,
		},
	},
	EventTypeTaskFailed: {
		Owner: EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{
			MutationDomainTaskProjection,
			MutationDomainReplanGraph,
		},
	},
	EventTypeTaskCompleted: {
		Owner: EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{
			MutationDomainTaskProjection,
			MutationDomainWorkerProjection,
		},
	},
	EventTypeWorkerStarted: {
		Owner:   EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{MutationDomainWorkerProjection},
	},
	EventTypeWorkerHeartbeat: {
		Owner:   EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{MutationDomainWorkerProjection},
	},
	EventTypeWorkerStopped: {
		Owner:   EventMutationOwnerRuntime,
		Domains: []StateMutationDomain{MutationDomainWorkerProjection},
	},
	EventTypeApprovalRequested: {
		Owner:   EventMutationOwnerBroker,
		Domains: []StateMutationDomain{MutationDomainApprovalStatus},
	},
	EventTypeApprovalGranted: {
		Owner: EventMutationOwnerBroker,
		Domains: []StateMutationDomain{
			MutationDomainApprovalStatus,
			MutationDomainReplanGraph,
		},
	},
	EventTypeApprovalDenied: {
		Owner: EventMutationOwnerBroker,
		Domains: []StateMutationDomain{
			MutationDomainApprovalStatus,
			MutationDomainReplanGraph,
		},
	},
	EventTypeApprovalExpired: {
		Owner: EventMutationOwnerBroker,
		Domains: []StateMutationDomain{
			MutationDomainApprovalStatus,
			MutationDomainReplanGraph,
		},
	},
	EventTypeWorkerStopRequested: {
		Owner: EventMutationOwnerOperator,
		Domains: []StateMutationDomain{
			MutationDomainTaskLifecycle,
		},
	},
	EventTypeOperatorInstruction: {
		Owner: EventMutationOwnerOperator,
		Domains: []StateMutationDomain{
			MutationDomainReplanGraph,
		},
	},
	EventTypeRunReportGenerated: {
		Owner:   EventMutationOwnerCoordinator,
		Domains: nil,
	},
	EventTypeRunWarning: {
		Owner:   EventMutationOwnerEventCache,
		Domains: nil,
	},
	EventTypeRunStateUpdated: {
		Owner:   EventMutationOwnerCoordinator,
		Domains: nil,
	},
	EventTypeRunReplanRequested: {
		Owner:   EventMutationOwnerCoordinator,
		Domains: nil,
	},
}

func EventMutatesDomain(eventType string, domain StateMutationDomain) bool {
	rule, ok := eventMutationTable[eventType]
	if !ok {
		return false
	}
	for _, allowed := range rule.Domains {
		if allowed == domain {
			return true
		}
	}
	return false
}

func RequireEventMutationDomain(eventType string, domain StateMutationDomain) error {
	if EventMutatesDomain(eventType, domain) {
		return nil
	}
	return fmt.Errorf("event %q is not authorized to mutate domain %q", eventType, domain)
}

func ValidateEventMutationTable() error {
	knownDomains := map[StateMutationDomain]struct{}{
		MutationDomainTaskLifecycle:    {},
		MutationDomainApprovalStatus:   {},
		MutationDomainReplanGraph:      {},
		MutationDomainRunProjection:    {},
		MutationDomainTaskProjection:   {},
		MutationDomainWorkerProjection: {},
	}
	for _, eventType := range CanonicalEventTypes() {
		rule, ok := eventMutationTable[eventType]
		if !ok {
			return fmt.Errorf("missing event mutation rule for %q", eventType)
		}
		seen := map[StateMutationDomain]struct{}{}
		for _, domain := range rule.Domains {
			if _, ok := knownDomains[domain]; !ok {
				return fmt.Errorf("event %q has unknown mutation domain %q", eventType, domain)
			}
			if _, dup := seen[domain]; dup {
				return fmt.Errorf("event %q duplicates mutation domain %q", eventType, domain)
			}
			seen[domain] = struct{}{}
		}
	}
	for eventType := range eventMutationTable {
		known := false
		for _, canonical := range CanonicalEventTypes() {
			if canonical == eventType {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("event mutation table contains unknown event %q", eventType)
		}
	}
	return nil
}
