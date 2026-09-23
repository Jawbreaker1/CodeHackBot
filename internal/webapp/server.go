// Package webapp provides the browser adapter for the shared assessment runtime.
// It owns HTTP state and presentation only; the assessment coordinator remains
// the owner of planning, approvals, execution, evidence, and reporting.
package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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
	RepoRoot       string
	SessionsRoot   string
	LLM            llmclient.Client
	Profiles       []ModelProfile
	DefaultProfile string
	Frame          behavior.Frame
	Limits         assessment.Limits
}

type Server struct {
	config  Config
	mu      sync.RWMutex
	runs    map[string]*run
	intakes map[string]*intakeRun
	seq     atomic.Uint64
	loadErr error
}

type intakeRun struct {
	permissionMode approval.Mode
	mu             sync.RWMutex
	id             string
	root           string
	customer       string
	client         llmclient.Client
	profileID      string
	conversation   intake.Conversation
	conversationMu sync.RWMutex
	messages       []intakeMessage
	proposal       *intake.Draft
	assessmentID   string
	deleted        bool
	busy           bool
	pendingTool    *intakeApproval
	events         []eventRecord
	updatedAt      time.Time
	persistMu      sync.Mutex
}

type intakeMessage struct {
	Role        string           `json:"role"`
	Text        string           `json:"text"`
	Attachments []attachmentRef  `json:"attachments,omitempty"`
	ImageRefs   []string         `json:"image_refs,omitempty"`
	Report      *generatedReport `json:"report,omitempty"`
	At          time.Time        `json:"at"`
}

type attachmentRef struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
	Bytes    int64  `json:"bytes"`
	Path     string `json:"path"`
}

type attachmentView struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
	Bytes    int64  `json:"bytes"`
	URL      string `json:"url"`
}

const (
	maxAttachmentCount      = 4
	maxAttachmentBytes      = 8 << 20
	maxTotalAttachmentBytes = 16 << 20
	maxMessageBodyBytes     = 24 << 20
	maxArtifactServeBytes   = 64 << 20
)

var allowedAttachmentMIME = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true,
	"application/pdf": true,
}

// decodeMessageInput accepts the JSON message shape used by the CLI and a
// bounded multipart shape used by the browser composer. Files are stored in
// the session directory before they are handed to a model; durable messages
// contain references and metadata, never file bytes.
func (s *Server) decodeMessageInput(w http.ResponseWriter, r *http.Request, root string) (string, []attachmentRef, error) {
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentType != "multipart/form-data" {
		var input messageRequest
		if !decodeJSON(w, r, &input) {
			return "", nil, fmt.Errorf("invalid message JSON")
		}
		return input.Text, nil, nil
	}
	if r.ContentLength > maxMessageBodyBytes {
		return "", nil, fmt.Errorf("message upload is too large (maximum %d MiB)", maxMessageBodyBytes>>20)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMessageBodyBytes)
	if err := r.ParseMultipartForm(maxMessageBodyBytes); err != nil {
		return "", nil, fmt.Errorf("parse message upload: %w", err)
	}
	text := r.FormValue("text")
	files := r.MultipartForm.File["attachment"]
	if len(files) > maxAttachmentCount {
		return "", nil, fmt.Errorf("attach at most %d files per message", maxAttachmentCount)
	}
	if len(files) == 0 {
		return text, nil, nil
	}
	attachmentDir := filepath.Join(root, "attachments")
	if err := os.MkdirAll(attachmentDir, 0700); err != nil {
		return "", nil, fmt.Errorf("create attachment directory: %w", err)
	}
	refs := make([]attachmentRef, 0, len(files))
	var total int64
	for _, header := range files {
		if header == nil || header.Size == 0 {
			return "", refs, fmt.Errorf("attachments must not be empty")
		}
		if header.Size > maxAttachmentBytes {
			return "", refs, fmt.Errorf("attachment %q is too large (maximum %d MiB)", header.Filename, maxAttachmentBytes>>20)
		}
		total += header.Size
		if total > maxTotalAttachmentBytes {
			return "", refs, fmt.Errorf("attachments are too large in total (maximum %d MiB)", maxTotalAttachmentBytes>>20)
		}
		file, err := header.Open()
		if err != nil {
			return "", refs, fmt.Errorf("open attachment %q: %w", header.Filename, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxAttachmentBytes+1))
		_ = file.Close()
		if readErr != nil {
			return "", refs, fmt.Errorf("read attachment %q: %w", header.Filename, readErr)
		}
		if len(data) == 0 || len(data) > maxAttachmentBytes {
			return "", refs, fmt.Errorf("attachment %q exceeds the size limit", header.Filename)
		}
		contentType := http.DetectContentType(data)
		if contentType == "application/octet-stream" && strings.EqualFold(filepath.Ext(header.Filename), ".pdf") {
			contentType = "application/pdf"
		}
		if !allowedAttachmentMIME[contentType] {
			return "", refs, fmt.Errorf("attachment %q has unsupported type %s; use PNG, JPEG, WebP, GIF, or PDF", header.Filename, contentType)
		}
		name := safeAttachmentFilename(header.Filename)
		if len(name) > 160 {
			name = name[:160]
		}
		id := fmt.Sprintf("attachment-%d", s.seq.Add(1))
		relative := filepath.Join("attachments", id)
		path := filepath.Join(root, relative)
		if err := os.WriteFile(path, data, 0600); err != nil {
			return "", refs, fmt.Errorf("store attachment %q: %w", name, err)
		}
		refs = append(refs, attachmentRef{ID: id, Filename: name, MIMEType: contentType, Bytes: int64(len(data)), Path: relative})
	}
	return text, refs, nil
}

