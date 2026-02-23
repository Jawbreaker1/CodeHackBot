package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type runEventCache struct {
	offset          int64
	events          []EventEnvelope
	seenEventIDs    map[string]struct{}
	lastSeqByWorker map[string]int64

	hasRunStarted bool
	runState      string
	workerActive  map[string]bool
	taskState     map[string]string
	workerByID    map[string]WorkerStatus

	quarantineLoaded bool
	quarantineHashes map[string]struct{}
}

type quarantineIndex struct {
	Hashes []string `json:"hashes"`
}

type quarantinedEventLine struct {
	Hash   string `json:"hash"`
	Offset int64  `json:"offset"`
	Error  string `json:"error"`
	Raw    string `json:"raw"`
}

func newRunEventCache() *runEventCache {
	return &runEventCache{
		seenEventIDs:     map[string]struct{}{},
		lastSeqByWorker:  map[string]int64{},
		workerActive:     map[string]bool{},
		taskState:        map[string]string{},
		workerByID:       map[string]WorkerStatus{},
		quarantineHashes: map[string]struct{}{},
	}
}

func (c *runEventCache) reset() {
	next := newRunEventCache()
	*c = *next
}

func (m *Manager) ensureEventCacheLocked(runID string) *runEventCache {
	cache, ok := m.eventCache[runID]
	if ok && cache != nil {
		return cache
	}
	cache = newRunEventCache()
	m.eventCache[runID] = cache
	return cache
}

func (m *Manager) refreshEventCacheLocked(runID string) (*runEventCache, error) {
	cache := m.ensureEventCacheLocked(runID)
	eventPath := m.eventPath(runID)

	info, err := os.Stat(eventPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("open event log: %w", err)
		}
		return nil, fmt.Errorf("stat event log: %w", err)
	}
	if cache.offset > info.Size() {
		cache.reset()
	}

	events, nextOffset, malformed, err := readEventsFromOffsetResilient(eventPath, cache.offset)
	if err != nil {
		return nil, err
	}
	seqState, seqLoaded := m.seqState[runID]
	seqDirty := false
	for _, event := range events {
		if _, seen := cache.seenEventIDs[event.EventID]; seen {
			continue
		}
		prev, ok := cache.lastSeqByWorker[event.WorkerID]
		if ok && event.Seq <= prev {
			return nil, fmt.Errorf("non-monotonic seq for worker %s: %d <= %d", event.WorkerID, event.Seq, prev)
		}
		cache.seenEventIDs[event.EventID] = struct{}{}
		cache.lastSeqByWorker[event.WorkerID] = event.Seq
		cache.events = append(cache.events, event)
		cache.applyEvent(event)
		if seqLoaded && event.Seq > seqState[event.WorkerID] {
			seqState[event.WorkerID] = event.Seq
			seqDirty = true
		}
	}
	cache.offset = nextOffset

	emittedWarning, err := m.handleMalformedLinesLocked(runID, cache, malformed)
	if err != nil {
		return nil, err
	}
	if emittedWarning {
		warningEvents, warningOffset, _, err := readEventsFromOffsetResilient(eventPath, cache.offset)
		if err != nil {
			return nil, err
		}
		for _, event := range warningEvents {
			if _, seen := cache.seenEventIDs[event.EventID]; seen {
				continue
			}
			prev, ok := cache.lastSeqByWorker[event.WorkerID]
			if ok && event.Seq <= prev {
				return nil, fmt.Errorf("non-monotonic seq for worker %s: %d <= %d", event.WorkerID, event.Seq, prev)
			}
			cache.seenEventIDs[event.EventID] = struct{}{}
			cache.lastSeqByWorker[event.WorkerID] = event.Seq
			cache.events = append(cache.events, event)
			cache.applyEvent(event)
			if seqLoaded && event.Seq > seqState[event.WorkerID] {
				seqState[event.WorkerID] = event.Seq
				seqDirty = true
			}
		}
		cache.offset = warningOffset
	}
	if seqLoaded && seqDirty {
		if err := m.writeSeqStateLocked(runID, seqState); err != nil {
			return nil, err
		}
	}
	return cache, nil
}

func (m *Manager) handleMalformedLinesLocked(runID string, cache *runEventCache, malformed []MalformedEventLine) (bool, error) {
	if len(malformed) == 0 {
		return false, nil
	}
	if err := m.loadQuarantineIndexLocked(runID, cache); err != nil {
		return false, err
	}
	eventPath := m.eventPath(runID)
	quarantinePath := m.quarantinePath(runID)

	emittedWarning := false
	indexChanged := false
	for _, line := range malformed {
		if _, exists := cache.quarantineHashes[line.Hash]; exists {
			continue
		}
		record := quarantinedEventLine{
			Hash:   line.Hash,
			Offset: line.Offset,
			Error:  line.Error,
			Raw:    line.Raw,
		}
		if err := appendJSONL(quarantinePath, record); err != nil {
			return false, err
		}
		seq, err := m.nextSeqLocked(runID, orchestratorWorkerID)
		if err != nil {
			return false, err
		}
		payload := map[string]any{
			"reason":          "malformed_event_line",
			"hash":            line.Hash,
			"offset":          line.Offset,
			"error":           line.Error,
			"quarantine_path": quarantinePath,
		}
		if err := AppendEventJSONL(eventPath, EventEnvelope{
			EventID:  NewEventID(),
			RunID:    runID,
			WorkerID: orchestratorWorkerID,
			Seq:      seq,
			TS:       m.Now(),
			Type:     EventTypeRunWarning,
			Payload:  mustJSONRaw(payload),
		}); err != nil {
			return false, err
		}
		cache.quarantineHashes[line.Hash] = struct{}{}
		indexChanged = true
		emittedWarning = true
	}
	if indexChanged {
		if err := m.writeQuarantineIndexLocked(runID, cache); err != nil {
			return false, err
		}
	}
	return emittedWarning, nil
}

