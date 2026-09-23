package webapp

// Durable web-session metadata lives beside each assessment's evidence. The
// assessment package remains the authority for assessment.json and report.md;
// this file stores only browser navigation state, the coordinator transcript,
// and the model selection needed to reopen a session.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

const sessionRecordVersion = 1

func (s *Server) clientForRecord(record sessionRecord) llmclient.Client {
	if record.ModelProfile != "" {
		if profile, ok := s.profile(record.ModelProfile); ok {
			return profile.client()
		}
		// A removed profile must not silently send a saved session to the
		// server's new default endpoint. The operator can select a new profile
		// in an idle draft or start a fresh session.
		return llmclient.Client{Model: record.Model}
	}
	client := s.config.LLM
	if strings.TrimSpace(record.Model) != "" {
		if len(s.config.Profiles) > 0 && record.Model != client.Model {
			// Old model-only sessions did not record their provider endpoint.
			// Do not guess that a different model belongs on today's default.
			return llmclient.Client{Model: record.Model}
		}
		client.Model = record.Model
	}
	return client
}

type sessionRecord struct {
	PermissionMode approval.Mode       `json:"permission_mode,omitempty"`
	Version        int                 `json:"version"`
	Kind           string              `json:"kind"`
	ID             string              `json:"id"`
	Customer       string              `json:"customer,omitempty"`
	Goal           string              `json:"goal,omitempty"`
	Scope          string              `json:"scope,omitempty"`
	Model          string              `json:"model,omitempty"`
	ModelProfile   string              `json:"model_profile,omitempty"`
	Status         string              `json:"status,omitempty"`
	Usage          *assessment.Usage   `json:"usage,omitempty"`
	AssessmentID   string              `json:"assessment_id,omitempty"`
	Messages       []intakeMessage     `json:"messages,omitempty"`
	Conversation   []llmclient.Message `json:"conversation,omitempty"`
	Proposal       *intake.Draft       `json:"proposal,omitempty"`
	Events         []eventRecord       `json:"events,omitempty"`
	Workers        []workerView        `json:"workers,omitempty"`
	Sequence       uint64              `json:"sequence,omitempty"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

func (s *Server) restoreSessions() error {
	if err := os.MkdirAll(s.config.SessionsRoot, 0700); err != nil {
		return fmt.Errorf("create sessions directory: %w", err)
	}
	entries, err := os.ReadDir(s.config.SessionsRoot)
	if err != nil {
		return fmt.Errorf("read sessions directory: %w", err)
	}
	var firstErr error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == "intake" {
			if err := s.restoreIntakes(filepath.Join(s.config.SessionsRoot, entry.Name())); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !validCustomerID(entry.Name()) {
			continue
		}
		customerRoot := filepath.Join(s.config.SessionsRoot, entry.Name())
		children, readErr := os.ReadDir(customerRoot)
		if readErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("read customer %s sessions: %w", entry.Name(), readErr)
			}
			continue
		}
		for _, child := range children {
			if !child.IsDir() || !validSessionID(child.Name()) {
				continue
			}
			if err := s.restoreRun(entry.Name(), filepath.Join(customerRoot, child.Name())); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (s *Server) restoreIntakes(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read intake sessions: %w", err)
	}
	var firstErr error
	for _, entry := range entries {
		if !entry.IsDir() || !validSessionID(entry.Name()) {
			continue
		}
		sessionRoot := filepath.Join(root, entry.Name())
		record, err := readSessionRecord(sessionRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("restore intake %s: %w", entry.Name(), err)
			}
			continue
		}
		if record.Kind != "intake" || record.ID != entry.Name() {
			continue
		}
		client := s.clientForRecord(record)
		conversation := intake.Conversation{}
		conversation.SetBehaviorContext(s.config.Frame.PromptText())
		conversation.RestoreMessages(record.Conversation)
		current := &intakeRun{permissionMode: record.PermissionMode.Normalized(), id: record.ID, root: sessionRoot, customer: record.Customer, client: client, profileID: record.ModelProfile, conversation: conversation, messages: append([]intakeMessage(nil), record.Messages...), proposal: cloneDraft(record.Proposal), assessmentID: record.AssessmentID, events: append([]eventRecord(nil), record.Events...), updatedAt: record.UpdatedAt}
		s.mu.Lock()
		s.intakes[current.id] = current
		s.mu.Unlock()
	}
	return firstErr
}

func (s *Server) restoreRun(customer, root string) error {
	state, err := assessment.LoadState(root)
	record, recordErr := readSessionRecord(root)
	if recordErr != nil && !os.IsNotExist(recordErr) {
		return fmt.Errorf("restore session metadata %s: %w", filepath.Base(root), recordErr)
	}
	if err != nil {
		if os.IsNotExist(err) {
			if recordErr != nil || record.Kind != "assessment" {
				// A cancelled draft may have created its directory before the
				// session record was written. It is not a resumable session.
				return nil
			}
			state = assessment.State{Version: 1, ID: record.ID, Goal: record.Goal, Scope: record.Scope, Model: record.Model, Status: "draft"}
		} else {
			return fmt.Errorf("restore assessment %s: %w", filepath.Base(root), err)
		}
	}
	if record.Usage != nil {
		state.Usage = newerUsage(state.Usage, *record.Usage)
	}
	id := filepath.Base(root)
	if recordErr == nil && record.ID != "" {
		id = record.ID
	}
	if state.ID != "" {
		id = state.ID
	}
	client := s.clientForRecord(record)
	if record.ModelProfile == "" && strings.TrimSpace(record.Model) == "" && strings.TrimSpace(state.Model) != "" {
		client.Model = state.Model
	}
	status := state.Status
	if status == "running" || status == "starting" {
		status = "interrupted"
		state.Error = "The web server restarted while this assessment was active; review the saved evidence before resuming."
	}
	if status == "" {
		status = "draft"
	}
	events := append([]eventRecord(nil), record.Events...)
	workers := make(map[string]workerView, len(record.Workers))
	for _, worker := range record.Workers {
		if worker.ID != "" {
			workers[worker.ID] = worker
		}
	}
	updated := record.UpdatedAt
	if updated.IsZero() {
		updated = state.FinishedAt
	}
	current := &run{permissionMode: record.PermissionMode.Normalized(), id: id, customer: customer, root: root, client: client, profileID: record.ModelProfile, goal: state.Goal, scope: state.Scope, status: status, state: state, resume: true, done: make(chan struct{}), approvals: make(map[string]*pendingApproval), questions: make(map[string]*pendingQuestion), sequence: record.Sequence, events: events, workers: workers, messages: append([]intakeMessage(nil), record.Messages...), updatedAt: updated}
	s.mu.Lock()
	s.runs[id] = current
	s.mu.Unlock()
	return nil
}

// Both files contain snapshots of the same monotonic meter. The browser
// metadata may be newer than assessment.json when the operator chats during a
// plan review; the assessment snapshot may be newer after worker execution.
func newerUsage(a, b assessment.Usage) assessment.Usage {
	return assessment.Usage{
		Calls:             max(a.Calls, b.Calls),
		FailedCalls:       max(a.FailedCalls, b.FailedCalls),
		ReportedTokens:    max(a.ReportedTokens, b.ReportedTokens),
		CallsWithoutUsage: max(a.CallsWithoutUsage, b.CallsWithoutUsage),
	}
}

func readSessionRecord(root string) (sessionRecord, error) {
	data, err := os.ReadFile(filepath.Join(root, "session.json"))
	if err != nil {
		return sessionRecord{}, err
	}
	var record sessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return sessionRecord{}, fmt.Errorf("parse session metadata: %w", err)
	}
	if record.Version != sessionRecordVersion || record.ID == "" || (record.Kind != "intake" && record.Kind != "assessment") {
		return sessionRecord{}, fmt.Errorf("unsupported session metadata")
	}
	return record, nil
}

func atomicWriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func (r *intakeRun) persist() error {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.RLock()
	if r.deleted {
		r.mu.RUnlock()
		return fmt.Errorf("session has been deleted")
	}
	r.conversationMu.RLock()
	conversation := r.conversation.Messages()
	r.conversationMu.RUnlock()
	record := sessionRecord{PermissionMode: r.permissionMode.Normalized(), Version: sessionRecordVersion, Kind: "intake", ID: r.id, Customer: r.customer, Model: r.client.Model, ModelProfile: r.profileID, Messages: append([]intakeMessage(nil), r.messages...), Conversation: conversation, Proposal: cloneDraft(r.proposal), AssessmentID: r.assessmentID, Events: append([]eventRecord(nil), r.events...), UpdatedAt: r.updatedAt}
	root := r.root
	r.mu.RUnlock()
	return atomicWriteJSON(filepath.Join(root, "session.json"), record)
}

func (r *run) persist() error {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.RLock()
	if r.deleted {
		r.mu.RUnlock()
		return fmt.Errorf("session has been deleted")
	}
	workers := make([]workerView, 0, len(r.workers))
	for _, worker := range r.workers {
		workers = append(workers, worker)
	}
	usage := r.state.Usage
	if r.budget != nil {
		usage = r.budget.Usage()
	}
	record := sessionRecord{PermissionMode: r.permissionMode.Normalized(), Version: sessionRecordVersion, Kind: "assessment", ID: r.id, Customer: r.customer, Goal: r.goal, Scope: r.scope, Model: r.client.Model, ModelProfile: r.profileID, Status: r.status, Usage: &usage, Messages: append([]intakeMessage(nil), r.messages...), Events: append([]eventRecord(nil), r.events...), Workers: workers, Sequence: r.sequence, UpdatedAt: r.updatedAt}
	root := r.root
	r.mu.RUnlock()
	return atomicWriteJSON(filepath.Join(root, "session.json"), record)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func validSessionID(id string) bool {
	if id == "" || len(id) > 180 || id == "." || id == ".." {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func compactTitle(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 72 {
		return value[:69] + "..."
	}
	return value
}