func safeAttachmentFilename(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "attachment"
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '\r' || r == '\n' {
			return '_'
		}
		return r
	}, name)
	return name
}

func attachmentPath(root string, ref attachmentRef) (string, error) {
	if ref.ID == "" || ref.Path == "" {
		return "", fmt.Errorf("attachment reference is incomplete")
	}
	clean := filepath.Clean(ref.Path)
	if clean != filepath.Join("attachments", ref.ID) || filepath.IsAbs(ref.Path) {
		return "", fmt.Errorf("invalid attachment path")
	}
	return filepath.Join(root, clean), nil
}

func loadLLMAttachments(root string, refs []attachmentRef) ([]llmclient.Attachment, error) {
	attachments := make([]llmclient.Attachment, 0, len(refs))
	var total int64
	for _, ref := range refs {
		path, err := attachmentPath(root, ref)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read attachment %q: %w", ref.Filename, err)
		}
		if len(data) == 0 || len(data) > maxAttachmentBytes {
			return nil, fmt.Errorf("attachment %q exceeds the size limit", ref.Filename)
		}
		total += int64(len(data))
		if total > maxTotalAttachmentBytes {
			return nil, fmt.Errorf("attachments are too large in total")
		}
		attachments = append(attachments, llmclient.Attachment{Filename: ref.Filename, MIMEType: ref.MIMEType, Data: data, Detail: "auto"})
	}
	return attachments, nil
}

func messageViews(messages []intakeMessage, kind, id string) []messageView {
	views := make([]messageView, 0, len(messages))
	for _, message := range messages {
		view := messageView{Role: message.Role, Text: message.Text, At: message.At}
		for _, ref := range message.Attachments {
			view.Attachments = append(view.Attachments, attachmentView{ID: ref.ID, Filename: ref.Filename, MIMEType: ref.MIMEType, Bytes: ref.Bytes, URL: "/api/v1/" + kind + "/" + url.PathEscape(id) + "/attachments/" + url.PathEscape(ref.ID)})
		}
		if kind == "assessments" && message.Report != nil {
			ref := message.Report
			view.Attachments = append(view.Attachments, attachmentView{ID: ref.Name, Filename: ref.Label(), MIMEType: "text/markdown", Bytes: ref.Bytes, URL: "/api/v1/assessments/" + url.PathEscape(id) + "/reports/" + url.PathEscape(ref.Name)})
		}
		views = append(views, view)
	}
	return views
}

func removeAttachmentFiles(root string, refs []attachmentRef) {
	for _, ref := range refs {
		path, err := attachmentPath(root, ref)
		if err == nil {
			_ = os.Remove(path)
		}
	}
}

func (s *Server) cloneMessagesWithAttachments(messages []intakeMessage, sourceRoot, targetRoot string) ([]intakeMessage, error) {
	cloned := make([]intakeMessage, len(messages))
	copy(cloned, messages)
	if err := os.MkdirAll(filepath.Join(targetRoot, "attachments"), 0700); err != nil {
		return nil, err
	}
	for i := range cloned {
		cloned[i].Attachments = append([]attachmentRef(nil), messages[i].Attachments...)
		for j, ref := range messages[i].Attachments {
			source, err := attachmentPath(sourceRoot, ref)
			if err != nil {
				return nil, err
			}
			data, err := os.ReadFile(source)
			if err != nil {
				return nil, fmt.Errorf("copy attachment %q: %w", ref.Filename, err)
			}
			if len(data) == 0 || len(data) > maxAttachmentBytes {
				return nil, fmt.Errorf("attachment %q exceeds the size limit", ref.Filename)
			}
			id := fmt.Sprintf("attachment-%d", s.seq.Add(1))
			relative := filepath.Join("attachments", id)
			if err := os.WriteFile(filepath.Join(targetRoot, relative), data, 0600); err != nil {
				return nil, fmt.Errorf("copy attachment %q: %w", ref.Filename, err)
			}
			cloned[i].Attachments[j] = attachmentRef{ID: id, Filename: ref.Filename, MIMEType: ref.MIMEType, Bytes: int64(len(data)), Path: relative}
		}
	}
	return cloned, nil
}

type messageView struct {
	Role        string           `json:"role"`
	Text        string           `json:"text"`
	Attachments []attachmentView `json:"attachments,omitempty"`
	Images      []attachmentView `json:"images,omitempty"`
	At          time.Time        `json:"at"`
}

