// Package webapp provides the browser adapter for the shared assessment runtime.
// It owns HTTP state and presentation only; the assessment coordinator remains
// the owner of planning, approvals, execution, evidence, and reporting.
package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

type Config struct {
	RepoRoot     string
	SessionsRoot string
	LLM          llmclient.Client
	Frame        behavior.Frame
	Limits       assessment.Limits
}

type Server struct {
	config  Config
	mu      sync.RWMutex
	runs    map[string]*run
	intakes map[string]*intakeRun
	seq     atomic.Uint64
}

type intakeRun struct {
	mu           sync.RWMutex
	id           string
	conversation intake.Conversation
	messages     []intakeMessage
	proposal     *intake.Draft
	assessmentID string
	busy         bool
	pendingTool  *intakeApproval
	events       []eventRecord
}

type intakeMessage struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type intakeView struct {
	ID              string              `json:"id"`
	Model           string              `json:"model"`
	ModelConfigured bool                `json:"model_configured"`
	Status          string              `json:"status"`
	Messages        []intakeMessage     `json:"messages"`
	Proposal        *intake.Draft       `json:"proposal,omitempty"`
	PendingTool     *intakeApprovalView `json:"pending_tool,omitempty"`
	Events          []eventRecord       `json:"events"`
	AssessmentID    string              `json:"assessment_id,omitempty"`
}

type intakeApproval struct {
	ID     string
	Tool   intake.ToolCall
	Result chan approval.Decision
}

type intakeApprovalView struct {
	ID   string          `json:"id"`
	Tool intake.ToolCall `json:"tool"`
}

type intakeMessageRequest struct {
	Text string `json:"text"`
}

type intakeStartRequest struct {
	Customer string `json:"customer"`
}

type run struct {
	mu       sync.RWMutex
	id       string
	customer string
	root     string
	goal     string
	scope    string
	status   string
	state    assessment.State
	started  bool
	cancel   context.CancelFunc
	done     chan struct{}
	chatBusy bool

	sequence  uint64
	events    []eventRecord
	messages  []intakeMessage
	approvals map[string]*pendingApproval
	questions map[string]*pendingQuestion
	workers   map[string]workerView
}

type eventRecord struct {
	Sequence uint64           `json:"sequence"`
	At       time.Time        `json:"at"`
	Event    assessment.Event `json:"event"`
}

type pendingApproval struct {
	ID      string
	taskID  string
	request approval.Request
	result  chan approval.Decision
}

type pendingQuestion struct {
	ID     string
	taskID string
	text   string
	answer chan string
}

type createRequest struct {
	Customer string `json:"customer"`
	Goal     string `json:"goal"`
	Scope    string `json:"scope"`
}

type messageRequest struct {
	Text string `json:"text"`
}

type approvalRequest struct {
	Decision string `json:"decision"`
}

type assessmentView struct {
	Customer         string               `json:"customer"`
	ID               string               `json:"id"`
	Goal             string               `json:"goal"`
	Scope            string               `json:"scope"`
	Status           string               `json:"status"`
	Model            string               `json:"model"`
	Error            string               `json:"error,omitempty"`
	StartedAt        time.Time            `json:"started_at,omitempty"`
	FinishedAt       time.Time            `json:"finished_at,omitempty"`
	Usage            assessment.Usage     `json:"usage"`
	Plans            int                  `json:"plans"`
	Workers          []workerView         `json:"workers"`
	Findings         []assessment.Finding `json:"findings"`
	Limits           assessment.Limits    `json:"limits"`
	Results          []assessment.Result  `json:"results"`
	Events           []eventRecord        `json:"events"`
	PendingApprovals []approvalView       `json:"pending_approvals"`
	PendingQuestions []questionView       `json:"pending_questions"`
	Messages         []intakeMessage      `json:"messages"`
	ReportURL        string               `json:"report_url,omitempty"`
}

type customerView struct {
	ID        string            `json:"id"`
	Sessions  []assessmentView  `json:"sessions"`
	Findings  []customerFinding `json:"findings"`
	Status    string            `json:"status"`
	ReportURL string            `json:"report_url"`
}

type customerFinding struct {
	SessionID string             `json:"session_id"`
	Finding   assessment.Finding `json:"finding"`
}

