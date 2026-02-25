package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
)

type EvidenceIngestResult struct {
	ArtifactsWritten int
	FindingsWritten  int
}

type evidenceIngestCursor struct {
	Offset int64 `json:"offset"`
}

func (m *Manager) IngestEvidence(runID string) (EvidenceIngestResult, error) {
	var out EvidenceIngestResult
	cursor, err := m.readEvidenceCursor(runID)
	if err != nil {
		return out, err
	}
	events, nextOffset, malformed, err := readEventsFromOffsetResilient(m.eventPath(runID), cursor.Offset)
	if err != nil {
		return out, err
	}
	if len(malformed) > 0 {
		m.mu.Lock()
		cache := m.ensureEventCacheLocked(runID)
		_, malformedErr := m.handleMalformedLinesLocked(runID, cache, malformed)
		m.mu.Unlock()
		if malformedErr != nil {
			return out, malformedErr
		}
	}
	events = DedupeEvents(events)
	paths := BuildRunPaths(m.SessionsDir, runID)

	for _, event := range events {
		switch event.Type {
		case EventTypeTaskArtifact:
			artifact, err := artifactFromEvent(event)
			if err != nil {
				return out, err
			}
			path := filepath.Join(paths.ArtifactDir, event.EventID+".json")
			if _, err := os.Stat(path); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return out, err
			}
			if err := WriteJSONAtomic(path, artifact); err != nil {
				return out, err
			}
			out.ArtifactsWritten++
		case EventTypeTaskFinding:
			finding, err := findingFromEvent(event)
			if err != nil {
				return out, err
			}
			dedupeKey := FindingDedupeKey(finding)
			finding.Metadata["dedupe_key"] = dedupeKey
			path := filepath.Join(paths.FindingDir, hashKey(dedupeKey)+".json")

			existing, ok, err := readFinding(path)
			if err != nil {
				return out, err
			}
			merged := finding
			if ok {
				merged = mergeFindings(existing, finding)
			}
			if ok && reflect.DeepEqual(existing, merged) {
				continue
			}
			if err := WriteJSONAtomic(path, merged); err != nil {
				return out, err
			}
			out.FindingsWritten++
		}
	}
	if err := m.writeEvidenceCursor(runID, evidenceIngestCursor{Offset: nextOffset}); err != nil {
		return out, err
	}
	return out, nil
}

func (m *Manager) readEvidenceCursor(runID string) (evidenceIngestCursor, error) {
	path := m.evidenceCursorPath(runID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return evidenceIngestCursor{}, nil
		}
		return evidenceIngestCursor{}, fmt.Errorf("read evidence cursor: %w", err)
	}
	var cursor evidenceIngestCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return evidenceIngestCursor{}, fmt.Errorf("parse evidence cursor: %w", err)
	}
	if cursor.Offset < 0 {
		cursor.Offset = 0
	}
	return cursor, nil
}

func (m *Manager) writeEvidenceCursor(runID string, cursor evidenceIngestCursor) error {
	if cursor.Offset < 0 {
		cursor.Offset = 0
	}
	return WriteJSONAtomic(m.evidenceCursorPath(runID), cursor)
}

func (m *Manager) evidenceCursorPath(runID string) string {
	return filepath.Join(BuildRunPaths(m.SessionsDir, runID).EventDir, "evidence.cursor.json")
}

func FindingDedupeKey(f Finding) string {
	location := f.Metadata["location"]
	return fmt.Sprintf("%s|%s|%s|%s",
		normalizeKeyPart(f.Target),
		normalizeKeyPart(f.FindingType),
		normalizeKeyPart(location),
		normalizeKeyPart(f.Title),
	)
}

func artifactFromEvent(event EventEnvelope) (Artifact, error) {
	payload := map[string]any{}
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return Artifact{}, fmt.Errorf("parse artifact payload: %w", err)
		}
	}
	artifact := Artifact{
		RunID:    event.RunID,
		TaskID:   event.TaskID,
		Type:     stringFromPayload(payload, "type", "generic"),
		Title:    stringFromPayload(payload, "title", event.EventID),
		Path:     stringFromPayload(payload, "path", ""),
		Metadata: metadataFromPayload(payload["metadata"]),
	}
	return artifact, nil
}

