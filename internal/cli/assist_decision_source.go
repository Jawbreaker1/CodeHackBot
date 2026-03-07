package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

const (
	decisionSourceLLMDirect    = "llm_direct"
	decisionSourceLLMRepair    = "llm_repair"
	decisionSourceRuntimeAdapt = "runtime_adapt"
	decisionSourceStatic       = "static_fallback"
)

type assistDecisionSourceRecord struct {
	Time      string `json:"time"`
	SessionID string `json:"session_id"`
	Source    string `json:"source"`
	Mode      string `json:"mode,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Command   string `json:"command,omitempty"`
}

func assistDecisionSourcePath(sessionDir string) string {
	return filepath.Join(sessionDir, "artifacts", "assist", "decision_source.jsonl")
}

func classifyAssistDecisionSource(metaSeen bool, meta assist.LLMSuggestMetadata, fallbackUsed bool) string {
	if fallbackUsed || !metaSeen {
		return decisionSourceStatic
	}
	if meta.ParseRepairUsed {
		return decisionSourceLLMRepair
	}
	return decisionSourceLLMDirect
}

func normalizeDecisionSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case decisionSourceLLMDirect:
		return decisionSourceLLMDirect
	case decisionSourceLLMRepair:
		return decisionSourceLLMRepair
	case decisionSourceRuntimeAdapt:
		return decisionSourceRuntimeAdapt
	case decisionSourceStatic:
		return decisionSourceStatic
	default:
		return ""
	}
}

func (r *Runner) appendAssistDecisionSource(sessionDir, source, mode, reason, command string) error {
	source = normalizeDecisionSource(source)
	if source == "" {
		return nil
	}
	record := assistDecisionSourceRecord{
		Time:      time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: strings.TrimSpace(r.sessionID),
		Source:    source,
		Mode:      strings.TrimSpace(mode),
		Reason:    strings.TrimSpace(reason),
		Command:   strings.TrimSpace(command),
	}
	path := assistDecisionSourcePath(sessionDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create decision source dir: %w", err)
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal decision source: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open decision source log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write decision source log: %w", err)
	}
	return nil
}

func readAssistDecisionSourceMix(sessionDir string) (map[string]int, int, error) {
	path := assistDecisionSourcePath(sessionDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]int{}, 0, nil
		}
		return nil, 0, err
	}
	counts := map[string]int{}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	total := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec assistDecisionSourceRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		source := normalizeDecisionSource(rec.Source)
		if source == "" {
			continue
		}
		counts[source]++
		total++
	}
	return counts, total, nil
}

func formatDecisionSourceMix(counts map[string]int, total int) string {
	if total <= 0 {
		return "(none)"
	}
	keys := []string{
		decisionSourceLLMDirect,
		decisionSourceLLMRepair,
		decisionSourceRuntimeAdapt,
		decisionSourceStatic,
	}
	extra := make([]string, 0, len(counts))
	for key := range counts {
		if key == decisionSourceLLMDirect || key == decisionSourceLLMRepair || key == decisionSourceRuntimeAdapt || key == decisionSourceStatic {
			continue
		}
		extra = append(extra, key)
	}
	sort.Strings(extra)
	keys = append(keys, extra...)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		n := counts[key]
		if n <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d (%.1f%%)", key, n, (float64(n)*100)/float64(total)))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}
