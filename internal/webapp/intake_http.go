package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
)

func (s *Server) intakeRoute(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/intake" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		current, err := s.newIntake()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, current.view())
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/intake/"), "/"), "/")
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodDelete {
		current := s.getIntake(parts[0])
		if current == nil {
			writeError(w, http.StatusNotFound, "intake session not found")
			return
		}
		if err := s.deleteIntake(current); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodGet {
		current := s.getIntake(parts[0])
		if current == nil {
			writeError(w, http.StatusNotFound, "intake session not found")
			return
		}
		writeJSON(w, http.StatusOK, current.view())
		return
	}
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	current := s.getIntake(parts[0])
	if current == nil {
		writeError(w, http.StatusNotFound, "intake session not found")
		return
	}
	if len(parts) == 3 && parts[1] == "attachments" {
		current.mu.RLock()
		root, messages := current.root, append([]intakeMessage(nil), current.messages...)
		current.mu.RUnlock()
		s.serveAttachment(w, r, root, messages, parts[2])
		return
	}
	if len(parts) == 3 {
		if parts[1] != "approvals" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var input approvalRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := current.approveTool(parts[2], input.Decision); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, current.view())
		return
	}
	switch parts[1] {
	case "permissions":
		s.changeIntakePermissions(w, r, current)
	case "model":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input modelRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		var err error
		if input.Profile != "" {
			err = s.changeIntakeProfile(current, input.Profile)
		} else {
			err = s.changeIntakeModel(current, input.Model)
		}
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, current.view())
	case "approvals":
		// Keep the route shape explicit: the pending observation ID belongs in
		// the path, just like assessment action approvals.
		http.NotFound(w, r)
	case "messages":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		text, refs, err := s.decodeMessageInput(w, r, current.root)
		if err != nil {
			removeAttachmentFiles(current.root, refs)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.intakeMessage(r.Context(), current, text, refs); err != nil {
			removeAttachmentFiles(current.root, refs)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, current.view())
	case "start":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input intakeStartRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		currentView, err := s.startIntake(current, input.Customer, input.ApproachID)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, currentView)
	case "customer":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input intakeCustomerRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.assignIntakeCustomer(current, input.Customer); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, current.view())
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) newIntake() (*intakeRun, error) {
	id := fmt.Sprintf("intake-%s-%06d", time.Now().UTC().Format("20060102-150405.000000000"), s.seq.Add(1))
	root := filepath.Join(s.config.SessionsRoot, "intake", id)
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create intake session directory: %w", err)
	}
	conversation := intake.Conversation{}
	conversation.SetBehaviorContext(s.config.Frame.PromptText())
	current := &intakeRun{id: id, root: root, client: s.config.LLM, profileID: s.config.DefaultProfile, conversation: conversation, updatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.intakes[current.id] = current
	s.mu.Unlock()
	if err := current.persist(); err != nil {
		return nil, err
	}
	return current, nil
}

func (s *Server) getIntake(id string) *intakeRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.intakes[id]
}

func (s *Server) intakeMessage(ctx context.Context, current *intakeRun, text string, refs []attachmentRef) error {
	text = strings.TrimSpace(text)
	if text == "" && len(refs) > 0 {
		text = "Please inspect the attached artifact(s)."
	}
	if text == "" {
		return fmt.Errorf("message is required")
	}
	attachments, err := loadLLMAttachments(current.root, refs)
	if err != nil {
		return err
	}
	current.mu.Lock()
	if current.deleted {
		current.mu.Unlock()
		return fmt.Errorf("session has been deleted")
	}
	if current.assessmentID != "" {
		current.mu.Unlock()
		return fmt.Errorf("this conversation already started an assessment")
	}
	if current.busy {
		current.mu.Unlock()
		return fmt.Errorf("the coordinator is still answering the previous message")
	}
	current.busy = true
	current.messages = append(current.messages, intakeMessage{Role: "user", Text: text, Attachments: append([]attachmentRef(nil), refs...), At: time.Now().UTC()})
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.persist(); err != nil {
		current.mu.Lock()
		current.removeLastMessage("user", text)
		current.busy = false
		current.mu.Unlock()
		return fmt.Errorf("save intake session: %w", err)
	}
	defer func() {
		current.mu.Lock()
		current.busy = false
		current.mu.Unlock()
	}()
	current.conversation.Inspection = &intake.Inspection{
		Workspace:   s.config.RepoRoot,
		EvidenceDir: filepath.Join(s.config.SessionsRoot, "intake", current.id),
		Connected:   s.config.Frame.Parameters["research_mode"] != "air_gapped" && s.config.Frame.Parameters["research_mode"] != "offline",
		Policy:      "Use local metadata and public DNS/page observations only for the operator's requested question. Keep them minimal; do not access credentials, scan targets, or mutate files. Broader testing belongs to assessment workers.",
		Approver:    &intakeToolApprover{run: current},
		Emit:        current.recordObservation,
	}
	current.conversation.SetBehaviorContext(s.config.Frame.PromptText())
	current.conversationMu.Lock()
	turn, err := current.conversation.Turn(ctx, current.client, text, attachments...)
	current.conversationMu.Unlock()
	if err != nil {
		current.mu.Lock()
		current.removeLastMessage("user", text)
		current.updatedAt = time.Now().UTC()
		current.mu.Unlock()
		_ = current.persist()
		return err
	}
	current.mu.Lock()
	current.messages = append(current.messages, intakeMessage{Role: "assistant", Text: turn.Reply, At: time.Now().UTC()})
	current.proposal = cloneDraft(turn.Proposal)
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	if persistErr := current.persist(); persistErr != nil {
		return persistErr
	}
	return nil
}

