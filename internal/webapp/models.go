package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

type modelOption struct {
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Current  bool   `json:"current"`
}

// ModelProfile binds a visible choice to its endpoint and request limits.
// The browser receives labels and IDs, never token paths or endpoint secrets.
type ModelProfile struct {
	ID                    string `json:"id"`
	Label                 string `json:"label"`
	Provider              string `json:"provider"`
	BaseURL               string `json:"base_url"`
	Model                 string `json:"model"`
	TokenFile             string `json:"token_file,omitempty"`
	ReasoningEffort       string `json:"reasoning_effort,omitempty"`
	StructuredJSON        bool   `json:"structured_json,omitempty"`
	MaxOutputTokens       int    `json:"max_output_tokens,omitempty"`
	MaxInputBytes         int    `json:"max_input_bytes,omitempty"`
	RequestTimeoutSeconds int    `json:"request_timeout_seconds,omitempty"`
}

type ModelProfilesFile struct {
	Default  string         `json:"default"`
	Profiles []ModelProfile `json:"profiles"`
}

func LoadModelProfiles(path string) (ModelProfilesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ModelProfilesFile{}, err
	}
	var file ModelProfilesFile
	if err := json.Unmarshal(data, &file); err != nil {
		return ModelProfilesFile{}, fmt.Errorf("decode model profiles: %w", err)
	}
	if len(file.Profiles) == 0 || len(file.Profiles) > 16 {
		return ModelProfilesFile{}, fmt.Errorf("configure between one and sixteen model profiles")
	}
	seen := map[string]bool{}
	for _, profile := range file.Profiles {
		if !validSessionID(profile.ID) || profile.ID == "" || seen[profile.ID] || strings.TrimSpace(profile.Label) == "" || strings.TrimSpace(profile.Model) == "" {
			return ModelProfilesFile{}, fmt.Errorf("model profiles need unique IDs, labels, and model IDs")
		}
		seen[profile.ID] = true
		u, err := url.Parse(profile.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return ModelProfilesFile{}, fmt.Errorf("profile %s needs an HTTP(S) model endpoint without embedded credentials", profile.ID)
		}
		if profile.Provider != "local" && profile.Provider != "subscription" {
			return ModelProfilesFile{}, fmt.Errorf("profile %s needs provider local or subscription", profile.ID)
		}
		if profile.Provider == "subscription" && profile.TokenFile == "" {
			return ModelProfilesFile{}, fmt.Errorf("subscription profile %s needs a local token_file", profile.ID)
		}
		if profile.MaxInputBytes < 0 || profile.MaxOutputTokens < 0 || profile.RequestTimeoutSeconds < 0 {
			return ModelProfilesFile{}, fmt.Errorf("profile %s has a negative request limit", profile.ID)
		}
	}
	if file.Default == "" {
		file.Default = file.Profiles[0].ID
	}
	if !seen[file.Default] {
		return ModelProfilesFile{}, fmt.Errorf("default model profile %q is not configured", file.Default)
	}
	return file, nil
}

func (p ModelProfile) client() llmclient.Client {
	client := llmclient.Client{BaseURL: p.BaseURL, Model: p.Model, AuthTokenFile: p.TokenFile, ReasoningEffort: p.ReasoningEffort, StructuredJSON: p.StructuredJSON, MaxOutputTokens: p.MaxOutputTokens, MaxInputBytes: p.MaxInputBytes}
	if p.Provider == "subscription" {
		client.ReasoningEffort = ""
		if client.MaxInputBytes == 0 {
			client.MaxInputBytes = llmclient.SubscriptionInputByteLimit
		}
		if client.MaxOutputTokens == 0 {
			client.MaxOutputTokens = llmclient.SubscriptionMaxOutputTokens
		}
	} else {
		if client.MaxInputBytes == 0 {
			client.MaxInputBytes = llmclient.DefaultInputByteLimit
		}
		if client.MaxOutputTokens == 0 {
			client.MaxOutputTokens = 32768
		}
	}
	if p.RequestTimeoutSeconds > 0 {
		client.HTTPClient = &http.Client{Timeout: time.Duration(p.RequestTimeoutSeconds) * time.Second}
	}
	return client
}

