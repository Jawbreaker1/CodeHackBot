package orchestrator

import (
	"fmt"
	"strings"
	"time"
)

type PermissionMode string

const (
	PermissionReadonly PermissionMode = "readonly"
	PermissionDefault  PermissionMode = "default"
	PermissionAll      PermissionMode = "all"
)

type RiskTier string

const (
	RiskReconReadonly     RiskTier = "recon_readonly"
	RiskActiveProbe       RiskTier = "active_probe"
	RiskExploitControlled RiskTier = "exploit_controlled"
	RiskPrivEsc           RiskTier = "priv_esc"
	RiskDisruptive        RiskTier = "disruptive"
)

type ApprovalDecision string

const (
	ApprovalAllow        ApprovalDecision = "allow"
	ApprovalNeedsRequest ApprovalDecision = "request"
	ApprovalDeny         ApprovalDecision = "deny"
)

type ApprovalScope string

const (
	ApprovalScopeOnce    ApprovalScope = "once"
	ApprovalScopeTask    ApprovalScope = "task"
	ApprovalScopeSession ApprovalScope = "session"
)

type ApprovalStatus string

const (
	ApprovalStatusPending ApprovalStatus = "pending"
	ApprovalStatusGranted ApprovalStatus = "granted"
	ApprovalStatusDenied  ApprovalStatus = "denied"
	ApprovalStatusExpired ApprovalStatus = "expired"
)

type ApprovalRequest struct {
	ID          string
	RunID       string
	TaskID      string
	RiskTier    RiskTier
	Status      ApprovalStatus
	Reason      string
	RequestedAt time.Time
	ExpiresAt   time.Time
	Actor       string
	Note        string
}

type ApprovalGrant struct {
	ID        string
	RunID     string
	TaskID    string
	Scope     ApprovalScope
	RiskTier  RiskTier
	Actor     string
	Reason    string
	ExpiresAt time.Time
	UsesLeft  int
}

type ApprovalBroker struct {
	mode            PermissionMode
	disruptiveOptIn bool
	approvalTimeout time.Duration
	pendingByTask   map[string]*ApprovalRequest
	pendingByID     map[string]*ApprovalRequest
	approvedQueue   []ApprovalRequest
	deniedQueue     []ApprovalRequest
	expiredQueue    []ApprovalRequest
	grants          []ApprovalGrant
}

func NewApprovalBroker(mode PermissionMode, disruptiveOptIn bool, approvalTimeout time.Duration) (*ApprovalBroker, error) {
	if approvalTimeout <= 0 {
		return nil, fmt.Errorf("approval timeout must be > 0")
	}
	switch mode {
	case PermissionReadonly, PermissionDefault, PermissionAll:
	default:
		return nil, fmt.Errorf("invalid permission mode: %s", mode)
	}
	return &ApprovalBroker{
		mode:            mode,
		disruptiveOptIn: disruptiveOptIn,
		approvalTimeout: approvalTimeout,
		pendingByTask:   map[string]*ApprovalRequest{},
		pendingByID:     map[string]*ApprovalRequest{},
	}, nil
}

func ParseRiskTier(raw string) (RiskTier, error) {
	tier := RiskTier(strings.TrimSpace(strings.ToLower(raw)))
	switch tier {
	case RiskReconReadonly, RiskActiveProbe, RiskExploitControlled, RiskPrivEsc, RiskDisruptive:
		return tier, nil
	default:
		return "", fmt.Errorf("invalid risk tier: %s", raw)
	}
}

func (b *ApprovalBroker) EvaluateTask(task TaskSpec, now time.Time) (ApprovalDecision, string, RiskTier, error) {
	tier, err := ParseRiskTier(task.RiskLevel)
	if err != nil {
		return "", "", "", err
	}
	base := b.baseDecision(tier)
	if base == ApprovalAllow {
		return ApprovalAllow, "allowed by policy", tier, nil
	}
	if base == ApprovalDeny {
		return ApprovalDeny, "denied by policy", tier, nil
	}
	if b.consumeGrant(task.TaskID, tier, now) {
		return ApprovalAllow, "approved by scope grant", tier, nil
	}
	return ApprovalNeedsRequest, "requires approval", tier, nil
}

func (b *ApprovalBroker) EnsureRequest(runID string, task TaskSpec, tier RiskTier, reason string, now time.Time) ApprovalRequest {
	if existing, ok := b.pendingByTask[task.TaskID]; ok && existing.Status == ApprovalStatusPending {
		return *existing
	}
	req := &ApprovalRequest{
		ID:          "apr-" + NewEventID(),
		RunID:       runID,
		TaskID:      task.TaskID,
		RiskTier:    tier,
		Status:      ApprovalStatusPending,
		Reason:      reason,
		RequestedAt: now,
		ExpiresAt:   now.Add(b.approvalTimeout),
	}
	b.pendingByTask[task.TaskID] = req
	b.pendingByID[req.ID] = req
	return *req
}

