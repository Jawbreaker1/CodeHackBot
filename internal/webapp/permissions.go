package webapp

import (
	"net/http"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

type permissionRequest struct {
	Mode        approval.Mode `json:"mode"`
	Acknowledge bool          `json:"acknowledge"`
}

func (a *runApprover) ApprovalMode() approval.Mode {
	a.run.mu.RLock()
	defer a.run.mu.RUnlock()
	return a.run.permissionMode.Normalized()
}

func readPermission(w http.ResponseWriter, r *http.Request) (approval.Mode, bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return "", false
	}
	var input permissionRequest
	if !decodeJSON(w, r, &input) {
		return "", false
	}
	if !input.Mode.Valid() {
		writeError(w, http.StatusBadRequest, "unknown approval mode")
		return "", false
	}
	if input.Mode != approval.EveryExecution && !input.Acknowledge {
		writeError(w, http.StatusBadRequest, "confirm the session approval policy before enabling automatic executions")
		return "", false
	}
	return input.Mode, true
}

func (s *Server) changeRunPermissions(w http.ResponseWriter, r *http.Request, current *run) {
	mode, ok := readPermission(w, r)
	if !ok {
		return
	}
	current.mu.Lock()
	previous := current.permissionMode
	current.permissionMode = mode
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.persist(); err != nil {
		current.mu.Lock()
		current.permissionMode = previous
		current.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	current.mu.Lock()
	if current.plan != nil && mode != approval.EveryExecution {
		pending := current.plan
		current.plan = nil
		ids := make([]string, 0, len(pending.plan.Tasks))
		for _, task := range pending.plan.Tasks {
			ids = append(ids, task.ID)
		}
		pending.result <- assessment.PlanReview{TaskIDs: ids}
	}
	for id, pending := range current.approvals {
		if mode.RequiresApproval(pending.request) {
			continue
		}
		delete(current.approvals, id)
		pending.result <- approval.DecisionApproveSession
	}
	current.mu.Unlock()
	current.writeView(w, "")
}

func (s *Server) changeIntakePermissions(w http.ResponseWriter, r *http.Request, current *intakeRun) {
	mode, ok := readPermission(w, r)
	if !ok {
		return
	}
	current.mu.Lock()
	if current.busy || current.assessmentID != "" || current.deleted {
		current.mu.Unlock()
		writeError(w, http.StatusConflict, "change permissions in the active assessment, or wait for this conversation turn to finish")
		return
	}
	previous := current.permissionMode
	current.permissionMode = mode
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.persist(); err != nil {
		current.mu.Lock()
		current.permissionMode = previous
		current.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if mode == approval.FullAccess {
		current.mu.Lock()
		if pending := current.pendingTool; pending != nil {
			current.pendingTool = nil
			pending.Result <- approval.DecisionApproveSession
		}
		current.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, current.view())
}