func (s *Server) profile(id string) (ModelProfile, bool) {
	for _, profile := range s.config.Profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return ModelProfile{}, false
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if len(s.config.Profiles) > 0 {
		options := make([]modelOption, 0, len(s.config.Profiles))
		for _, profile := range s.config.Profiles {
			options = append(options, modelOption{ID: profile.ID, Label: profile.Label, Provider: profile.Provider, Model: profile.Model, Current: profile.ID == s.config.DefaultProfile})
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": options, "current": s.config.DefaultProfile, "profiles_enabled": true})
		return
	}
	current := strings.TrimSpace(s.config.LLM.Model)
	options := map[string]modelOption{}
	if current != "" {
		options[current] = modelOption{ID: current, Current: true}
	}
	ctx := r.Context()
	if deadline, ok := ctx.Deadline(); !ok || deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, modelCatalogTimeout)
		defer cancel()
	}
	models, err := s.config.LLM.ListModels(ctx)
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		item := options[id]
		item.ID = id
		item.Current = id == current
		options[id] = item
	}
	values := make([]modelOption, 0, len(options))
	for _, item := range options {
		values = append(values, item)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Current != values[j].Current {
			return values[i].Current
		}
		return values[i].ID < values[j].ID
	})
	result := map[string]any{"models": values, "current": current}
	if err != nil {
		result["catalog_error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) changeIntakeProfile(current *intakeRun, id string) error {
	profile, ok := s.profile(id)
	if !ok {
		return fmt.Errorf("model profile %q is not configured", id)
	}
	current.mu.Lock()
	if current.deleted || current.busy || current.assessmentID != "" || len(current.messages) > 0 {
		current.mu.Unlock()
		return fmt.Errorf("start a new session to use another model after the conversation begins")
	}
	current.client, current.profileID = profile.client(), id
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	return current.persist()
}

func (s *Server) changeRunProfile(current *run, id string) error {
	profile, ok := s.profile(id)
	if !ok {
		return fmt.Errorf("model profile %q is not configured", id)
	}
	current.mu.Lock()
	if current.deleted || current.started || current.chatBusy || current.status != "draft" {
		current.mu.Unlock()
		return fmt.Errorf("start a new session to change the model after an assessment begins")
	}
	current.client, current.profileID = profile.client(), id
	current.state.Model = profile.Model
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	return current.persist()
}

const modelCatalogTimeout = 5 * time.Second

func validateModelID(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("model is required")
	}
	if len(model) > 240 || strings.ContainsAny(model, "\r\n") {
		return "", fmt.Errorf("model ID is invalid")
	}
	return model, nil
}

func (s *Server) changeIntakeModel(current *intakeRun, model string) error {
	if len(s.config.Profiles) > 0 {
		return fmt.Errorf("choose a configured model profile")
	}
	model, err := validateModelID(model)
	if err != nil {
		return err
	}
	current.mu.Lock()
	deleted := current.deleted
	if deleted || current.busy || current.assessmentID != "" {
		current.mu.Unlock()
		if deleted {
			return fmt.Errorf("session has been deleted")
		}
		return fmt.Errorf("model cannot change while this session is active")
	}
	current.client.Model = model
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.persist(); err != nil {
		return fmt.Errorf("save model selection: %w", err)
	}
	return nil
}

func (s *Server) changeRunModel(current *run, model string) error {
	if len(s.config.Profiles) > 0 {
		return fmt.Errorf("choose a configured model profile")
	}
	model, err := validateModelID(model)
	if err != nil {
		return err
	}
	current.mu.Lock()
	deleted := current.deleted
	if deleted || current.started || current.chatBusy {
		current.mu.Unlock()
		if deleted {
			return fmt.Errorf("session has been deleted")
		}
		return fmt.Errorf("model cannot change while this session is active")
	}
	current.client.Model = model
	current.state.Model = model
	current.updatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.persist(); err != nil {
		return fmt.Errorf("save model selection: %w", err)
	}
	return nil
}