type approvalView struct {
	ID       string `json:"id"`
	TaskID   string `json:"task_id"`
	Command  string `json:"command"`
	UseShell bool   `json:"use_shell"`
	Cwd      string `json:"cwd"`
	Impact   string `json:"impact,omitempty"`
}

type questionView struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Text   string `json:"text"`
}

func NewServer(config Config) *Server {
	if config.SessionsRoot == "" {
		config.SessionsRoot = filepath.Join(config.RepoRoot, "sessions", "web")
	}
	if config.Limits == (assessment.Limits{}) {
		config.Limits = assessment.DefaultLimits()
	}
	return &Server{config: config, runs: make(map[string]*run), intakes: make(map[string]*intakeRun)}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if serveUI(w, r) {
		return
	}
	if r.URL.Path == "/" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
		return
	}
	if r.URL.Path == "/api/v1/healthz" {
		s.health(w, r)
		return
	}
	if r.URL.Path == "/api/v1/intake" || strings.HasPrefix(r.URL.Path, "/api/v1/intake/") {
		s.intakeRoute(w, r)
		return
	}
	if r.URL.Path == "/api/v1/assessments" {
		s.assessments(w, r)
		return
	}
	if r.URL.Path == "/api/v1/customers" {
		s.customers(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/assessments/") {
		s.assessmentRoute(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/customers/") {
		s.customerRoute(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"model_configured": strings.TrimSpace(s.config.LLM.BaseURL) != "" && strings.TrimSpace(s.config.LLM.Model) != "",
		"loopback_warning": "This preview has no authentication; bind it to loopback and use only an authorized lab.",
	})
}

func (s *Server) intakeRoute(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/intake" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		current := s.newIntake()
		writeJSON(w, http.StatusOK, current.view(s.config.LLM))
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/intake/"), "/"), "/")
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodGet {
		current := s.getIntake(parts[0])
		if current == nil {
			writeError(w, http.StatusNotFound, "intake session not found")
			return
		}
		writeJSON(w, http.StatusOK, current.view(s.config.LLM))
		return
	}
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 3 && parts[1] != "approvals" {
		http.NotFound(w, r)
		return
	}
	current := s.getIntake(parts[0])
	if current == nil {
		writeError(w, http.StatusNotFound, "intake session not found")
		return
	}
	if len(parts) == 3 {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
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
		writeJSON(w, http.StatusOK, current.view(s.config.LLM))
		return
	}
	switch parts[1] {
	case "approvals":
		// Keep the route shape explicit: the pending observation ID belongs in
		// the path, just like assessment action approvals.
		http.NotFound(w, r)
	case "messages":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input intakeMessageRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.intakeMessage(r.Context(), current, input.Text); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, current.view(s.config.LLM))
	case "start":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input intakeStartRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		currentView, err := s.startIntake(current, input.Customer)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, currentView)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) newIntake() *intakeRun {
	current := &intakeRun{id: fmt.Sprintf("intake-%06d", s.seq.Add(1))}
	s.mu.Lock()
	s.intakes[current.id] = current
	s.mu.Unlock()
	return current
}

func (s *Server) getIntake(id string) *intakeRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.intakes[id]
}

func (s *Server) intakeMessage(ctx context.Context, current *intakeRun, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("message is required")
	}
	current.mu.Lock()
	if current.assessmentID != "" {
		current.mu.Unlock()
		return fmt.Errorf("this conversation already started an assessment")
	}
	if current.busy {
		current.mu.Unlock()
		return fmt.Errorf("the coordinator is still answering the previous message")
	}
	current.busy = true
	current.messages = append(current.messages, intakeMessage{Role: "user", Text: text, At: time.Now().UTC()})
	current.mu.Unlock()
	defer func() {
		current.mu.Lock()
		current.busy = false
		current.mu.Unlock()
	}()
	current.conversation.Inspection = &intake.Inspection{
		Workspace:   s.config.RepoRoot,
		EvidenceDir: filepath.Join(s.config.SessionsRoot, "intake", current.id),
		Policy:      "Authorized lab only. Keep observations minimal and local; do not access credentials or mutate files.",
		Approver:    &intakeToolApprover{run: current},
		Emit:        current.recordObservation,
	}
	turn, err := current.conversation.Turn(ctx, s.config.LLM, text)
	if err != nil {
		current.mu.Lock()
		current.removeLastMessage("user", text)
		current.mu.Unlock()
		return err
	}
	current.mu.Lock()
	current.messages = append(current.messages, intakeMessage{Role: "assistant", Text: turn.Reply, At: time.Now().UTC()})
	current.proposal = cloneDraft(turn.Proposal)
	current.mu.Unlock()
	return nil
}