func (s *Server) startIntake(current *intakeRun, customer, approachID string) (assessmentView, error) {
	customer = strings.TrimSpace(customer)
	current.mu.Lock()
	if customer == "" {
		customer = current.customer
	}
	if current.deleted || current.busy || current.assessmentID != "" || current.proposal == nil {
		current.mu.Unlock()
		return assessmentView{}, fmt.Errorf("assessment requires an idle, undeleted conversation with a proposal")
	}
	if !validCustomerID(customer) {
		current.mu.Unlock()
		return assessmentView{}, fmt.Errorf("customer must contain only letters, numbers, hyphens, or underscores")
	}
	if strings.TrimSpace(current.client.BaseURL) == "" || strings.TrimSpace(current.client.Model) == "" {
		current.mu.Unlock()
		return assessmentView{}, fmt.Errorf("model endpoint and model are required; configure the web server first")
	}
	proposal := cloneDraft(current.proposal)
	var selectedApproach *assessment.Approach
	if len(proposal.Approaches) > 0 {
		for i := range proposal.Approaches {
			if proposal.Approaches[i].ID == approachID {
				choice := proposal.Approaches[i]
				selectedApproach = &choice
				break
			}
		}
		if selectedApproach == nil {
			current.mu.Unlock()
			return assessmentView{}, fmt.Errorf("choose an investigation approach before starting")
		}
	} else if approachID != "" {
		current.mu.Unlock()
		return assessmentView{}, fmt.Errorf("the selected investigation approach is unavailable")
	}
	client := current.client
	current.customer = customer
	current.busy = true
	current.mu.Unlock()
	defer func() {
		current.mu.Lock()
		current.busy = false
		current.mu.Unlock()
	}()
	created, err := s.newRun(customer, proposal.Goal, proposal.Scope)
	if err != nil {
		return assessmentView{}, err
	}
	current.mu.Lock()
	current.assessmentID = created.id
	intakeMessages := append([]intakeMessage(nil), current.messages...)
	current.mu.Unlock()
	intakeMessages, err = s.cloneMessagesWithAttachments(intakeMessages, current.root, created.root)
	if err != nil {
		return assessmentView{}, err
	}
	created.mu.Lock()
	created.state.Approach = selectedApproach
	created.state.Target = canonicalTarget(proposal.Target)
	if created.state.Target == "" {
		created.state.Target = legacyHostname(proposal.Scope)
	}
	created.permissionMode = current.permissionMode
	created.client = client
	created.profileID = current.profileID
	created.messages = intakeMessages
	created.updatedAt = time.Now().UTC()
	created.mu.Unlock()
	if err := current.persist(); err != nil {
		return assessmentView{}, err
	}
	if err := created.persist(); err != nil {
		return assessmentView{}, err
	}
	if err := s.start(created); err != nil {
		return assessmentView{}, err
	}
	return created.view(""), nil
}

func (s *Server) assignIntakeCustomer(current *intakeRun, customer string) error {
	customer = strings.TrimSpace(customer)
	if !validCustomerID(customer) {
		return fmt.Errorf("customer must contain only letters, numbers, hyphens, or underscores")
	}
	current.mu.Lock()
	if current.deleted {
		current.mu.Unlock()
		return fmt.Errorf("session has been deleted")
	}
	if current.busy {
		current.mu.Unlock()
		return fmt.Errorf("wait for the conversation to finish before moving it")
	}
	if current.assessmentID != "" {
		current.mu.Unlock()
		return fmt.Errorf("started assessments cannot be moved; move the assessment session instead")
	}
	current.customer = customer
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	return current.persist()
}