type intakeView struct {
	PermissionMode  approval.Mode       `json:"permission_mode"`
	ID              string              `json:"id"`
	Customer        string              `json:"customer,omitempty"`
	Title           string              `json:"title"`
	Model           string              `json:"model"`
	ModelProfile    string              `json:"model_profile,omitempty"`
	ModelConfigured bool                `json:"model_configured"`
	ModelBusy       bool                `json:"model_busy"`
	CanChangeModel  bool                `json:"can_change_model"`
	Status          string              `json:"status"`
	Messages        []messageView       `json:"messages"`
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

type modelRequest struct {
	Model   string `json:"model"`
	Profile string `json:"profile,omitempty"`
}

type intakeStartRequest struct {
	Customer   string `json:"customer"`
	ApproachID string `json:"approach_id,omitempty"`
}

type intakeCustomerRequest struct {
	Customer string `json:"customer"`
}

type run struct {
	permissionMode approval.Mode
	mu             sync.RWMutex
	id             string
	customer       string
	root           string
	client         llmclient.Client
	budget         *assessment.ModelBudget
	postRunBudget  *assessment.ModelBudget
	postRunUsage   assessment.Usage
	profileID      string
	goal           string
	scope          string
	status         string
	state          assessment.State
	started        bool
	deleted        bool
	runCtx         context.Context
	cancel         context.CancelFunc
	failCancel     context.CancelCauseFunc
	done           chan struct{}
	chatBusy       bool
	resume         bool
	updatedAt      time.Time
	persistMu      sync.Mutex
	persistErr     error

	sequence  uint64
	events    []eventRecord
	messages  []intakeMessage
	approvals map[string]*pendingApproval
	questions map[string]*pendingQuestion
	workers   map[string]workerView
	plan      *pendingPlan
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

type pendingPlan struct {
	ID     string
	plan   assessment.Decision
	result chan assessment.PlanReview
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

type planReviewRequest struct {
	Decision        string   `json:"decision"`
	ApprovedTaskIDs []string `json:"approved_task_ids"`
}

type assessmentView struct {
	PermissionMode   approval.Mode         `json:"permission_mode"`
	Customer         string                `json:"customer"`
	ID               string                `json:"id"`
	Goal             string                `json:"goal"`
	Scope            string                `json:"scope"`
	Approach         *assessment.Approach  `json:"approach,omitempty"`
	Status           string                `json:"status"`
	Conclusion       string                `json:"conclusion,omitempty"`
	Model            string                `json:"model"`
	ModelProfile     string                `json:"model_profile,omitempty"`
	ModelBusy        bool                  `json:"model_busy"`
	CanChangeModel   bool                  `json:"can_change_model"`
	Resumable        bool                  `json:"resumable"`
	UpdatedAt        time.Time             `json:"updated_at,omitempty"`
	Error            string                `json:"error,omitempty"`
	StartedAt        time.Time             `json:"started_at,omitempty"`
	FinishedAt       time.Time             `json:"finished_at,omitempty"`
	Usage            assessment.Usage      `json:"usage"`
	PostRunUsage     assessment.Usage      `json:"post_run_usage"`
	ContextWindow    contextWindowView     `json:"context_window"`
	Plans            int                   `json:"plans"`
	PlanTimeline     []coordinatorPlanView `json:"plan_timeline,omitempty"`
	Workers          []workerView          `json:"workers"`
	Findings         []assessment.Finding  `json:"findings"`
	Limits           assessment.Limits     `json:"limits"`
	Results          []assessment.Result   `json:"results"`
	Events           []eventRecord         `json:"events"`
	PendingApprovals []approvalView        `json:"pending_approvals"`
	PendingQuestions []questionView        `json:"pending_questions"`
	PendingPlan      *planApprovalView     `json:"pending_plan,omitempty"`
	Messages         []messageView         `json:"messages"`
	ReportURL        string                `json:"report_url,omitempty"`
	ReportReady      bool                  `json:"report_ready,omitempty"`
}

type planApprovalView struct {
	ID      string            `json:"id"`
	Phase   string            `json:"phase"`
	Summary string            `json:"summary"`
	Tasks   []assessment.Task `json:"tasks"`
}

// contextWindowView reports the largest active worker request, or the latest
// worker request after completion, against the application input ceiling. These are bytes of
// message text, not provider token counts.
type contextWindowView struct {
	UsedBytes      int    `json:"used_bytes"`
	LimitBytes     int    `json:"limit_bytes"`
	RemainingBytes int    `json:"remaining_bytes"`
	Percent        int    `json:"percent"`
	WorkerID       string `json:"worker_id,omitempty"`
	Active         bool   `json:"active"`
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
	Summary  string `json:"summary"`
	Target   string `json:"target"`
	Risk     string `json:"risk"`
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
	if len(config.Profiles) > 0 {
		if config.DefaultProfile == "" {
			config.DefaultProfile = config.Profiles[0].ID
		}
		for _, profile := range config.Profiles {
			if profile.ID == config.DefaultProfile {
				config.LLM = profile.client()
				break
			}
		}
	}
	if config.SessionsRoot == "" {
		config.SessionsRoot = filepath.Join(config.RepoRoot, "sessions", "web")
	}
	if config.Limits == (assessment.Limits{}) {
		config.Limits = assessment.DefaultLimits()
	}
	server := &Server{config: config, runs: make(map[string]*run), intakes: make(map[string]*intakeRun)}
	server.loadErr = server.restoreSessions()
	return server
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
	if r.URL.Path == "/analysis" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(analysisHTML))
		return
	}
	if r.URL.Path == "/context" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if !localDebugRequest(r) {
			http.Error(w, "context debugger requires local loopback", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(contextHTML))
		return
	}
	if r.URL.Path == "/api/v1/healthz" {
		s.health(w, r)
		return
	}
	if r.URL.Path == "/api/v1/models" {
		s.models(w, r)
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
		"status":             "ok",
		"model_configured":   strings.TrimSpace(s.config.LLM.BaseURL) != "" && strings.TrimSpace(s.config.LLM.Model) != "",
		"session_load_error": errorText(s.loadErr),
		"loopback_warning":   "This preview has no authentication; bind it to loopback and use only an authorized lab.",
	})
}

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