func (s *Server) startIntake(current *intakeRun, customer string) (assessmentView, error) {
	customer = strings.TrimSpace(customer)
	if !validCustomerID(customer) {
		return assessmentView{}, fmt.Errorf("customer must contain only letters, numbers, hyphens, or underscores")
	}
	current.mu.RLock()
	proposal := current.proposal
	assessmentID := current.assessmentID
	busy := current.busy
	current.mu.RUnlock()
	if busy {
		return assessmentView{}, fmt.Errorf("wait for the coordinator to finish before starting the assessment")
	}
	if assessmentID != "" {
		return assessmentView{}, fmt.Errorf("this conversation already started an assessment")
	}
	if proposal == nil {
		return assessmentView{}, fmt.Errorf("the coordinator has not proposed an assessment yet")
	}
	if strings.TrimSpace(s.config.LLM.BaseURL) == "" || strings.TrimSpace(s.config.LLM.Model) == "" {
		return assessmentView{}, fmt.Errorf("model endpoint and model are required; configure the web server first")
	}
	created, err := s.newRun(customer, proposal.Goal, proposal.Scope)
	if err != nil {
		return assessmentView{}, err
	}
	if err := s.start(created); err != nil {
		return assessmentView{}, err
	}
	current.mu.Lock()
	current.assessmentID = created.id
	intakeMessages := append([]intakeMessage(nil), current.messages...)
	current.mu.Unlock()
	created.mu.Lock()
	created.messages = intakeMessages
	created.mu.Unlock()
	return created.view(""), nil
}

func (r *intakeRun) view(client llmclient.Client) intakeView {
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
		pending = &intakeApprovalView{ID: r.pendingTool.ID, Tool: r.pendingTool.Tool}
	}
	return intakeView{ID: r.id, Model: client.Model, ModelConfigured: strings.TrimSpace(client.BaseURL) != "" && strings.TrimSpace(client.Model) != "", Status: status, Messages: append([]intakeMessage(nil), r.messages...), Proposal: cloneDraft(r.proposal), PendingTool: pending, Events: append([]eventRecord(nil), r.events...), AssessmentID: r.assessmentID}
}

func (r *intakeRun) recordObservation(event assessment.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, eventRecord{Sequence: uint64(len(r.events) + 1), At: time.Now().UTC(), Event: event})
}

type intakeToolApprover struct{ run *intakeRun }

func (a *intakeToolApprover) Approve(ctx context.Context, request approval.Request) (approval.Decision, error) {
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
	pending := &intakeApproval{ID: id, Tool: tool, Result: make(chan approval.Decision, 1)}
	a.run.pendingTool = pending
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
	defer r.mu.Unlock()
	pending := r.pendingTool
	if pending == nil || pending.ID != id {
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
		return fmt.Errorf("unsupported observation decision")
	}
	r.pendingTool = nil
	pending.Result <- result
	return nil
}

func cloneDraft(draft *intake.Draft) *intake.Draft {
	if draft == nil {
		return nil
	}
	copy := *draft
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
	ID       string           `json:"id"`
	Status   string           `json:"status"`
	Sessions []assessmentView `json:"sessions"`
}

