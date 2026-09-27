package approval

import "strings"

// Mode is an operator-selected session policy, never a model decision.
type Mode string

// ModeProvider lets the shared worker expose the current runtime policy in its
// next model request without granting the model control over that policy.
type ModeProvider interface{ ApprovalMode() Mode }

const (
	EveryExecution Mode = "per_action"
	DangerousOnly  Mode = "dangerous_only"
	FullAccess     Mode = "full_access"
)

func (m Mode) Valid() bool { return m == EveryExecution || m == DangerousOnly || m == FullAccess }
func (m Mode) Normalized() Mode {
	if !m.Valid() {
		return EveryExecution
	}
	return m
}
func (m Mode) Label() string {
	switch m.Normalized() {
	case DangerousOnly:
		return "Review commands and risky actions"
	case FullAccess:
		return "Approve everything"
	default:
		return "Approve every execution"
	}
}

// A model-authored risk label is advisory. Arbitrary commands are opaque and
// require review in dangerous-only mode, even when labeled low. Only the fixed
// observation path can mark a validated read-only operation for auto-approval.
func (m Mode) RequiresApproval(r Request) bool {
	switch m.Normalized() {
	case FullAccess:
		return false
	case DangerousOnly:
		return !r.trustedReadOnlyObservation || r.Risk != "low" || strings.TrimSpace(r.Summary) == "" || strings.TrimSpace(r.Target) == "" || strings.TrimSpace(r.Impact) == ""
	default:
		return true
	}
}
