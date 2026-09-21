package webapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Lock ordering matches session persistence: persistMu, then mu. The server
// lock keeps the index and linked conversation consistent during removal.
func (s *Server) deleteIntake(current *intakeRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current.persistMu.Lock()
	defer current.persistMu.Unlock()
	current.mu.Lock()
	defer current.mu.Unlock()
	if current.deleted {
		return fmt.Errorf("session has already been deleted")
	}
	if current.busy || current.pendingTool != nil {
		return fmt.Errorf("wait for the coordinator to finish before deleting this session")
	}
	if current.assessmentID != "" {
		return fmt.Errorf("delete the assessment session instead of its intake conversation")
	}
	if err := s.removeSessionRoot(current.root); err != nil {
		return err
	}
	current.deleted = true
	delete(s.intakes, current.id)
	return nil
}

func (s *Server) deleteRun(current *run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current.persistMu.Lock()
	defer current.persistMu.Unlock()
	current.mu.Lock()
	defer current.mu.Unlock()
	if current.deleted {
		return fmt.Errorf("session has already been deleted")
	}
	if current.started || current.chatBusy || len(current.approvals) > 0 || len(current.questions) > 0 {
		return fmt.Errorf("stop the assessment and wait for it to finish before deleting this session")
	}
	// The existing assessment_id link also covers sessions saved by older
	// versions; no second linkage field or migration is needed.
	var linked []*intakeRun
	for _, intake := range s.intakes {
		intake.mu.RLock()
		matches, busy := intake.assessmentID == current.id, intake.busy
		intake.mu.RUnlock()
		if matches {
			if busy {
				return fmt.Errorf("wait for the assessment to finish starting before deleting it")
			}
			linked = append(linked, intake)
		}
	}
	for _, intake := range linked {
		intake.persistMu.Lock()
		defer intake.persistMu.Unlock()
		intake.mu.Lock()
		defer intake.mu.Unlock()
	}
	if err := s.removeSessionRoot(current.root); err != nil {
		return err
	}
	current.deleted = true
	delete(s.runs, current.id)
	for _, intake := range linked {
		if err := s.removeSessionRoot(intake.root); err != nil {
			return fmt.Errorf("assessment removed, but its original conversation could not be removed: %w", err)
		}
		intake.deleted = true
		delete(s.intakes, intake.id)
	}
	return nil
}

func (s *Server) removeSessionRoot(root string) error {
	base, err := filepath.Abs(s.config.SessionsRoot)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(base, target)
	if err != nil || !filepath.IsLocal(relative) || len(strings.Split(relative, string(filepath.Separator))) != 2 {
		return fmt.Errorf("refusing to remove a path outside a session directory")
	}
	// RemoveAll does not follow the final symlink, but an intermediate customer
	// directory must not redirect deletion outside the configured session store.
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return err
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return err
	}
	if filepath.Dir(parent) != resolvedBase {
		return fmt.Errorf("refusing to remove a session through a redirected parent directory")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove session data: %w", err)
	}
	return nil
}
