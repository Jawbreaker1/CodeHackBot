package webapp

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func (s *Server) assessments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		views := make([]assessmentView, 0, len(s.runs))
		for _, current := range s.runs {
			views = append(views, current.view(""))
		}
		s.mu.RUnlock()
		sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
		writeJSON(w, http.StatusOK, map[string]any{"assessments": views})
	case http.MethodPost:
		var input createRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		input.Customer = strings.TrimSpace(input.Customer)
		input.Goal, input.Scope = strings.TrimSpace(input.Goal), strings.TrimSpace(input.Scope)
		if input.Customer == "" || input.Goal == "" || input.Scope == "" {
			writeError(w, http.StatusBadRequest, "customer, goal, and scope are required")
			return
		}
		if !validCustomerID(input.Customer) {
			writeError(w, http.StatusBadRequest, "customer must contain only letters, numbers, hyphens, or underscores")
			return
		}
		if len(input.Goal) > 12000 || len(input.Scope) > 12000 {
			writeError(w, http.StatusBadRequest, "goal and scope must be at most 12000 bytes each")
			return
		}
		current, err := s.newRun(input.Customer, input.Goal, input.Scope)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, current.view(""))
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

type customerIndexView struct {
	ID       string                `json:"id"`
	Title    string                `json:"title"`
	Status   string                `json:"status"`
	Sessions []assessmentIndexView `json:"sessions"`
	Drafts   []intakeIndexView     `json:"drafts,omitempty"`
}

