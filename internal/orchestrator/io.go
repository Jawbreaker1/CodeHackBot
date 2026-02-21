package orchestrator

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type MalformedEventLine struct {
	Offset int64
	Raw    string
	Error  string
	Hash   string
}

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
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
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

func readEventsFromOffsetResilient(path string, fromOffset int64) ([]EventEnvelope, int64, []MalformedEventLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, nil, fmt.Errorf("stat event log: %w", err)
	}
	size := info.Size()
	if fromOffset < 0 || fromOffset > size {
		fromOffset = 0
	}
	if _, err := f.Seek(fromOffset, io.SeekStart); err != nil {
		return nil, 0, nil, fmt.Errorf("seek event log: %w", err)
	}

	events := make([]EventEnvelope, 0)
	malformed := make([]MalformedEventLine, 0)
	reader := bufio.NewReader(f)
	offset := fromOffset
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.EOF {
			return nil, 0, nil, fmt.Errorf("read event log: %w", readErr)
		}
		lineOffset := offset
		offset += int64(len(line))
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var event EventEnvelope
			if err := json.Unmarshal(trimmed, &event); err != nil {
				malformed = append(malformed, MalformedEventLine{
					Offset: lineOffset,
					Raw:    string(trimmed),
					Error:  fmt.Sprintf("parse event: %v", err),
					Hash:   hashMalformedEvent(trimmed, err),
				})
			} else if err := ValidateEventEnvelope(event); err != nil {
				malformed = append(malformed, MalformedEventLine{
					Offset: lineOffset,
					Raw:    string(trimmed),
					Error:  fmt.Sprintf("validate event: %v", err),
					Hash:   hashMalformedEvent(trimmed, err),
				})
			} else {
				events = append(events, event)
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	return events, offset, malformed, nil
}

func hashMalformedEvent(raw []byte, parseErr error) string {
	sum := sha256.Sum256(append(append([]byte{}, raw...), []byte(parseErr.Error())...))
	return hex.EncodeToString(sum[:])
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
