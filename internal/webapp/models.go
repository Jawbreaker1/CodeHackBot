package webapp

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type modelOption struct {
	ID      string `json:"id"`
	Current bool   `json:"current"`
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
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
	model, err := validateModelID(model)
	if err != nil {
		return err
	}
	current.mu.Lock()
	if current.busy || current.assessmentID != "" {
		current.mu.Unlock()
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
	model, err := validateModelID(model)
	if err != nil {
		return err
	}
	current.mu.Lock()
	if current.started || current.chatBusy {
		current.mu.Unlock()
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