func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request, root string, messages []intakeMessage, id string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	var ref attachmentRef
	found := false
	for _, message := range messages {
		for _, candidate := range message.Attachments {
			if candidate.ID == id {
				ref, found = candidate, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	path, err := attachmentPath(root, ref)
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", ref.MIMEType)
	w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(safeAttachmentFilename(ref.Filename), `"`, "")+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, ref.Filename, time.Time{}, file)
}

func (s *Server) serveAssessmentArtifact(w http.ResponseWriter, r *http.Request, current *run) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	wanted := filepath.Clean(strings.TrimSpace(r.URL.Query().Get("path")))
	if wanted == "." || !filepath.IsAbs(wanted) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	current.mu.RLock()
	root := current.root
	found := false
	for _, result := range current.state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.ArtifactRefs {
				if filepath.Clean(ref) == wanted {
					found = true
					break
				}
			}
		}
	}
	for _, worker := range current.workers {
		for _, evidence := range worker.Evidence {
			for _, ref := range evidence.ArtifactRefs {
				if filepath.Clean(ref) == wanted {
					found = true
				}
			}
		}
		for _, ref := range worker.ExpectedArtifacts {
			if filepath.Clean(ref) == wanted && resolvedWithin(filepath.Join(root, "tasks", worker.ID, "work"), wanted) {
				found = true
			}
		}
	}
	current.mu.RUnlock()
	if !found || !resolvedWithin(root, wanted) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	file, err := os.Open(wanted)
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxArtifactServeBytes {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(wanted))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(filepath.Base(wanted), `"`, "")+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(wanted), info.ModTime(), file)
}

func pathWithin(root, path string) bool {
	root, rootErr := filepath.Abs(root)
	path, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
		Policy:      "These intake observations inspect only this local host and workspace. Keep them minimal; do not access credentials, contact the assessment target, or mutate files.",
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
		pending = &intakeApprovalView{ID: r.pendingTool.ID, Tool: r.pendingTool.Tool}
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
	pending := &intakeApproval{ID: id, Tool: tool, Result: make(chan approval.Decision, 1)}
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
	ID       string            `json:"id"`
	Status   string            `json:"status"`
	Sessions []assessmentView  `json:"sessions"`
	Drafts   []intakeIndexView `json:"drafts,omitempty"`
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
	drafts := make([]intakeIndexView, 0, len(s.intakes))
	for _, current := range s.intakes {
		view := current.view()
		if view.AssessmentID != "" {
			continue
		}
		draft := intakeIndexView{ID: view.ID, Customer: view.Customer, Title: view.Title, Status: view.Status, Model: view.Model}
		if view.Customer != "" {
			ids[view.Customer] = struct{}{}
		}
		drafts = append(drafts, draft)
	}
	s.mu.RUnlock()
	views := make([]customerIndexView, 0, len(ids))
	for id := range ids {
		view := s.customerView(id)
		views = append(views, customerIndexView{ID: view.ID, Status: view.Status, Sessions: view.Sessions})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	intakes := make([]intakeIndexView, 0, len(drafts))
	for _, draft := range drafts {
		if draft.Customer == "" {
			intakes = append(intakes, draft)
			continue
		}
		for i := range views {
			if views[i].ID == draft.Customer {
				views[i].Drafts = append(views[i].Drafts, draft)
				break
			}
		}
	}
	sort.Slice(intakes, func(i, j int) bool { return intakes[i].ID < intakes[j].ID })
	for i := range views {
		sort.Slice(views[i].Drafts, func(a, b int) bool { return views[i].Drafts[a].ID < views[i].Drafts[b].ID })
	}
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
		for _, finding := range assessment.CurrentFindings(current.state.Plans) {
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
	if current.deleted {
		current.mu.Unlock()
		return fmt.Errorf("session has been deleted")
	}
	if current.started {
		current.mu.Unlock()
		return fmt.Errorf("assessment has already been started")
	}
	if strings.TrimSpace(current.client.BaseURL) == "" || strings.TrimSpace(current.client.Model) == "" {
		current.mu.Unlock()
		return fmt.Errorf("model endpoint and model are required; configure the web server first")
	}
	if current.state.Status == "completed" || current.state.Status == "completed_with_gaps" {
		current.mu.Unlock()
		return fmt.Errorf("assessment has already been finalized")
	}
	ctx, cancelCause := context.WithCancelCause(context.Background())
	cancel := func() { cancelCause(context.Canceled) }
	previousStatus, previousUpdatedAt, previousDone, previousBudget := current.status, current.updatedAt, current.done, current.budget
	limits := current.state.Limits
	if limits == (assessment.Limits{}) {
		limits = s.config.Limits
	}
	if limits == (assessment.Limits{}) {
		limits = assessment.DefaultLimits()
	}
	current.budget = assessment.NewModelBudget(limits.ModelCalls, current.state.Usage)
	current.started, current.status, current.runCtx, current.cancel, current.failCancel, current.done, current.updatedAt = true, "starting", ctx, cancel, cancelCause, make(chan struct{}), time.Now().UTC()
	current.mu.Unlock()
	if err := current.persist(); err != nil {
		cancel()
		current.mu.Lock()
		current.started, current.status, current.runCtx, current.cancel, current.failCancel, current.done, current.updatedAt, current.budget = false, previousStatus, nil, nil, nil, previousDone, previousUpdatedAt, previousBudget
		current.mu.Unlock()
		return fmt.Errorf("save assessment session: %w", err)
	}
	current.emit(assessment.Event{Kind: "assessment_started", Message: "Assessment accepted; the coordinator is preparing its first plan."})
	go s.runAssessment(ctx, current)
	return nil
}

func (s *Server) runAssessment(ctx context.Context, current *run) {
	current.mu.RLock()
	client := current.client
	budget := current.budget
	resume := current.resume
	initial := current.state
	current.mu.RUnlock()
	runner := assessment.Coordinator{
		LLM:      client,
		Budget:   budget,
		Frame:    s.config.Frame,
		Limits:   s.config.Limits,
		Approach: initial.Approach,
		Emit:     current.emit,
		Approver: func(task assessment.Task) approval.Approver {
			return &runApprover{run: current, taskID: task.ID}
		},
		AskUser: func(ctx context.Context, task assessment.Task, question string) (string, error) {
			return current.ask(ctx, task.ID, question)
		},
		PlanApproval: func(ctx context.Context, plan assessment.Decision) (assessment.PlanReview, error) {
			return current.reviewPlanWait(ctx, plan)
		},
		Conversation: current.conversation,
		Snapshot:     current.snapshot,
	}
	var state assessment.State
	var err error
	if resume && initial.Goal != "" {
		state, err = runner.RunState(ctx, current.root, initial)
	} else {
		state, err = runner.Run(ctx, current.root, current.goal, current.scope)
	}
	current.mu.Lock()
	if budget != nil {
		state.Usage = budget.Usage()
	}
	current.state = state
	if err != nil {
		current.status = state.Status
		if current.status == "" {
			current.status = "incomplete"
		}
	} else {
		current.status = state.Status
	}
	current.resume = true
	done := current.done
	current.runCtx, current.cancel, current.failCancel = nil, nil, nil
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	_ = current.persistOrStop()
	current.emit(assessment.Event{Kind: "assessment_finished", Message: current.status})
	close(done)
	current.mu.Lock()
	if current.done == done {
		current.started = false
	}
	current.mu.Unlock()
}

func (r *run) snapshot(state assessment.State) {
	r.mu.Lock()
	if r.budget != nil {
		state.Usage = r.budget.Usage()
	}
	r.state = state
	r.status = state.Status
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	_ = r.persistOrStop()
}

func (r *run) conversation() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]string, 0, len(r.messages))
	for _, message := range r.messages {
		values = append(values, message.Role+": "+message.Text+attachmentSummary(message.Attachments))
	}
	return values
}