func (s *Server) customers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.RLock()
	ids := make(map[string]struct{})
	for _, current := range s.runs {
		current.mu.RLock()
		ids[current.customer] = struct{}{}
		current.mu.RUnlock()
	}
	s.mu.RUnlock()
	views := make([]customerIndexView, 0, len(ids))
	for id := range ids {
		view := s.customerView(id)
		views = append(views, customerIndexView{ID: view.ID, Status: view.Status, Sessions: view.Sessions})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"customers": views})
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
	current := &run{id: id, customer: customer, root: root, goal: goal, scope: scope, status: "draft", done: make(chan struct{}), approvals: make(map[string]*pendingApproval), questions: make(map[string]*pendingQuestion)}
	s.mu.Lock()
	s.runs[id] = current
	s.mu.Unlock()
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
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		current.writeView(w, r.URL.Query().Get("after"))
		return
	}
	switch parts[1] {
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
	case "messages":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input messageRequest
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.message(r.Context(), current, input.Text); err != nil {
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
	case "report":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		current.report(w)
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
	methodNotAllowed(w, http.MethodGet)
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
		for _, plan := range current.state.Plans {
			for _, finding := range plan.Findings {
				key := current.id + "\x00" + finding.Title + "\x00" + finding.Status + "\x00" + strings.Join(finding.Evidence, "\x00")
				if _, exists := seenFindings[key]; exists {
					continue
				}
				seenFindings[key] = struct{}{}
				view.Findings = append(view.Findings, customerFinding{SessionID: current.id, Finding: finding})
			}
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
			fmt.Fprintf(&b, "### %s\n\nSession: `%s`\n\nStatus: %s\n\nImpact: %s\n\n", item.Finding.Title, item.SessionID, item.Finding.Status, item.Finding.Impact)
			b.WriteString("Evidence:\n\n")
			for _, evidence := range item.Finding.Evidence {
				fmt.Fprintf(&b, "- %s\n", evidence)
			}
			b.WriteString("\n")
		}
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

func (s *Server) start(current *run) error {
	current.mu.Lock()
	if current.started {
		current.mu.Unlock()
		return fmt.Errorf("assessment has already been started")
	}
	if strings.TrimSpace(s.config.LLM.BaseURL) == "" || strings.TrimSpace(s.config.LLM.Model) == "" {
		current.mu.Unlock()
		return fmt.Errorf("model endpoint and model are required; configure the web server first")
	}
	ctx, cancel := context.WithCancel(context.Background())
	current.started, current.status, current.cancel = true, "starting", cancel
	current.mu.Unlock()
	current.emit(assessment.Event{Kind: "assessment_started", Message: "Assessment accepted; the coordinator is preparing its first plan."})
	go s.runAssessment(ctx, current)
	return nil
}

func (s *Server) runAssessment(ctx context.Context, current *run) {
	runner := assessment.Coordinator{
		LLM:    s.config.LLM,
		Frame:  s.config.Frame,
		Limits: s.config.Limits,
		Emit:   current.emit,
		Approver: func(task assessment.Task) approval.Approver {
			return &runApprover{run: current, taskID: task.ID}
		},
		AskUser: func(ctx context.Context, task assessment.Task, question string) (string, error) {
			return current.ask(ctx, task.ID, question)
		},
		Conversation: current.conversation,
		Snapshot:     current.snapshot,
	}
	state, err := runner.Run(ctx, current.root, current.goal, current.scope)
	current.mu.Lock()
	current.state = state
	if err != nil {
		current.status = state.Status
		if current.status == "" {
			current.status = "incomplete"
		}
	} else {
		current.status = state.Status
	}
	current.mu.Unlock()
	current.emit(assessment.Event{Kind: "assessment_finished", Message: current.status})
	close(current.done)
}

func (r *run) snapshot(state assessment.State) {
	r.mu.Lock()
	r.state = state
	r.status = state.Status
	r.mu.Unlock()
}

func (r *run) conversation() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]string, 0, len(r.messages))
	for _, message := range r.messages {
		values = append(values, message.Role+": "+message.Text)
	}
	return values
}

func (r *run) emit(event assessment.Event) {
	r.mu.Lock()
	r.updateWorker(event)
	r.sequence++
	r.events = append(r.events, eventRecord{Sequence: r.sequence, At: time.Now().UTC(), Event: event})
	if len(r.events) > 200 {
		r.events = r.events[len(r.events)-200:]
	}
	r.mu.Unlock()
}

func (r *run) stop() error {
	r.mu.RLock()
	cancel, started, status := r.cancel, r.started, r.status
	r.mu.RUnlock()
	if !started || cancel == nil {
		return fmt.Errorf("assessment is not running")
	}
	if status != "running" && status != "starting" {
		return fmt.Errorf("assessment is already %s", status)
	}
	cancel()
	r.emit(assessment.Event{Kind: "assessment_stop_requested", Message: "Stop requested; waiting for workers to finalize."})
	return nil
}

