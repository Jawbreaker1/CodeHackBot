package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

type assistContextSection struct {
	Section string `json:"section"`
	Source  string `json:"source,omitempty"`
	Path    string `json:"path,omitempty"`
	Chars   int    `json:"chars,omitempty"`
	Items   int    `json:"items,omitempty"`
}

type assistContextPacket struct {
	Time              string                  `json:"time"`
	SessionID         string                  `json:"session_id"`
	Goal              string                  `json:"goal"`
	Mode              string                  `json:"mode"`
	ApproxPromptChars int                     `json:"approx_prompt_chars"`
	InputSizes        assistContextAuditSizes `json:"input_sizes"`
	Sections          []assistContextSection  `json:"sections,omitempty"`
}

type assistMemoryOperation struct {
	Time      string `json:"time"`
	SessionID string `json:"session_id"`
	Direction string `json:"direction"`
	Component string `json:"component"`
	Source    string `json:"source,omitempty"`
	Path      string `json:"path,omitempty"`
	Chars     int    `json:"chars,omitempty"`
	Items     int    `json:"items,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func assistContextPacketPath(sessionDir string) string {
	return filepath.Join(sessionDir, "artifacts", "assist", "context_packet.json")
}

func assistMemoryOpsPath(sessionDir string) string {
	return filepath.Join(sessionDir, "artifacts", "assist", "memory_ops.jsonl")
}

func estimateAssistPromptChars(input assist.Input) int {
	total := len(input.Goal) +
		len(input.Summary) +
		len(input.Focus) +
		len(input.ChatHistory) +
		len(input.WorkingDir) +
		len(input.RecentLog) +
		len(input.Playbooks) +
		len(input.Tools) +
		len(input.Mode) +
		len(input.Plan) +
		len(input.Inventory)
	for _, item := range input.Scope {
		total += len(item)
	}
	for _, item := range input.Targets {
		total += len(item)
	}
	for _, item := range input.KnownFacts {
		total += len(item)
	}
	return total
}

func (r *Runner) buildAssistContextSections(sessionDir string, artifacts memory.Artifacts, input assist.Input) []assistContextSection {
	toolsManifest := filepath.Join(sessionDir, "artifacts", "tools", "manifest.json")
	journalPath := taskJournalPath(sessionDir)
	journal := readFileTrimmed(journalPath)
	return []assistContextSection{
		{Section: "goal", Source: "user_input", Chars: len(input.Goal), Items: boolCount(strings.TrimSpace(input.Goal) != "")},
		{Section: "scope", Source: "config.scope.networks", Chars: len(strings.Join(input.Scope, " ")), Items: len(input.Scope)},
		{Section: "targets", Source: "config.scope.targets", Chars: len(strings.Join(input.Targets, " ")), Items: len(input.Targets)},
		{Section: "summary", Source: "memory.summary", Path: artifacts.SummaryPath, Chars: len(input.Summary), Items: boolCount(strings.TrimSpace(input.Summary) != "")},
		{Section: "known_facts", Source: "memory.known_facts", Path: artifacts.FactsPath, Chars: len(strings.Join(input.KnownFacts, "\n")), Items: len(input.KnownFacts)},
		{Section: "focus", Source: "memory.focus", Path: artifacts.FocusPath, Chars: len(input.Focus), Items: boolCount(strings.TrimSpace(input.Focus) != "")},
		{Section: "chat_history", Source: "memory.chat", Path: artifacts.ChatPath, Chars: len(input.ChatHistory), Items: boolCount(strings.TrimSpace(input.ChatHistory) != "")},
		{Section: "recent_log", Source: "memory.state.recent_observations", Path: artifacts.StatePath, Chars: len(input.RecentLog), Items: boolCount(strings.TrimSpace(input.RecentLog) != "")},
		{Section: "plan", Source: "session.plan", Path: filepath.Join(sessionDir, r.cfg.Session.PlanFilename), Chars: len(input.Plan), Items: boolCount(strings.TrimSpace(input.Plan) != "")},
		{Section: "inventory", Source: "session.inventory", Path: filepath.Join(sessionDir, r.cfg.Session.InventoryFilename), Chars: len(input.Inventory), Items: boolCount(strings.TrimSpace(input.Inventory) != "")},
		{Section: "playbooks", Source: "docs.playbooks", Path: filepath.Join("docs", "playbooks"), Chars: len(input.Playbooks), Items: boolCount(strings.TrimSpace(input.Playbooks) != "")},
		{Section: "tools", Source: "session.tools.manifest", Path: toolsManifest, Chars: len(input.Tools), Items: boolCount(strings.TrimSpace(input.Tools) != "")},
		{Section: "task_journal", Source: "session.artifacts.assist", Path: journalPath, Chars: len(journal), Items: boolCount(strings.TrimSpace(journal) != "")},
	}
}

func (r *Runner) writeAssistContextPacket(
	sessionDir string,
	goal string,
	mode string,
	input assist.Input,
	sections []assistContextSection,
) error {
	packet := assistContextPacket{
		Time:              time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:         strings.TrimSpace(r.sessionID),
		Goal:              clampAuditText(goal, assistAuditSectionMaxChars),
		Mode:              strings.TrimSpace(mode),
		ApproxPromptChars: estimateAssistPromptChars(input),
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
		Sections: sections,
	}
	path := assistContextPacketPath(sessionDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create assist packet dir: %w", err)
	}
	data, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal assist context packet: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write assist context packet: %w", err)
	}
	return nil
}

func (r *Runner) appendAssistMemoryOp(sessionDir string, op assistMemoryOperation) error {
	op.Time = time.Now().UTC().Format(time.RFC3339Nano)
	op.SessionID = strings.TrimSpace(r.sessionID)
	op.Direction = strings.ToLower(strings.TrimSpace(op.Direction))
	if op.Direction == "" {
		op.Direction = "read"
	}
	path := assistMemoryOpsPath(sessionDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create assist memory ops dir: %w", err)
	}
	payload, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("marshal assist memory op: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open assist memory ops: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write assist memory op: %w", err)
	}
	return nil
}

func (r *Runner) appendAssistMemoryReadTrace(sessionDir string, sections []assistContextSection, reason string) error {
	for _, section := range sections {
		if err := r.appendAssistMemoryOp(sessionDir, assistMemoryOperation{
			Direction: "read",
			Component: strings.TrimSpace(section.Section),
			Source:    strings.TrimSpace(section.Source),
			Path:      strings.TrimSpace(section.Path),
			Chars:     section.Chars,
			Items:     section.Items,
			Reason:    strings.TrimSpace(reason),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) recentAssistMemoryOps(sessionDir string, max int) ([]assistMemoryOperation, error) {
	path := assistMemoryOpsPath(sessionDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	records := make([]assistMemoryOperation, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var op assistMemoryOperation
		if err := json.Unmarshal([]byte(line), &op); err != nil {
			continue
		}
		records = append(records, op)
	}
	if max > 0 && len(records) > max {
		records = records[len(records)-max:]
	}
	return records, nil
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
