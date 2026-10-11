// Package webapp provides the browser adapter for the shared assessment runtime.
// It owns HTTP state and presentation only; the assessment coordinator remains
// the owner of planning, approvals, execution, evidence, and reporting.
package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
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
	login   *subscription.LoginManager
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
			mime := "text/markdown"
			if ref.Output == pdfOutput {
				mime = "application/pdf"
			}
			view.Attachments = append(view.Attachments, attachmentView{ID: ref.Name, Filename: ref.Label(), MIMEType: mime, Bytes: ref.Bytes, URL: "/api/v1/assessments/" + url.PathEscape(id) + "/reports/" + url.PathEscape(ref.Name)})
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
	ID      string
	Tool    intake.ToolCall
	Summary string
	Impact  string
	Target  string
	Result  chan approval.Decision
}

type intakeApprovalView struct {
	ID      string          `json:"id"`
	Tool    intake.ToolCall `json:"tool"`
	Summary string          `json:"summary,omitempty"`
	Impact  string          `json:"impact,omitempty"`
	Target  string          `json:"target,omitempty"`
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
	permissionMode     approval.Mode
	mu                 sync.RWMutex
	id                 string
	customer           string
	root               string
	client             llmclient.Client
	budget             *assessment.ModelBudget
	postRunBudget      *assessment.ModelBudget
	postRunUsage       assessment.Usage
	coordinatorContext contextWindowView
	profileID          string
	goal               string
	scope              string
	status             string
	state              assessment.State
	started            bool
	deleted            bool
	runCtx             context.Context
	cancel             context.CancelFunc
	failCancel         context.CancelCauseFunc
	done               chan struct{}
	chatBusy           bool
	resume             bool
	updatedAt          time.Time
	persistMu          sync.Mutex
	persistErr         error

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
	PermissionMode   approval.Mode              `json:"permission_mode"`
	Customer         string                     `json:"customer"`
	ID               string                     `json:"id"`
	Goal             string                     `json:"goal"`
	Scope            string                     `json:"scope"`
	Approach         *assessment.Approach       `json:"approach,omitempty"`
	Status           string                     `json:"status"`
	Conclusion       string                     `json:"conclusion,omitempty"`
	ConclusionDetail string                     `json:"conclusion_detail,omitempty"`
	Model            string                     `json:"model"`
	ModelProfile     string                     `json:"model_profile,omitempty"`
	ModelBusy        bool                       `json:"model_busy"`
	CanChangeModel   bool                       `json:"can_change_model"`
	Resumable        bool                       `json:"resumable"`
	UpdatedAt        time.Time                  `json:"updated_at,omitempty"`
	Error            string                     `json:"error,omitempty"`
	StartedAt        time.Time                  `json:"started_at,omitempty"`
	FinishedAt       time.Time                  `json:"finished_at,omitempty"`
	Usage            assessment.Usage           `json:"usage"`
	PostRunUsage     assessment.Usage           `json:"post_run_usage"`
	ContextWindow    contextWindowView          `json:"context_window"`
	ContextWindows   []contextWindowView        `json:"context_windows,omitempty"`
	ReportReadiness  assessment.ReportReadiness `json:"report_readiness"`
	Plans            int                        `json:"plans"`
	PlanTimeline     []coordinatorPlanView      `json:"plan_timeline,omitempty"`
	Workers          []workerView               `json:"workers"`
	Findings         []assessment.Finding       `json:"findings"`
	Artifacts        []artifactView             `json:"artifacts"`
	Limits           assessment.Limits          `json:"limits"`
	Results          []assessment.Result        `json:"results"`
	Events           []eventRecord              `json:"events"`
	PendingApprovals []approvalView             `json:"pending_approvals"`
	PendingQuestions []questionView             `json:"pending_questions"`
	PendingPlan      *planApprovalView          `json:"pending_plan,omitempty"`
	Messages         []messageView              `json:"messages"`
	ReportURL        string                     `json:"report_url,omitempty"`
	ReportReady      bool                       `json:"report_ready,omitempty"`
	EvidenceIndexURL string                     `json:"evidence_index_url,omitempty"`
}

type planApprovalView struct {
	ID           string            `json:"id"`
	Phase        string            `json:"phase"`
	Summary      string            `json:"summary"`
	PlainSummary string            `json:"plain_summary,omitempty"`
	Tasks        []assessment.Task `json:"tasks"`
}

// Each coordinator or worker has its own request packet. These are bytes of
// message text against the application input ceiling, not provider token counts.
type contextWindowView struct {
	UsedBytes      int    `json:"used_bytes"`
	LimitBytes     int    `json:"limit_bytes"`
	RemainingBytes int    `json:"remaining_bytes"`
	Percent        int    `json:"percent"`
	AgentID        string `json:"agent_id,omitempty"`
	Role           string `json:"role,omitempty"`
	Status         string `json:"status,omitempty"`
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
	Summary      string `json:"summary"`
	Target       string `json:"target"`
	Risk         string `json:"risk"`
	ReviewReason string `json:"review_reason,omitempty"`
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	Command      string `json:"command"`
	UseShell     bool   `json:"use_shell"`
	Cwd          string `json:"cwd"`
	Impact       string `json:"impact,omitempty"`
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
	server := &Server{config: config, runs: make(map[string]*run), intakes: make(map[string]*intakeRun), login: &subscription.LoginManager{Home: codexHome()}}
	server.loadErr = server.restoreSessions()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Browser pages on another origin must not be able to trigger local
	// assessment actions through a form or fetch request. Non-browser local
	// clients without Origin/Fetch metadata remain usable.
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
		http.Error(w, "cross-origin action denied", http.StatusForbidden)
		return
	}
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
	if strings.HasPrefix(r.URL.Path, "/api/v1/subscription/login/") {
		s.subscriptionLogin(w, r)
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
		"loopback_warning":   "This preview has no authentication; bind it to loopback until access controls are implemented.",
	})
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
