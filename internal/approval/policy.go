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
		return "Approve dangerous executions"
	case FullAccess:
		return "Approve everything"
	default:
		return "Approve every execution"
	}
}

// Risk is model-authored advisory information about the complete invocation,
// including scripts it calls. Unknown or incomplete assessments require review.
func (m Mode) RequiresApproval(r Request) bool {
	switch m.Normalized() {
	case FullAccess:
		return false
	case DangerousOnly:
		return r.Risk != "low" || strings.TrimSpace(r.Summary) == "" || strings.TrimSpace(r.Target) == "" || strings.TrimSpace(r.Impact) == ""
	default:
		return true
	}
}
