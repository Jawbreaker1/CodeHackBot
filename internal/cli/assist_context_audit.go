package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

const (
	assistAuditSectionMaxChars = 1600
	assistAuditTextMaxChars    = 900
	assistAuditListMaxItems    = 20
)

type assistContextAuditRecord struct {
	Time       string                     `json:"time"`
	SessionID  string                     `json:"session_id"`
	Goal       string                     `json:"goal"`
	Mode       string                     `json:"mode"`
	Input      assistContextAuditInput    `json:"input"`
	InputSizes assistContextAuditSizes    `json:"input_sizes"`
	Suggestion assistContextAuditDecision `json:"suggestion"`
	Error      string                     `json:"error,omitempty"`
	LLM        assistContextAuditLLM      `json:"llm"`
}

type assistContextAuditInput struct {
	Scope       []string `json:"scope,omitempty"`
	Targets     []string `json:"targets,omitempty"`
	WorkingDir  string   `json:"working_dir,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	KnownFacts  []string `json:"known_facts,omitempty"`
	Focus       string   `json:"focus,omitempty"`
	Plan        string   `json:"plan,omitempty"`
	Inventory   string   `json:"inventory,omitempty"`
	ChatHistory string   `json:"chat_history,omitempty"`
	RecentLog   string   `json:"recent_log,omitempty"`
	Playbooks   string   `json:"playbooks,omitempty"`
	Tools       string   `json:"tools,omitempty"`
}

type assistContextAuditSizes struct {
	SummaryChars     int `json:"summary_chars"`
	KnownFactsCount  int `json:"known_facts_count"`
	FocusChars       int `json:"focus_chars"`
	PlanChars        int `json:"plan_chars"`
	InventoryChars   int `json:"inventory_chars"`
	ChatHistoryChars int `json:"chat_history_chars"`
	RecentLogChars   int `json:"recent_log_chars"`
	PlaybooksChars   int `json:"playbooks_chars"`
	ToolsChars       int `json:"tools_chars"`
}

type assistContextAuditDecision struct {
	Type         string   `json:"type,omitempty"`
	Decision     string   `json:"decision,omitempty"`
	Command      string   `json:"command,omitempty"`
	Args         []string `json:"args,omitempty"`
	Question     string   `json:"question,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Final        string   `json:"final,omitempty"`
	ObjectiveMet *bool    `json:"objective_met,omitempty"`
}

type assistContextAuditLLM struct {
	Attempted           bool   `json:"attempted"`
	Model               string `json:"model,omitempty"`
	ParseRepairUsed     bool   `json:"parse_repair_used,omitempty"`
	PrimaryFinishReason string `json:"primary_finish_reason,omitempty"`
	RepairFinishReason  string `json:"repair_finish_reason,omitempty"`
	PrimaryPreview      string `json:"primary_preview,omitempty"`
	RepairPreview       string `json:"repair_preview,omitempty"`
}

func (r *Runner) writeAssistContextAudit(
	sessionDir string,
	goal string,
	mode string,
	input assist.Input,
	suggestion assist.Suggestion,
	suggestErr error,
	metaSeen bool,
	meta assist.LLMSuggestMetadata,
) error {
	path := filepath.Join(sessionDir, "artifacts", "assist", "context_audit.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create assist audit dir: %w", err)
	}

	record := assistContextAuditRecord{
		Time:      time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: strings.TrimSpace(r.sessionID),
		Goal:      clampAuditText(goal, assistAuditTextMaxChars),
		Mode:      strings.TrimSpace(mode),
		Input: assistContextAuditInput{
			Scope:       clampStringList(input.Scope, assistAuditListMaxItems, 120),
			Targets:     clampStringList(input.Targets, assistAuditListMaxItems, 160),
			WorkingDir:  clampAuditText(input.WorkingDir, assistAuditTextMaxChars),
			Summary:     clampAuditText(input.Summary, assistAuditSectionMaxChars),
			KnownFacts:  clampStringList(input.KnownFacts, assistAuditListMaxItems, 220),
			Focus:       clampAuditText(input.Focus, assistAuditSectionMaxChars),
			Plan:        clampAuditText(input.Plan, assistAuditSectionMaxChars),
			Inventory:   clampAuditText(input.Inventory, assistAuditSectionMaxChars),
			ChatHistory: clampAuditText(input.ChatHistory, assistAuditSectionMaxChars),
			RecentLog:   clampAuditText(input.RecentLog, assistAuditSectionMaxChars),
			Playbooks:   clampAuditText(input.Playbooks, assistAuditSectionMaxChars),
			Tools:       clampAuditText(input.Tools, assistAuditSectionMaxChars),
		},
		InputSizes: assistContextAuditSizes{
			SummaryChars:     len(input.Summary),
			KnownFactsCount:  len(input.KnownFacts),
			FocusChars:       len(input.Focus),
			PlanChars:        len(input.Plan),
			InventoryChars:   len(input.Inventory),
			ChatHistoryChars: len(input.ChatHistory),
			RecentLogChars:   len(input.RecentLog),
			PlaybooksChars:   len(input.Playbooks),
			ToolsChars:       len(input.Tools),
		},
		Suggestion: assistContextAuditDecision{
			Type:         strings.TrimSpace(suggestion.Type),
			Decision:     strings.TrimSpace(suggestion.Decision),
			Command:      strings.TrimSpace(suggestion.Command),
			Args:         clampStringList(suggestion.Args, assistAuditListMaxItems, 120),
			Question:     clampAuditText(suggestion.Question, assistAuditTextMaxChars),
			Summary:      clampAuditText(suggestion.Summary, assistAuditTextMaxChars),
			Final:        clampAuditText(suggestion.Final, assistAuditTextMaxChars),
			ObjectiveMet: suggestion.ObjectiveMet,
		},
		LLM: assistContextAuditLLM{
			Attempted:           metaSeen,
			Model:               clampAuditText(meta.Model, 120),
			ParseRepairUsed:     meta.ParseRepairUsed,
			PrimaryFinishReason: clampAuditText(meta.PrimaryFinishReason, 120),
			RepairFinishReason:  clampAuditText(meta.RepairFinishReason, 120),
			PrimaryPreview:      clampAuditText(meta.PrimaryResponse, assistAuditSectionMaxChars),
			RepairPreview:       clampAuditText(meta.RepairResponse, assistAuditSectionMaxChars),
		},
	}
	if suggestErr != nil {
		record.Error = clampAuditText(suggestErr.Error(), assistAuditSectionMaxChars)
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal assist context audit: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open assist context audit: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write assist context audit: %w", err)
	}
	return nil
}

func clampStringList(values []string, maxItems int, maxChars int) []string {
	if len(values) == 0 {
		return nil
	}
	if maxItems <= 0 {
		maxItems = len(values)
	}
	if maxChars <= 0 {
		maxChars = assistAuditTextMaxChars
	}
	out := make([]string, 0, maxItems)
	for _, value := range values {
		value = clampAuditText(value, maxChars)
		if value == "" {
			continue
		}
		out = append(out, value)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func clampAuditText(text string, max int) string {
	normalized := strings.TrimSpace(text)
	if normalized == "" || max <= 0 || len(normalized) <= max {
		return normalized
	}
	if max <= 18 {
		return normalized[:max]
	}
	return strings.TrimSpace(normalized[:max-14]) + "...(truncated)"
}
