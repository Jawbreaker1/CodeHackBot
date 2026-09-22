package webapp

import (
	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"net/http"
	"time"
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
	// Existing requests remain explicit decisions; changing a mode never drains
	// or silently approves the pending queue.
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
	writeJSON(w, http.StatusOK, current.view())
}