type assessmentIndexView struct {
	ID        string    `json:"id"`
	Goal      string    `json:"goal"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type intakeIndexView struct {
	ID        string    `json:"id"`
	Customer  string    `json:"customer,omitempty"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Model     string    `json:"model"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (s *Server) customers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var input folderRecord
		if !decodeJSON(w, r, &input) {
			return
		}
		id, err := s.createFolder(input.Title)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "title": strings.TrimSpace(input.Title)})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	folderTitles := s.folderTitles()
	s.mu.RLock()
	groups := make(map[string]*customerIndexView)
	ensureGroup := func(id string) *customerIndexView {
		if group := groups[id]; group != nil {
			return group
		}
		title := folderTitles[id]
		if title == "" {
			title = id
		}
		group := &customerIndexView{ID: id, Title: title, Status: "no_sessions"}
		groups[id] = group
		return group
	}
	for id := range folderTitles {
		ensureGroup(id)
	}
	for _, current := range s.runs {
		current.mu.RLock()
		group := ensureGroup(current.customer)
		group.Sessions = append(group.Sessions, assessmentIndexView{ID: current.id, Goal: current.goal, Status: current.status, UpdatedAt: current.updatedAt})
		if current.status == "running" || current.status == "starting" {
			group.Status = "active"
		} else if group.Status == "no_sessions" {
			group.Status = current.status
		}
		current.mu.RUnlock()
	}
	intakes := make([]intakeIndexView, 0, len(s.intakes))
	for _, current := range s.intakes {
		view := current.view()
		if view.AssessmentID != "" {
			continue
		}
		current.mu.RLock()
		updatedAt := current.updatedAt
		current.mu.RUnlock()
		draft := intakeIndexView{ID: view.ID, Customer: view.Customer, Title: view.Title, Status: view.Status, Model: view.Model, UpdatedAt: updatedAt}
		if view.Customer != "" {
			ensureGroup(view.Customer).Drafts = append(ensureGroup(view.Customer).Drafts, draft)
		} else {
			intakes = append(intakes, draft)
		}
	}
	s.mu.RUnlock()
	views := make([]customerIndexView, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.Sessions, func(a, b int) bool { return group.Sessions[a].ID < group.Sessions[b].ID })
		sort.Slice(group.Drafts, func(a, b int) bool { return group.Drafts[a].ID < group.Drafts[b].ID })
		views = append(views, *group)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Title < views[j].Title })
	sort.Slice(intakes, func(i, j int) bool { return intakes[i].ID < intakes[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"customers": views, "intakes": intakes})
}

func (s *Server) newRun(customer, goal, scope string) (*run, error) {
	if err := os.MkdirAll(s.config.SessionsRoot, 0700); err != nil {
		return nil, fmt.Errorf("create web sessions directory: %w", err)
	}
	id := fmt.Sprintf("web-assessment-%s-%06d", time.Now().UTC().Format("20060102-150405.000000000"), s.seq.Add(1))
	root := filepath.Join(s.config.SessionsRoot, customer, id)
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return nil, fmt.Errorf("create customer sessions directory: %w", err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return nil, fmt.Errorf("create assessment directory: %w", err)
	}
	current := &run{id: id, customer: customer, root: root, client: s.config.LLM, profileID: s.config.DefaultProfile, goal: goal, scope: scope, status: "draft", done: make(chan struct{}), approvals: make(map[string]*pendingApproval), questions: make(map[string]*pendingQuestion), updatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.runs[id] = current
	s.mu.Unlock()
	if err := current.persist(); err != nil {
		return nil, fmt.Errorf("save assessment session: %w", err)
	}
	return current, nil
}

func (s *Server) assessmentRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/assessments/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	current := s.getRun(parts[0])
	if current == nil {
		writeError(w, http.StatusNotFound, "assessment not found")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := s.deleteRun(current); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		current.writeView(w, r.URL.Query().Get("after"))
		return
	}
	if len(parts) == 2 && parts[1] == "customer" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input intakeCustomerRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.assignRunCustomer(current, input.Customer); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, current.view(""))
		return
	}
	if len(parts) == 3 && parts[1] == "attachments" {
		current.mu.RLock()
		root, messages := current.root, append([]intakeMessage(nil), current.messages...)
		current.mu.RUnlock()
		s.serveAttachment(w, r, root, messages, parts[2])
		return
	}
	if len(parts) == 2 && parts[1] == "artifact" {
		s.serveAssessmentArtifact(w, r, current)
		return
	}
	if len(parts) == 3 && parts[1] == "analysis" && parts[2] == "notes" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input webNote
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := current.saveWebNote(input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, current.analysis())
		return
	}
	if len(parts) == 3 && parts[1] == "context" {
		s.contextDebug(w, r, current, parts[2])
		return
	}
	switch parts[1] {
	case "permissions":
		s.changeRunPermissions(w, r, current)
	case "watch":
		current.watch(w, r)
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
			err = s.changeRunProfile(current, input.Profile)
		} else {
			err = s.changeRunModel(current, input.Model)
		}
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		current.writeView(w, "")
	case "start":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if err := s.start(current); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		current.writeView(w, "")
	case "stop":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if err := current.stop(); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		current.writeView(w, "")
	case "pause":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if err := current.pause(); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		current.writeView(w, "")
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
		if err := s.message(r.Context(), current, text, refs); err != nil {
			removeAttachmentFiles(current.root, refs)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		current.writeView(w, "")
	case "approvals":
		if len(parts) != 3 || r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input approvalRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := current.approve(parts[2], input.Decision); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		current.writeView(w, "")
	case "questions":
		if len(parts) != 3 || r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input messageRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := current.answer(parts[2], input.Text); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		current.writeView(w, "")
	case "plans":
		if len(parts) != 3 || r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input planReviewRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := current.reviewPlan(parts[2], input); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		current.writeView(w, "")
	case "report":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		current.report(w)
	case "evidence-index":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		current.evidenceIndex(w)
	case "reports":
		if len(parts) != 3 || r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		current.formattedReport(w, parts[2])
	case "analysis":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, current.analysis())
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) customerRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/customers/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" || !validCustomerID(parts[0]) {
		http.NotFound(w, r)
		return
	}
	view := s.customerView(parts[0])
	if len(parts) == 1 && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, view)
		return
	}
	if len(parts) == 2 && parts[1] == "report" && r.Method == http.MethodGet {
		s.customerReport(w, view)
		return
	}
	if len(parts) == 2 && parts[1] == "analysis" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.customerAnalysis(parts[0]))
		return
	}
	methodNotAllowed(w, http.MethodGet)
}

func (s *Server) customerAnalysis(id string) analysisView {
	s.mu.RLock()
	runs := make([]*run, 0)
	for _, current := range s.runs {
		current.mu.RLock()
		matches := current.customer == id
		current.mu.RUnlock()
		if matches {
			runs = append(runs, current)
		}
	}
	s.mu.RUnlock()
	sessions := make([]analysisView, 0, len(runs))
	for _, current := range runs {
		sessions = append(sessions, current.analysis())
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
	return buildCustomerAnalysis(id, sessions)
}

func (s *Server) customerView(id string) customerView {
	s.mu.RLock()
	runs := make([]*run, 0)
	for _, current := range s.runs {
		current.mu.RLock()
		matches := current.customer == id
		current.mu.RUnlock()
		if matches {
			runs = append(runs, current)
		}
	}
	s.mu.RUnlock()
	view := customerView{ID: id, Status: "no_sessions", ReportURL: "/api/v1/customers/" + id + "/report"}
	seenFindings := make(map[string]struct{})
	for _, current := range runs {
		session := current.view("")
		view.Sessions = append(view.Sessions, session)
		if session.Status == "running" || session.Status == "starting" {
			view.Status = "active"
		} else if view.Status == "no_sessions" {
			view.Status = session.Status
		}
		current.mu.RLock()
		for _, finding := range assessment.DisplayFindings(current.state) {
			key := current.id + "\x00" + finding.Title + "\x00" + finding.Status + "\x00" + strings.Join(finding.Evidence, "\x00")
			if _, exists := seenFindings[key]; exists {
				continue
			}
			seenFindings[key] = struct{}{}
			view.Findings = append(view.Findings, customerFinding{SessionID: current.id, Finding: finding})
		}
		current.mu.RUnlock()
	}
	sort.Slice(view.Sessions, func(i, j int) bool { return view.Sessions[i].ID < view.Sessions[j].ID })
	sort.Slice(view.Findings, func(i, j int) bool {
		if view.Findings[i].SessionID == view.Findings[j].SessionID {
			return view.Findings[i].Finding.Title < view.Findings[j].Finding.Title
		}
		return view.Findings[i].SessionID < view.Findings[j].SessionID
	})
	return view
}

func (s *Server) customerReport(w http.ResponseWriter, view customerView) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Customer assessment summary: %s\n\nStatus: **%s**\n\n", view.ID, view.Status)
	fmt.Fprintf(&b, "Sessions: %d\n\n", len(view.Sessions))
	for _, session := range view.Sessions {
		fmt.Fprintf(&b, "- `%s`: **%s** — %s\n", session.ID, session.Status, session.Goal)
	}
	b.WriteString("\n## Findings\n\n")
	if len(view.Findings) == 0 {
		b.WriteString("No model-authored findings have been recorded. This is not evidence that the customer environment is secure.\n")
	} else {
		for _, item := range view.Findings {
			fmt.Fprintf(&b, "### %s\n\nSession: `%s`\n\nStatus: %s\n\n", item.Finding.Title, item.SessionID, item.Finding.Status)
			if item.Finding.Severity != "" {
				fmt.Fprintf(&b, "Severity: **%s**\n\n", item.Finding.Severity)
			}
			if len(item.Finding.CVEIDs) > 0 {
				fmt.Fprintf(&b, "CVE references: %s\n\n", strings.Join(item.Finding.CVEIDs, ", "))
			}
			fmt.Fprintf(&b, "Impact: %s\n\n", item.Finding.Impact)
			b.WriteString("Evidence:\n\n")
			for _, evidence := range item.Finding.Evidence {
				fmt.Fprintf(&b, "- %s\n", evidence)
			}
			if len(item.Finding.References) > 0 {
				b.WriteString("\nResearch references:\n\n")
				for _, ref := range item.Finding.References {
					fmt.Fprintf(&b, "- %s\n", ref)
				}
			}
			b.WriteString("\n")
		}
	}
	analysis := s.customerAnalysis(view.ID)
	if len(analysis.PriorReviews) > 0 {
		b.WriteString("## Follow-up on earlier findings\n\nA later worker explicitly retested these earlier records. An omitted finding in any other session is not proof of remediation.\n\n")
		for _, review := range analysis.PriorReviews {
			fmt.Fprintf(&b, "- `%s` — **%s** in session `%s`: %s. Evidence: %s\n", review.FindingID, review.Verdict, review.SessionID, review.Reason, strings.Join(review.Evidence, ", "))
		}
		b.WriteString("\n")
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func validCustomerID(id string) bool {
	if len(id) == 0 || len(id) > 80 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Server) getRun(id string) *run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runs[id]
}