func (b *ApprovalBroker) Approve(id string, scope ApprovalScope, actor, reason string, now time.Time, expiresIn time.Duration) error {
	req, ok := b.pendingByID[id]
	if !ok {
		return fmt.Errorf("approval request not found: %s", id)
	}
	if req.Status != ApprovalStatusPending {
		return fmt.Errorf("approval request not pending: %s", id)
	}
	req.Status = ApprovalStatusGranted
	req.Actor = actor
	req.Note = reason
	b.approvedQueue = append(b.approvedQueue, *req)

	grant := ApprovalGrant{
		ID:       "agr-" + NewEventID(),
		RunID:    req.RunID,
		TaskID:   req.TaskID,
		Scope:    scope,
		RiskTier: req.RiskTier,
		Actor:    actor,
		Reason:   reason,
	}
	if expiresIn > 0 {
		grant.ExpiresAt = now.Add(expiresIn)
	}
	switch scope {
	case ApprovalScopeOnce:
		grant.UsesLeft = 1
	case ApprovalScopeTask, ApprovalScopeSession:
		grant.UsesLeft = 0
	default:
		return fmt.Errorf("invalid approval scope: %s", scope)
	}
	b.grants = append(b.grants, grant)
	return nil
}

func (b *ApprovalBroker) Deny(id, actor, reason string) error {
	req, ok := b.pendingByID[id]
	if !ok {
		return fmt.Errorf("approval request not found: %s", id)
	}
	if req.Status != ApprovalStatusPending {
		return fmt.Errorf("approval request not pending: %s", id)
	}
	req.Status = ApprovalStatusDenied
	req.Actor = actor
	req.Note = reason
	b.deniedQueue = append(b.deniedQueue, *req)
	return nil
}

func (b *ApprovalBroker) Expire(now time.Time) []ApprovalRequest {
	out := make([]ApprovalRequest, 0)
	for _, req := range b.pendingByTask {
		if req.Status != ApprovalStatusPending {
			continue
		}
		if now.Before(req.ExpiresAt) {
			continue
		}
		req.Status = ApprovalStatusExpired
		b.expiredQueue = append(b.expiredQueue, *req)
		out = append(out, *req)
	}
	return out
}

func (b *ApprovalBroker) DrainGranted() []ApprovalRequest {
	out := b.approvedQueue
	b.approvedQueue = nil
	return out
}

func (b *ApprovalBroker) DrainDenied() []ApprovalRequest {
	out := b.deniedQueue
	b.deniedQueue = nil
	return out
}

func (b *ApprovalBroker) DrainExpired() []ApprovalRequest {
	out := b.expiredQueue
	b.expiredQueue = nil
	return out
}

func (b *ApprovalBroker) Pending() []ApprovalRequest {
	out := make([]ApprovalRequest, 0, len(b.pendingByTask))
	for _, req := range b.pendingByTask {
		if req.Status == ApprovalStatusPending {
			out = append(out, *req)
		}
	}
	return out
}

func (b *ApprovalBroker) PendingForTask(taskID string) (ApprovalRequest, bool) {
	req, ok := b.pendingByTask[taskID]
	if !ok || req.Status != ApprovalStatusPending {
		return ApprovalRequest{}, false
	}
	return *req, true
}

func (b *ApprovalBroker) baseDecision(tier RiskTier) ApprovalDecision {
	switch b.mode {
	case PermissionReadonly:
		if tier == RiskReconReadonly {
			return ApprovalAllow
		}
		return ApprovalDeny
	case PermissionDefault:
		if tier == RiskReconReadonly {
			return ApprovalAllow
		}
		if tier == RiskDisruptive && !b.disruptiveOptIn {
			return ApprovalDeny
		}
		return ApprovalNeedsRequest
	case PermissionAll:
		switch tier {
		case RiskReconReadonly, RiskActiveProbe, RiskExploitControlled:
			return ApprovalAllow
		case RiskPrivEsc:
			return ApprovalNeedsRequest
		case RiskDisruptive:
			if b.disruptiveOptIn {
				return ApprovalNeedsRequest
			}
			return ApprovalDeny
		default:
			return ApprovalNeedsRequest
		}
	default:
		return ApprovalDeny
	}
}

func (b *ApprovalBroker) consumeGrant(taskID string, tier RiskTier, now time.Time) bool {
	matched := false
	next := make([]ApprovalGrant, 0, len(b.grants))
	for _, grant := range b.grants {
		if !grant.ExpiresAt.IsZero() && now.After(grant.ExpiresAt) {
			continue
		}
		if grant.RiskTier != tier {
			next = append(next, grant)
			continue
		}
		taskMatch := grant.Scope == ApprovalScopeSession || (grant.Scope == ApprovalScopeTask && grant.TaskID == taskID) || (grant.Scope == ApprovalScopeOnce && grant.TaskID == taskID)
		if !taskMatch {
			next = append(next, grant)
			continue
		}
		matched = true
		if grant.Scope == ApprovalScopeOnce {
			if grant.UsesLeft > 1 {
				grant.UsesLeft--
				next = append(next, grant)
			}
			// UsesLeft==1 consumes grant.
			continue
		}
		next = append(next, grant)
	}
	b.grants = next
	return matched
}