func (s *Server) message(ctx context.Context, r *run, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("message is required")
	}
	r.mu.Lock()
	if !r.started || (r.status != "running" && r.status != "starting") {
		r.mu.Unlock()
		return fmt.Errorf("start the assessment before sending messages")
	}
	for id, question := range r.questions {
		delete(r.questions, id)
		r.messages = append(r.messages, intakeMessage{Role: "user", Text: text, At: time.Now().UTC()})
		question.answer <- text
		r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "operator_message", Message: text}})
		r.mu.Unlock()
		return nil
	}
	if r.chatBusy {
		r.mu.Unlock()
		return fmt.Errorf("the coordinator is still answering the previous message")
	}
	r.chatBusy = true
	r.messages = append(r.messages, intakeMessage{Role: "user", Text: text, At: time.Now().UTC()})
	r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "operator_message", Message: text}})
	state := r.state
	pending := make([]string, 0, len(r.approvals)+len(r.questions))
	for _, approval := range r.approvals {
		pending = append(pending, "approval required for "+approval.request.Command+" in "+approval.request.Cwd)
	}
	for _, question := range r.questions {
		pending = append(pending, "question for "+question.taskID+": "+question.text)
	}
	sort.Strings(pending)
	r.mu.Unlock()

	prompt := []llmclient.Message{
		{Role: "system", Content: "You are the assessment coordinator's conversational interface. Answer the operator directly and concisely while workers may be running. Explain progress, blockers, and next steps from the supplied state. Preserve scope and approval boundaries. Do not claim that a finding is confirmed from chat alone. Do not execute tools or change the plan from this chat response. Task working directories shown in pending actions are internal evidence workspaces managed by the runtime; they are not target scope. Judge command arguments against the declared scope."},
		{Role: "user", Content: "Current assessment state (untrusted evidence): " + compactRunState(state, pending) + "\nOperator message: " + text},
	}
	reply, err := s.config.LLM.Chat(ctx, prompt)
	r.mu.Lock()
	r.chatBusy = false
	if err != nil {
		r.removeLastMessage("user", text)
		r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "coordinator_error", Message: err.Error()}})
		r.mu.Unlock()
		return fmt.Errorf("coordinator response: %w", err)
	}
	r.messages = append(r.messages, intakeMessage{Role: "assistant", Text: strings.TrimSpace(reply), At: time.Now().UTC()})
	r.state.Usage.Calls++
	r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "coordinator_message", Message: strings.TrimSpace(reply)}})
	r.mu.Unlock()
	return nil
}

func (r *run) removeLastMessage(role, text string) {
	if len(r.messages) == 0 {
		return
	}
	last := r.messages[len(r.messages)-1]
	if last.Role == role && last.Text == text {
		r.messages = r.messages[:len(r.messages)-1]
	}
}

func compactRunState(state assessment.State, pending []string) string {
	results := make([]string, 0, len(state.Results))
	for _, result := range state.Results {
		summary := strings.Join(strings.Fields(result.Summary), " ")
		if len(summary) > 240 {
			summary = summary[:237] + "..."
		}
		results = append(results, result.Task.ID+"="+result.Status+": "+summary)
	}
	data, _ := json.Marshal(struct {
		Status     string   `json:"status"`
		Goal       string   `json:"goal"`
		Scope      string   `json:"scope"`
		Plans      int      `json:"plans"`
		Results    []string `json:"results"`
		Pending    []string `json:"pending_operator_actions"`
		ModelCalls int      `json:"model_calls"`
	}{Status: state.Status, Goal: state.Goal, Scope: state.Scope, Plans: len(state.Plans), Results: results, Pending: pending, ModelCalls: state.Usage.Calls})
	return string(data)
}

func (r *run) ask(ctx context.Context, taskID, text string) (string, error) {
	r.mu.Lock()
	id := r.nextIDLocked("question")
	question := &pendingQuestion{ID: id, taskID: taskID, text: text, answer: make(chan string, 1)}
	r.questions[id] = question
	r.sequence++
	r.events = append(r.events, eventRecord{Sequence: r.sequence, At: time.Now().UTC(), Event: assessment.Event{TaskID: taskID, Kind: "user_question", Message: text}})
	r.mu.Unlock()
	select {
	case answer := <-question.answer:
		return answer, nil
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.questions, id)
		r.mu.Unlock()
		return "", ctx.Err()
	}
}

func (r *run) answer(id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("answer is required")
	}
	r.mu.Lock()
	question := r.questions[id]
	if question != nil {
		delete(r.questions, id)
	}
	r.mu.Unlock()
	if question == nil {
		return fmt.Errorf("question is no longer pending")
	}
	question.answer <- text
	return nil
}

func (r *run) nextIDLocked(prefix string) string {
	r.sequence++
	return fmt.Sprintf("%s-%d", prefix, r.sequence)
}