func findingFromEvent(event EventEnvelope) (Finding, error) {
	payload := map[string]any{}
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return Finding{}, fmt.Errorf("parse finding payload: %w", err)
		}
	}
	metadata := metadataFromPayload(payload["metadata"])
	if _, ok := metadata["location"]; !ok {
		if location := stringFromPayload(payload, "location", ""); location != "" {
			metadata["location"] = location
		}
	}
	state := normalizeFindingState(stringFromPayload(payload, "state", ""))
	if state == "" {
		state = normalizeFindingState(metadata["finding_state"])
	}
	if state == "" {
		state = FindingStateCandidate
	}
	metadata["finding_state"] = state
	source := stringFromPayload(payload, "source", "")
	if source == "" {
		source = event.WorkerID
	}
	severity := stringFromPayload(payload, "severity", "info")
	confidence := stringFromPayload(payload, "confidence", "medium")
	return Finding{
		RunID:       event.RunID,
		TaskID:      event.TaskID,
		Target:      stringFromPayload(payload, "target", ""),
		FindingType: stringFromPayload(payload, "finding_type", "generic"),
		Title:       stringFromPayload(payload, "title", event.EventID),
		State:       state,
		Severity:    severity,
		Confidence:  confidence,
		Evidence:    evidenceFromPayload(payload["evidence"]),
		Metadata:    metadata,
		Sources: []FindingSource{
			{
				EventID:    event.EventID,
				WorkerID:   event.WorkerID,
				TaskID:     event.TaskID,
				Source:     source,
				Severity:   severity,
				Confidence: confidence,
				TS:         event.TS,
			},
		},
	}, nil
}