func (r *run) emit(event assessment.Event) {
	r.mu.Lock()
	r.updateWorker(event)
	r.sequence++
	r.events = append(r.events, eventRecord{Sequence: r.sequence, At: time.Now().UTC(), Event: event})
	r.updatedAt = time.Now().UTC()
	if len(r.events) > 200 {
		r.events = r.events[len(r.events)-200:]
	}
	r.mu.Unlock()
	_ = r.persistOrStop()
}

func (r *run) persistOrStop() error {
	err := r.persist()
	if err == nil {
		return nil
	}
	r.mu.Lock()
	if r.persistErr == nil {
		r.persistErr = fmt.Errorf("save web session: %w", err)
	}
	cancel := r.failCancel
	r.mu.Unlock()
	if cancel != nil {
		cancel(fmt.Errorf("web session persistence failed: %w", err))
	}
	return err
}

func (r *run) stop() error {
	r.mu.RLock()
	deleted, cancel, started, status := r.deleted, r.cancel, r.started, r.status
	r.mu.RUnlock()
	if deleted {
		return fmt.Errorf("session has been deleted")
	}
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

func (s *Server) message(ctx context.Context, r *run, text string, refs []attachmentRef) error {
	text = strings.TrimSpace(text)
	if text == "" && len(refs) > 0 {
		text = "Please inspect the attached artifact(s)."
	}
	if text == "" {
		return fmt.Errorf("message is required")
	}
	attachments, err := loadLLMAttachments(r.root, refs)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.deleted {
		r.mu.Unlock()
		return fmt.Errorf("session has been deleted")
	}
	postRun := !r.started && (r.status == "completed" || r.status == "completed_with_gaps" || r.status == "incomplete" || r.status == "aborted")
	if !postRun && (!r.started || (r.status != "running" && r.status != "starting")) {
		r.mu.Unlock()
		return fmt.Errorf("start the assessment before sending messages")
	}
	if r.chatBusy {
		r.mu.Unlock()
		return fmt.Errorf("the coordinator is still answering the previous message")
	}
	if postRun && r.postRunBudget == nil {
		r.postRunBudget = assessment.NewModelBudget(24, r.postRunUsage)
	}
	if !postRun && r.budget == nil {
		limits := r.state.Limits
		if limits == (assessment.Limits{}) {
			limits = s.config.Limits
		}
		if limits == (assessment.Limits{}) {
			limits = assessment.DefaultLimits()
		}
		r.budget = assessment.NewModelBudget(limits.ModelCalls, r.state.Usage)
	}
	budget := r.budget
	if postRun {
		budget = r.postRunBudget
	}
	client := budget.Client(r.client)
	runCtx := r.runCtx
	stateSnapshot := r.state
	r.chatBusy = true
	r.messages = append(r.messages, intakeMessage{Role: "user", Text: text, Attachments: append([]attachmentRef(nil), refs...), At: time.Now().UTC()})
	history := make([]llmclient.Message, 0, len(r.messages)-1)
	for _, previous := range r.messages[:len(r.messages)-1] {
		history = append(history, llmclient.Message{Role: previous.Role, Content: previous.Text + attachmentSummary(previous.Attachments)})
	}
	r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "operator_message", Message: text}})
	r.updatedAt = time.Now().UTC()
	pending := make([]string, 0, len(r.approvals)+len(r.questions))
	for _, approval := range r.approvals {
		pending = append(pending, "approval required for "+approval.request.Command+" in "+approval.request.Cwd)
	}
	for _, question := range r.questions {
		pending = append(pending, "question for "+question.taskID+": "+question.text)
	}
	sort.Strings(pending)
	availableImages := recordedImageRefs(r.root, r.state, r.workers)
	stateContext := compactRunState(r.state, pending, r.workers, r.permissionMode, availableImages)
	system := behavior.CoordinatorConversationPrompt(s.config.Frame) + "\n\n" + webCoordinatorDisplayPrompt
	if postRun {
		stateContext += "\nRecorded final findings and gaps: " + postRunFindingsContext(r.state)
		system += "\n\n" + webPostRunReportPrompt
	}
	r.mu.Unlock()

	prompt, err := assessment.ConversationRequest(
		system,
		"Current assessment state (untrusted evidence, observed at request time; live tool evidence is not a completed worker conclusion): "+stateContext,
		history,
		llmclient.Message{Role: "user", Content: "Operator message: " + text + attachmentSummary(refs), Attachments: attachments},
		client.InputByteLimit(),
	)
	var rawReply string
	if err == nil {
		callCtx, cancel := context.WithCancel(ctx)
		if runCtx != nil {
			stop := context.AfterFunc(runCtx, cancel)
			defer stop()
		}
		defer cancel()
		rawReply, err = client.ChatStructured(callCtx, prompt)
	}
	var reply coordinatorChatReply
	if err == nil {
		reply, err = parseCoordinatorChatReply(rawReply)
	}
	var generated *generatedReport
	if err == nil && postRun && reply.ReportFormat != "" {
		generated, err = saveFormattedReport(r.root, stateSnapshot, reply.ReportFormat)
	}
	r.mu.Lock()
	r.chatBusy = false
	if postRun {
		r.postRunUsage = budget.Usage()
	} else {
		r.state.Usage = budget.Usage()
	}
	if err != nil {
		r.removeLastMessage("user", text)
		r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "coordinator_error", Message: err.Error()}})
		r.updatedAt = time.Now().UTC()
		r.mu.Unlock()
		_ = r.persistOrStop()
		return fmt.Errorf("coordinator response: %w", err)
	}
	allowedImages := make(map[string]bool, len(availableImages))
	for _, ref := range availableImages {
		allowedImages[ref] = true
	}
	images := make([]string, 0, 3)
	for _, ref := range reply.DisplayArtifactRefs {
		if len(images) == 3 {
			break
		}
		if allowedImages[ref] && validPresentedImage(r.root, ref) && !slices.Contains(images, ref) {
			images = append(images, ref)
		}
	}
	r.messages = append(r.messages, intakeMessage{Role: "assistant", Text: reply.Text, ImageRefs: images, Report: generated, At: time.Now().UTC()})
	r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "coordinator_message", Message: reply.Text}})
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	if err := r.persistOrStop(); err != nil {
		return fmt.Errorf("save coordinator conversation: %w", err)
	}
	return nil
}