func (r *run) nextSequenceLocked() uint64 {
	r.sequence++
	return r.sequence
}

func (r *run) approve(id, decision string) error {
	decision = strings.ToLower(strings.TrimSpace(decision))
	var value approval.Decision
	switch decision {
	case "approved_once", "approve_once", "approve", "yes":
		value = approval.DecisionApproveOnce
	case "approved_session", "approve_session":
		value = approval.DecisionApproveSession
	case "denied", "deny", "no":
		value = approval.DecisionDeny
	default:
		return fmt.Errorf("unsupported approval decision")
	}
	r.mu.RLock()
	pending := r.approvals[id]
	r.mu.RUnlock()
	if pending == nil {
		return fmt.Errorf("approval is no longer pending")
	}
	pending.result <- value
	return nil
}

func (r *run) view(after string) assessmentView {
	r.mu.RLock()
	defer r.mu.RUnlock()
	view := assessmentView{Customer: r.customer, ID: r.id, Goal: r.goal, Scope: r.scope, Status: r.status, Model: r.state.Model, Error: r.state.Error, StartedAt: r.state.StartedAt, FinishedAt: r.state.FinishedAt, Usage: r.state.Usage, Plans: len(r.state.Plans), Results: append([]assessment.Result(nil), r.state.Results...), Messages: append([]intakeMessage(nil), r.messages...), ReportURL: "/api/v1/assessments/" + r.id + "/report"}
	view.Limits = r.state.Limits
	for _, worker := range r.workers {
		view.Workers = append(view.Workers, worker)
	}
	sort.Slice(view.Workers, func(i, j int) bool { return view.Workers[i].ID < view.Workers[j].ID })
	for _, plan := range r.state.Plans {
		view.Findings = append(view.Findings, plan.Findings...)
	}
	if n, err := strconv.ParseUint(strings.TrimSpace(after), 10, 64); err == nil {
		for _, event := range r.events {
			if event.Sequence > n {
				view.Events = append(view.Events, event)
			}
		}
	} else {
		view.Events = append([]eventRecord(nil), r.events...)
	}
	for _, pending := range r.approvals {
		view.PendingApprovals = append(view.PendingApprovals, approvalView{ID: pending.ID, TaskID: pending.taskID, Command: pending.request.Command, UseShell: pending.request.UseShell, Cwd: pending.request.Cwd, Impact: pending.request.Impact})
	}
	for _, question := range r.questions {
		view.PendingQuestions = append(view.PendingQuestions, questionView{ID: question.ID, TaskID: question.taskID, Text: question.text})
	}
	sort.Slice(view.PendingApprovals, func(i, j int) bool { return view.PendingApprovals[i].ID < view.PendingApprovals[j].ID })
	sort.Slice(view.PendingQuestions, func(i, j int) bool { return view.PendingQuestions[i].ID < view.PendingQuestions[j].ID })
	return view
}

func (r *run) writeView(w http.ResponseWriter, after string) {
	writeJSON(w, http.StatusOK, r.view(after))
}

func (r *run) report(w http.ResponseWriter) {
	data, err := os.ReadFile(filepath.Join(r.root, "report.md"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "report is not ready")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write(data)
}

type runApprover struct {
	run    *run
	taskID string
}

func (a *runApprover) Approve(ctx context.Context, request approval.Request) (approval.Decision, error) {
	a.run.mu.Lock()
	id := a.run.nextIDLocked("approval")
	pending := &pendingApproval{ID: id, taskID: a.taskID, request: request, result: make(chan approval.Decision, 1)}
	a.run.approvals[id] = pending
	a.run.events = append(a.run.events, eventRecord{Sequence: a.run.sequence, At: time.Now().UTC(), Event: assessment.Event{TaskID: a.taskID, Kind: "approval_required", Action: request.Command, Message: "Operator approval required"}})
	a.run.mu.Unlock()
	select {
	case decision := <-pending.result:
		a.run.mu.Lock()
		delete(a.run.approvals, id)
		a.run.mu.Unlock()
		return decision, nil
	case <-ctx.Done():
		a.run.mu.Lock()
		delete(a.run.approvals, id)
		a.run.mu.Unlock()
		return approval.DecisionDeny, ctx.Err()
	}
}

func (r *run) nextID(prefix string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextIDLocked(prefix)
}

func decodeJSON(w http.ResponseWriter, request *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, 32*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
