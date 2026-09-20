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
	config Config
	mu     sync.RWMutex
	runs   map[string]*run
	seq    atomic.Uint64
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

	sequence  uint64
	events    []eventRecord
	messages  []string
	approvals map[string]*pendingApproval
	questions map[string]*pendingQuestion
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
	Customer         string              `json:"customer"`
	ID               string              `json:"id"`
	Goal             string              `json:"goal"`
	Scope            string              `json:"scope"`
	Status           string              `json:"status"`
	Model            string              `json:"model"`
	Error            string              `json:"error,omitempty"`
	StartedAt        time.Time           `json:"started_at,omitempty"`
	FinishedAt       time.Time           `json:"finished_at,omitempty"`
	Usage            assessment.Usage    `json:"usage"`
	Plans            int                 `json:"plans"`
	Results          []assessment.Result `json:"results"`
	Events           []eventRecord       `json:"events"`
	PendingApprovals []approvalView      `json:"pending_approvals"`
	PendingQuestions []questionView      `json:"pending_questions"`
	ReportURL        string              `json:"report_url,omitempty"`
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
	return &Server{config: config, runs: make(map[string]*run)}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	if r.URL.Path == "/api/v1/assessments" {
		s.assessments(w, r)
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
		if err := current.message(input.Text); err != nil {
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
	return append([]string(nil), r.messages...)
}

func (r *run) emit(event assessment.Event) {
	r.mu.Lock()
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

func (r *run) message(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("message is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return fmt.Errorf("start the assessment before sending messages")
	}
	for id, question := range r.questions {
		delete(r.questions, id)
		question.answer <- text
		r.messages = append(r.messages, "user: "+text)
		return nil
	}
	r.messages = append(r.messages, "user: "+text)
	r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "operator_message", Message: text}})
	return nil
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
	view := assessmentView{Customer: r.customer, ID: r.id, Goal: r.goal, Scope: r.scope, Status: r.status, Model: r.state.Model, Error: r.state.Error, StartedAt: r.state.StartedAt, FinishedAt: r.state.FinishedAt, Usage: r.state.Usage, Plans: len(r.state.Plans), Results: append([]assessment.Result(nil), r.state.Results...), ReportURL: "/api/v1/assessments/" + r.id + "/report"}
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
		view.PendingApprovals = append(view.PendingApprovals, approvalView{ID: pending.ID, TaskID: pending.taskID, Command: pending.request.Command, UseShell: pending.request.UseShell, Cwd: pending.request.Cwd})
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
