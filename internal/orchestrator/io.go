package orchestrator

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func WriteJSONAtomic(path string, v any) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func AppendEventJSONL(path string, event EventEnvelope) error {
	if err := ValidateEventEnvelope(event); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func ReadEvents(path string) ([]EventEnvelope, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()

	events := []EventEnvelope{}
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var event EventEnvelope
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("parse event line %d: %w", lineNo, err)
		}
		if err := ValidateEventEnvelope(event); err != nil {
			return nil, fmt.Errorf("validate event line %d: %w", lineNo, err)
		}
		events = append(events, event)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read event log: %w", err)
	}
	return events, nil
}

func DedupeEvents(events []EventEnvelope) []EventEnvelope {
	out := make([]EventEnvelope, 0, len(events))
	seen := make(map[string]struct{}, len(events))
	for _, event := range events {
		if _, ok := seen[event.EventID]; ok {
			continue
		}
		seen[event.EventID] = struct{}{}
		out = append(out, event)
	}
	return out
}

func ValidateMonotonicSequences(events []EventEnvelope) error {
	lastSeq := map[string]int64{}
	for _, event := range events {
		prev, ok := lastSeq[event.WorkerID]
		if ok && event.Seq <= prev {
			return fmt.Errorf("non-monotonic seq for worker %s: %d <= %d", event.WorkerID, event.Seq, prev)
		}
		lastSeq[event.WorkerID] = event.Seq
	}
	return nil
}

func readLease(path string) (TaskLease, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskLease{}, fmt.Errorf("read lease %s: %w", path, err)
	}
	var lease TaskLease
	if err := json.Unmarshal(data, &lease); err != nil {
		return TaskLease{}, fmt.Errorf("parse lease %s: %w", path, err)
	}
	if err := ValidateTaskLease(lease); err != nil {
		return TaskLease{}, fmt.Errorf("validate lease %s: %w", path, err)
	}
	return lease, nil
}