func (r *intakeRun) view() intakeView {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status := "conversation"
	if r.busy {
		status = "thinking"
	} else if r.proposal != nil {
		status = "ready"
	}
	if r.assessmentID != "" {
		status = "started"
	}
	var pending *intakeApprovalView
	if r.pendingTool != nil {
		status = "waiting_approval"
		pending = &intakeApprovalView{ID: r.pendingTool.ID, Tool: r.pendingTool.Tool, Summary: r.pendingTool.Summary, Impact: r.pendingTool.Impact, Target: r.pendingTool.Target}
	}
	title := "New session"
	if r.proposal != nil && strings.TrimSpace(r.proposal.Goal) != "" {
		title = strings.TrimSpace(r.proposal.Goal)
	} else if len(r.messages) > 0 && r.messages[0].Role == "user" {
		title = compactTitle(r.messages[0].Text)
	}
	return intakeView{PermissionMode: r.permissionMode.Normalized(), ID: r.id, Customer: r.customer, Title: title, Model: r.client.Model, ModelProfile: r.profileID, ModelConfigured: strings.TrimSpace(r.client.BaseURL) != "" && strings.TrimSpace(r.client.Model) != "", ModelBusy: r.busy, CanChangeModel: !r.busy && r.assessmentID == "" && (r.profileID == "" || len(r.messages) == 0), Status: status, Messages: messageViews(r.messages, "intake", r.id), Proposal: cloneDraft(r.proposal), PendingTool: pending, Events: append([]eventRecord(nil), r.events...), AssessmentID: r.assessmentID}
}

func (r *intakeRun) recordObservation(event assessment.Event) {
	r.mu.Lock()
	r.events = append(r.events, eventRecord{Sequence: uint64(len(r.events) + 1), At: time.Now().UTC(), Event: event})
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
}

type intakeToolApprover struct{ run *intakeRun }

func (a *intakeToolApprover) Approve(ctx context.Context, request approval.Request) (approval.Decision, error) {
	a.run.mu.RLock()
	mode := a.run.permissionMode
	a.run.mu.RUnlock()
	if !mode.RequiresApproval(request) {
		return approval.DecisionApproveSession, ctx.Err()
	}
	a.run.mu.Lock()
	if a.run.pendingTool != nil {
		a.run.mu.Unlock()
		return approval.DecisionDeny, fmt.Errorf("another local observation is already pending")
	}
	id := fmt.Sprintf("observation-%d", time.Now().UnixNano())
	var tool intake.ToolCall
	if err := json.Unmarshal([]byte(request.Command), &tool); err != nil {
		a.run.mu.Unlock()
		return approval.DecisionDeny, fmt.Errorf("invalid observation request")
	}
	pending := &intakeApproval{ID: id, Tool: tool, Summary: request.Summary, Impact: request.Impact, Target: request.Target, Result: make(chan approval.Decision, 1)}
	a.run.pendingTool = pending
	a.run.updatedAt = time.Now().UTC()
	a.run.mu.Unlock()
	select {
	case decision := <-pending.Result:
		a.run.mu.Lock()
		if a.run.pendingTool == pending {
			a.run.pendingTool = nil
		}
		a.run.mu.Unlock()
		return decision, nil
	case <-ctx.Done():
		a.run.mu.Lock()
		if a.run.pendingTool == pending {
			a.run.pendingTool = nil
		}
		a.run.mu.Unlock()
		return approval.DecisionDeny, ctx.Err()
	}
}

func (r *intakeRun) approveTool(id, decision string) error {
	r.mu.Lock()
	pending := r.pendingTool
	if pending == nil || pending.ID != id {
		r.mu.Unlock()
		return fmt.Errorf("observation is no longer pending")
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	var result approval.Decision
	switch decision {
	case "approved_once":
		result = approval.DecisionApproveOnce
	case "denied":
		result = approval.DecisionDeny
	default:
		r.mu.Unlock()
		return fmt.Errorf("unsupported observation decision")
	}
	r.pendingTool = nil
	r.updatedAt = time.Now().UTC()
	pending.Result <- result
	r.mu.Unlock()
	return nil
}

func cloneDraft(draft *intake.Draft) *intake.Draft {
	if draft == nil {
		return nil
	}
	copy := *draft
	copy.Approaches = append([]assessment.Approach(nil), draft.Approaches...)
	return &copy
}

func (r *intakeRun) removeLastMessage(role, text string) {
	if len(r.messages) == 0 {
		return
	}
	last := r.messages[len(r.messages)-1]
	if last.Role == role && last.Text == text {
		r.messages = r.messages[:len(r.messages)-1]
	}
}