func mergeFindings(existing, incoming Finding) Finding {
	merged := existing
	if merged.RunID == "" {
		merged.RunID = incoming.RunID
	}
	if merged.TaskID == "" {
		merged.TaskID = incoming.TaskID
	}
	if merged.Target == "" {
		merged.Target = incoming.Target
	}
	if merged.FindingType == "" {
		merged.FindingType = incoming.FindingType
	}
	if merged.Title == "" {
		merged.Title = incoming.Title
	}
	if merged.State == "" {
		merged.State = incoming.State
	}
	merged.Sources = mergeSources(merged.Sources, incoming.Sources)
	merged.Evidence = appendUnique(merged.Evidence, incoming.Evidence...)
	if merged.Metadata == nil {
		merged.Metadata = map[string]string{}
	}

	if merged.Confidence == "" {
		merged.Confidence = incoming.Confidence
	} else if incoming.Confidence != "" && !strings.EqualFold(merged.Confidence, incoming.Confidence) {
		resolution := merged.Confidence
		if confidenceRank(incoming.Confidence) > confidenceRank(merged.Confidence) {
			resolution = incoming.Confidence
			merged.Confidence = incoming.Confidence
		}
		merged.Conflicts = appendConflict(merged.Conflicts, FindingConflict{
			Field:          "confidence",
			ExistingValue:  existing.Confidence,
			IncomingValue:  incoming.Confidence,
			IncomingEvent:  firstIncomingEvent(incoming.Sources),
			IncomingSource: firstIncomingSource(incoming.Sources),
			Resolution:     resolution,
		})
	}
	if merged.Severity == "" {
		merged.Severity = incoming.Severity
	} else if incoming.Severity != "" && !strings.EqualFold(merged.Severity, incoming.Severity) {
		resolution := merged.Severity
		if severityRank(incoming.Severity) > severityRank(merged.Severity) {
			resolution = incoming.Severity
			merged.Severity = incoming.Severity
		}
		merged.Conflicts = appendConflict(merged.Conflicts, FindingConflict{
			Field:          "severity",
			ExistingValue:  existing.Severity,
			IncomingValue:  incoming.Severity,
			IncomingEvent:  firstIncomingEvent(incoming.Sources),
			IncomingSource: firstIncomingSource(incoming.Sources),
			Resolution:     resolution,
		})
	}
	if incoming.Title != "" && !strings.EqualFold(merged.Title, incoming.Title) {
		resolution := merged.Title
		if confidenceRank(incoming.Confidence) > confidenceRank(existing.Confidence) {
			resolution = incoming.Title
			merged.Title = incoming.Title
		}
		merged.Conflicts = appendConflict(merged.Conflicts, FindingConflict{
			Field:          "title",
			ExistingValue:  existing.Title,
			IncomingValue:  incoming.Title,
			IncomingEvent:  firstIncomingEvent(incoming.Sources),
			IncomingSource: firstIncomingSource(incoming.Sources),
			Resolution:     resolution,
		})
	}
	if normalizedIncomingState := normalizeFindingState(incoming.State); normalizedIncomingState != "" {
		normalizedExistingState := normalizeFindingState(merged.State)
		if normalizedExistingState == "" {
			normalizedExistingState = FindingStateCandidate
		}
		merged.State = normalizedExistingState
		if normalizedExistingState != normalizedIncomingState {
			resolution := normalizedExistingState
			if findingStateRank(normalizedIncomingState) >= findingStateRank(normalizedExistingState) {
				resolution = normalizedIncomingState
				merged.State = normalizedIncomingState
			}
			merged.Conflicts = appendConflict(merged.Conflicts, FindingConflict{
				Field:          "state",
				ExistingValue:  normalizedExistingState,
				IncomingValue:  normalizedIncomingState,
				IncomingEvent:  firstIncomingEvent(incoming.Sources),
				IncomingSource: firstIncomingSource(incoming.Sources),
				Resolution:     resolution,
			})
		}
	}

	for k, v := range incoming.Metadata {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(k), "finding_state") {
			merged.Metadata["finding_state"] = normalizeFindingState(merged.State)
			continue
		}
		current, exists := merged.Metadata[k]
		if !exists {
			merged.Metadata[k] = v
			continue
		}
		if strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(v)) {
			continue
		}
		resolution := current
		if confidenceRank(incoming.Confidence) > confidenceRank(existing.Confidence) {
			resolution = v
			merged.Metadata[k] = v
		}
		merged.Conflicts = appendConflict(merged.Conflicts, FindingConflict{
			Field:          "metadata." + k,
			ExistingValue:  current,
			IncomingValue:  v,
			IncomingEvent:  firstIncomingEvent(incoming.Sources),
			IncomingSource: firstIncomingSource(incoming.Sources),
			Resolution:     resolution,
		})
	}
	merged.State = normalizeFindingState(merged.State)
	if merged.State == "" {
		merged.State = FindingStateCandidate
	}
	merged.Metadata["finding_state"] = merged.State
	return merged
}

func readFinding(path string) (Finding, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Finding{}, false, nil
		}
		return Finding{}, false, err
	}
	var finding Finding
	if err := json.Unmarshal(data, &finding); err != nil {
		return Finding{}, false, err
	}
	if finding.Metadata == nil {
		finding.Metadata = map[string]string{}
	}
	finding.State = normalizeFindingState(finding.State)
	if finding.State == "" {
		finding.State = normalizeFindingState(finding.Metadata["finding_state"])
	}
	if finding.State == "" {
		finding.State = FindingStateCandidate
	}
	finding.Metadata["finding_state"] = finding.State
	return finding, true, nil
}

func normalizeFindingState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case FindingStateHypothesis, "suspected":
		return FindingStateHypothesis
	case FindingStateCandidate, "candidate", "candidate-finding":
		return FindingStateCandidate
	case FindingStateVerified, "verified", "confirmed", "verified-finding":
		return FindingStateVerified
	case FindingStateRejected, "rejected", "false_positive", "false-positive", "rejected-finding":
		return FindingStateRejected
	default:
		return ""
	}
}