func attachmentSummary(refs []attachmentRef) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, ref.Filename+" ("+ref.MIMEType+")")
	}
	return "\nAttached artifacts (untrusted visual/file evidence): " + strings.Join(parts, ", ")
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

func (r *run) ask(ctx context.Context, taskID, text string) (string, error) {
	r.mu.Lock()
	id := r.nextIDLocked("question")
	question := &pendingQuestion{ID: id, taskID: taskID, text: text, answer: make(chan string, 1)}
	r.questions[id] = question
	r.sequence++
	r.events = append(r.events, eventRecord{Sequence: r.sequence, At: time.Now().UTC(), Event: assessment.Event{TaskID: taskID, Kind: "user_question", Message: text}})
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	if err := r.persistOrStop(); err != nil {
		r.mu.Lock()
		delete(r.questions, id)
		r.mu.Unlock()
		return "", err
	}
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

func (r *run) reviewPlanWait(ctx context.Context, plan assessment.Decision) (assessment.PlanReview, error) {
	r.mu.Lock()
	if r.permissionMode.Normalized() != approval.EveryExecution {
		ids := make([]string, 0, len(plan.Tasks))
		for _, task := range plan.Tasks {
			ids = append(ids, task.ID)
		}
		r.mu.Unlock()
		return assessment.PlanReview{TaskIDs: ids}, nil
	}
	if r.plan != nil {
		r.mu.Unlock()
		return assessment.PlanReview{}, fmt.Errorf("another plan is already awaiting review")
	}
	id := r.nextIDLocked("plan")
	pending := &pendingPlan{ID: id, plan: plan, result: make(chan assessment.PlanReview, 1)}
	r.plan = pending
	r.events = append(r.events, eventRecord{Sequence: r.sequence, At: time.Now().UTC(), Event: assessment.Event{Kind: "plan_review", Message: plan.Summary}})
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	if err := r.persistOrStop(); err != nil {
		r.mu.Lock()
		if r.plan == pending {
			r.plan = nil
		}
		r.mu.Unlock()
		return assessment.PlanReview{}, err
	}
	select {
	case review := <-pending.result:
		return review, nil
	case <-ctx.Done():
		r.mu.Lock()
		if r.plan == pending {
			r.plan = nil
		}
		r.mu.Unlock()
		return assessment.PlanReview{}, ctx.Err()
	}
}

func (r *run) reviewPlan(id string, input planReviewRequest) error {
	r.mu.Lock()
	pending := r.plan
	if pending == nil || pending.ID != id {
		r.mu.Unlock()
		return fmt.Errorf("plan is no longer awaiting review")
	}
	decision := strings.ToLower(strings.TrimSpace(input.Decision))
	if decision == "deny" || decision == "denied" || decision == "reject" || decision == "rejected" {
		r.plan = nil
		r.updatedAt = time.Now().UTC()
		r.mu.Unlock()
		if err := r.persistOrStop(); err != nil {
			return err
		}
		pending.result <- assessment.PlanReview{}
		return nil
	}
	if decision != "" && decision != "approve" && decision != "approved" && decision != "approved_once" {
		r.mu.Unlock()
		return fmt.Errorf("unsupported plan decision")
	}
	ids := append([]string(nil), input.ApprovedTaskIDs...)
	if len(ids) == 0 {
		for _, task := range pending.plan.Tasks {
			ids = append(ids, task.ID)
		}
	}
	known := make(map[string]bool, len(pending.plan.Tasks))
	for _, task := range pending.plan.Tasks {
		known[task.ID] = true
	}
	seen := make(map[string]bool, len(ids))
	for _, taskID := range ids {
		if !known[taskID] || seen[taskID] {
			r.mu.Unlock()
			return fmt.Errorf("plan selection contains an unknown or duplicate task")
		}
		seen[taskID] = true
	}
	r.plan = nil
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	if err := r.persistOrStop(); err != nil {
		return err
	}
	pending.result <- assessment.PlanReview{TaskIDs: ids}
	return nil
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
	r.mu.Lock()
	pending := r.approvals[id]
	if pending == nil {
		r.mu.Unlock()
		return fmt.Errorf("approval is no longer pending")
	}
	delete(r.approvals, id)
	pending.result <- value
	r.mu.Unlock()
	return nil
}

func (r *run) view(after string) assessmentView {
	r.mu.RLock()
	defer r.mu.RUnlock()
	model := r.client.Model
	if strings.TrimSpace(model) == "" {
		model = r.state.Model
	}
	view := assessmentView{Customer: r.customer, ID: r.id, Goal: r.goal, Scope: r.scope, Approach: r.state.Approach, Status: r.status, Model: model, ModelProfile: r.profileID, ModelBusy: r.chatBusy || r.started, CanChangeModel: !r.chatBusy && !r.started && r.status == "draft", Resumable: !r.started && r.status != "draft" && r.status != "completed" && r.status != "completed_with_gaps", UpdatedAt: r.updatedAt, Error: r.state.Error, StartedAt: r.state.StartedAt, FinishedAt: r.state.FinishedAt, Usage: r.state.Usage, PostRunUsage: r.postRunUsage, Plans: len(r.state.Plans), Results: append([]assessment.Result(nil), r.state.Results...), Messages: messageViews(r.messages, "assessments", r.id), ReportURL: "/api/v1/assessments/" + r.id + "/report"}
	if r.persistErr != nil {
		view.Error = r.persistErr.Error()
	}
	if r.budget != nil {
		view.Usage = r.budget.Usage()
	}
	for i, message := range r.messages {
		for _, ref := range message.ImageRefs {
			if validPresentedImage(r.root, ref) {
				view.Messages[i].Images = append(view.Messages[i].Images, presentedImageView(r.id, ref))
			}
		}
	}
	if r.status != "draft" && r.status != "running" && r.status != "starting" {
		if info, err := os.Stat(filepath.Join(r.root, "report.md")); err == nil && info.Mode().IsRegular() {
			view.ReportReady = true
		}
	}
	view.Limits = r.state.Limits
	view.PermissionMode = r.permissionMode.Normalized()
	view.Conclusion = assessmentConclusion(r.state)
	for _, worker := range r.workers {
		worker.Evidence = append([]assessment.EvidenceView(nil), worker.Evidence...)
		view.Workers = append(view.Workers, worker)
	}
	sort.Slice(view.Workers, func(i, j int) bool { return view.Workers[i].ID < view.Workers[j].ID })
	view.ContextWindow = aggregateContextWindow(r.state, view.Workers)
	for i := range view.Workers {
		for j := range view.Workers[i].Evidence {
			view.Workers[i].Evidence[j] = decorateEvidence(view.Workers[i].Evidence[j], r.id)
		}
	}
	view.Findings = assessment.CurrentFindings(r.state.Plans)
	view.PlanTimeline = coordinatorPlans(r.state, r.workers)
	if n, err := strconv.ParseUint(strings.TrimSpace(after), 10, 64); err == nil {
		for _, event := range r.events {
			if event.Sequence > n {
				view.Events = append(view.Events, event)
			}
		}
	} else {
		view.Events = append([]eventRecord(nil), r.events...)
	}
	for i := range view.Events {
		if view.Events[i].Event.Evidence != nil {
			decorated := decorateEvidence(*view.Events[i].Event.Evidence, r.id)
			view.Events[i].Event.Evidence = &decorated
		}
	}
	for _, pending := range r.approvals {
		view.PendingApprovals = append(view.PendingApprovals, approvalView{ID: pending.ID, TaskID: pending.taskID, Command: pending.request.Command, UseShell: pending.request.UseShell, Cwd: pending.request.Cwd, Impact: pending.request.Impact, Summary: pending.request.Summary, Target: pending.request.Target, Risk: pending.request.Risk})
	}
	for _, question := range r.questions {
		view.PendingQuestions = append(view.PendingQuestions, questionView{ID: question.ID, TaskID: question.taskID, Text: question.text})
	}
	if r.plan != nil {
		phase := r.plan.plan.Phase
		if phase == "" {
			phase = "assessment"
		}
		view.PendingPlan = &planApprovalView{ID: r.plan.ID, Phase: phase, Summary: r.plan.plan.Summary, Tasks: append([]assessment.Task(nil), r.plan.plan.Tasks...)}
		pending := coordinatorPlanView{Round: len(r.state.Plans) + 1, Phase: phase, Summary: r.plan.plan.Summary, PlainSummary: r.plan.plan.PlainSummary, Signal: "review", Status: "review"}
		for _, task := range r.plan.plan.Tasks {
			pending.Tasks = append(pending.Tasks, coordinatorTaskView{ID: task.ID, Goal: task.Goal, DoneWhen: task.DoneWhen, StrategyHints: task.StrategyHints, Status: "review"})
		}
		view.PlanTimeline = append(view.PlanTimeline, pending)
	}
	sort.Slice(view.PendingApprovals, func(i, j int) bool { return view.PendingApprovals[i].ID < view.PendingApprovals[j].ID })
	sort.Slice(view.PendingQuestions, func(i, j int) bool { return view.PendingQuestions[i].ID < view.PendingQuestions[j].ID })
	return view
}

func decorateEvidence(evidence assessment.EvidenceView, assessmentID string) assessment.EvidenceView {
	evidence.ArtifactURLs = nil
	for _, ref := range evidence.ArtifactRefs {
		evidence.ArtifactURLs = append(evidence.ArtifactURLs, "/api/v1/assessments/"+url.PathEscape(assessmentID)+"/artifact?path="+url.QueryEscape(ref))
	}
	return evidence
}

func validPresentedImage(root, ref string) bool {
	if imageMIME(ref) == "" || !filepath.IsAbs(ref) || !resolvedWithin(root, ref) {
		return false
	}
	info, err := os.Stat(ref)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= maxAttachmentBytes
}

func presentedImageView(assessmentID, ref string) attachmentView {
	info, _ := os.Stat(ref)
	return attachmentView{
		Filename: filepath.Base(ref), MIMEType: imageMIME(ref), Bytes: info.Size(),
		URL: "/api/v1/assessments/" + url.PathEscape(assessmentID) + "/artifact?path=" + url.QueryEscape(ref),
	}
}

func aggregateContextWindow(state assessment.State, workers []workerView) contextWindowView {
	limit := state.MaxInputBytes
	var selected *workerView
	for _, worker := range workers {
		if limit == 0 && worker.ContextLimitBytes > limit {
			limit = worker.ContextLimitBytes
		}
		if worker.ContextUsedBytes == 0 || worker.Phase == "done" || worker.Phase == "task_completed" || worker.Phase == "failed" || worker.Phase == "task_failed" || worker.Phase == "blocked" || worker.Phase == "task_blocked" || worker.Phase == "aborted" {
			continue
		}
		if selected == nil || worker.ContextUsedBytes > selected.ContextUsedBytes {
			copy := worker
			selected = &copy
		}
	}
	active := selected != nil
	if selected == nil {
		for _, worker := range workers {
			if worker.ContextUsedBytes == 0 {
				continue
			}
			if selected == nil || worker.UpdatedAt.After(selected.UpdatedAt) {
				copy := worker
				selected = &copy
			}
		}
	}
	used, workerID := 0, ""
	if selected != nil {
		used, workerID = selected.ContextUsedBytes, selected.ID
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	percent := 0
	if limit > 0 {
		percent = used * 100 / limit
		if percent > 100 {
			percent = 100
		}
	}
	return contextWindowView{UsedBytes: used, LimitBytes: limit, RemainingBytes: remaining, Percent: percent, WorkerID: workerID, Active: active}
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
	if !a.run.permissionMode.RequiresApproval(request) {
		a.run.mu.Unlock()
		return approval.DecisionApproveSession, ctx.Err()
	}
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