func (m *Manager) loadQuarantineIndexLocked(runID string, cache *runEventCache) error {
	if cache.quarantineLoaded {
		return nil
	}
	indexPath := m.quarantineIndexPath(runID)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			cache.quarantineLoaded = true
			return nil
		}
		return fmt.Errorf("read quarantine index: %w", err)
	}
	var index quarantineIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("parse quarantine index: %w", err)
	}
	for _, hash := range index.Hashes {
		trimmed := strings.TrimSpace(hash)
		if trimmed == "" {
			continue
		}
		cache.quarantineHashes[trimmed] = struct{}{}
	}
	cache.quarantineLoaded = true
	return nil
}

func (m *Manager) writeQuarantineIndexLocked(runID string, cache *runEventCache) error {
	hashes := make([]string, 0, len(cache.quarantineHashes))
	for hash := range cache.quarantineHashes {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	return WriteJSONAtomic(m.quarantineIndexPath(runID), quarantineIndex{Hashes: hashes})
}

func (m *Manager) quarantinePath(runID string) string {
	return filepath.Join(BuildRunPaths(m.SessionsDir, runID).EventDir, "event.quarantine.jsonl")
}

func (m *Manager) quarantineIndexPath(runID string) string {
	return filepath.Join(BuildRunPaths(m.SessionsDir, runID).EventDir, "event.quarantine.index.json")
}

func appendJSONL(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal jsonl record: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create jsonl dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open jsonl: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append jsonl: %w", err)
	}
	return nil
}

func (c *runEventCache) applyEvent(event EventEnvelope) {
	taskID := strings.TrimSpace(event.TaskID)
	switch event.Type {
	case EventTypeRunStarted:
		c.hasRunStarted = true
	case EventTypeRunStopped:
		c.runState = "stopped"
	case EventTypeRunCompleted:
		c.runState = "completed"
	case EventTypeWorkerStarted:
		c.workerActive[event.WorkerID] = true
	case EventTypeWorkerStopped:
		c.workerActive[event.WorkerID] = false
	case EventTypeTaskLeased:
		if taskID != "" {
			c.taskState[taskID] = "queued"
		}
	case EventTypeTaskStarted, EventTypeTaskProgress, EventTypeTaskArtifact, EventTypeTaskFinding:
		if taskID != "" {
			c.taskState[taskID] = "running"
		}
	case EventTypeTaskCompleted, EventTypeTaskFailed:
		if taskID != "" {
			c.taskState[taskID] = "done"
		}
	}

	if strings.TrimSpace(event.WorkerID) == "" {
		return
	}
	ws := c.workerByID[event.WorkerID]
	if ws.WorkerID == "" {
		ws.WorkerID = event.WorkerID
		ws.State = "seen"
	}
	ws.LastSeq = maxI64(ws.LastSeq, event.Seq)
	if event.TS.After(ws.LastEvent) {
		ws.LastEvent = event.TS
	}
	switch event.Type {
	case EventTypeWorkerStarted:
		ws.State = "active"
	case EventTypeWorkerHeartbeat:
		if ws.State != "stopped" {
			ws.State = "active"
		}
	case EventTypeWorkerStopped:
		ws.State = "stopped"
	case EventTypeTaskStarted, EventTypeTaskProgress, EventTypeTaskArtifact, EventTypeTaskFinding:
		if ws.State != "stopped" {
			ws.State = "active"
		}
		ws.CurrentTask = event.TaskID
	case EventTypeTaskCompleted, EventTypeTaskFailed:
		if ws.CurrentTask == event.TaskID {
			ws.CurrentTask = ""
		}
	}
	c.workerByID[event.WorkerID] = ws
}

func buildRunStatusFromCache(runID string, cache *runEventCache) RunStatus {
	status := RunStatus{
		RunID: runID,
		State: "unknown",
	}
	if cache == nil {
		return status
	}
	if strings.TrimSpace(cache.runState) != "" {
		status.State = cache.runState
	} else if cache.hasRunStarted {
		status.State = "running"
	}
	for _, active := range cache.workerActive {
		if active {
			status.ActiveWorkers++
		}
	}
	for _, task := range cache.taskState {
		switch task {
		case "queued":
			status.QueuedTasks++
		case "running":
			status.RunningTasks++
		}
	}
	return status
}

func buildWorkerStatusFromCache(cache *runEventCache) []WorkerStatus {
	if cache == nil {
		return nil
	}
	out := make([]WorkerStatus, 0, len(cache.workerByID))
	for _, worker := range cache.workerByID {
		out = append(out, worker)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].WorkerID < out[j].WorkerID
	})
	return out
}