func findingStateRank(state string) int {
	switch normalizeFindingState(state) {
	case FindingStateRejected:
		return 4
	case FindingStateVerified:
		return 3
	case FindingStateCandidate:
		return 2
	case FindingStateHypothesis:
		return 1
	default:
		return 0
	}
}

func findingIsVerified(f Finding) bool {
	return normalizeFindingState(f.State) == FindingStateVerified
}

func stringFromPayload(payload map[string]any, key, fallback string) string {
	v, _ := payload[key].(string)
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func evidenceFromPayload(v any) []string {
	switch e := v.(type) {
	case []string:
		return appendUnique(nil, e...)
	case []any:
		out := make([]string, 0, len(e))
		for _, item := range e {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return appendUnique(nil, out...)
	default:
		return nil
	}
}

func metadataFromPayload(v any) map[string]string {
	out := map[string]string{}
	switch meta := v.(type) {
	case map[string]string:
		for k, val := range meta {
			out[strings.TrimSpace(k)] = strings.TrimSpace(val)
		}
	case map[string]any:
		for k, val := range meta {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			out[key] = strings.TrimSpace(fmt.Sprint(val))
		}
	}
	return out
}

func appendUnique(base []string, values ...string) []string {
	out := append([]string{}, base...)
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		if slices.Contains(out, v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

func normalizeKeyPart(v string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(v)), " "))
}

func hashKey(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:16])
}

func (m *Manager) ListFindings(runID string) ([]Finding, error) {
	paths, err := filepath.Glob(filepath.Join(BuildRunPaths(m.SessionsDir, runID).FindingDir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]Finding, 0, len(paths))
	for _, path := range paths {
		finding, ok, err := readFinding(path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, finding)
	}
	return out, nil
}

func (m *Manager) CountEvidence(runID string) (artifacts int, findings int, err error) {
	artifactPaths, err := filepath.Glob(filepath.Join(BuildRunPaths(m.SessionsDir, runID).ArtifactDir, "*.json"))
	if err != nil {
		return 0, 0, err
	}
	findingPaths, err := filepath.Glob(filepath.Join(BuildRunPaths(m.SessionsDir, runID).FindingDir, "*.json"))
	if err != nil {
		return 0, 0, err
	}
	return len(artifactPaths), len(findingPaths), nil
}

func mergeSources(existing, incoming []FindingSource) []FindingSource {
	out := append([]FindingSource{}, existing...)
	byEvent := map[string]struct{}{}
	for _, s := range out {
		if s.EventID != "" {
			byEvent[s.EventID] = struct{}{}
		}
	}
	for _, s := range incoming {
		if s.EventID != "" {
			if _, seen := byEvent[s.EventID]; seen {
				continue
			}
			byEvent[s.EventID] = struct{}{}
		}
		out = append(out, s)
	}
	return out
}

func appendConflict(existing []FindingConflict, conflict FindingConflict) []FindingConflict {
	for _, c := range existing {
		if c.Field == conflict.Field &&
			strings.EqualFold(c.ExistingValue, conflict.ExistingValue) &&
			strings.EqualFold(c.IncomingValue, conflict.IncomingValue) &&
			c.IncomingEvent == conflict.IncomingEvent &&
			strings.EqualFold(c.Resolution, conflict.Resolution) {
			return existing
		}
	}
	return append(existing, conflict)
}

func firstIncomingEvent(sources []FindingSource) string {
	if len(sources) == 0 {
		return ""
	}
	return sources[0].EventID
}

func firstIncomingSource(sources []FindingSource) string {
	if len(sources) == 0 {
		return ""
	}
	return sources[0].Source
}

func confidenceRank(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "high", "confirmed":
		return 3
	case "medium", "probable":
		return 2
	case "low", "possible":
		return 1
	default:
		return 0
	}
}

func severityRank(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "info", "informational":
		return 1
	default:
		return 0
	}
}
