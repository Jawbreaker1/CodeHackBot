package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerIngestEvidenceWritesArtifactAndFinding(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-evidence-1"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	now := time.Now().UTC()
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e1",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "task-1",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskArtifact,
		Payload: mustJSONRaw(map[string]any{
			"type":  "log",
			"title": "nmap output",
			"path":  "sessions/x/logs/nmap.log",
			"metadata": map[string]any{
				"tool": "nmap",
			},
		}),
	}); err != nil {
		t.Fatalf("append artifact event: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e2",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "task-1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.20",
			"finding_type": "open_port",
			"title":        "SSH service exposed",
			"severity":     "medium",
			"confidence":   "high",
			"location":     "22/tcp",
			"evidence":     []any{"nmap:22 open"},
		}),
	}); err != nil {
		t.Fatalf("append finding event: %v", err)
	}

	res, err := manager.IngestEvidence(runID)
	if err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	if res.ArtifactsWritten != 1 || res.FindingsWritten != 1 {
		t.Fatalf("unexpected ingest result: %+v", res)
	}

	artifactPath := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, "e1.json")
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read artifact file: %v", err)
	}
	var artifact Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	if artifact.Type != "log" || artifact.TaskID != "task-1" {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}

	findingFiles, err := filepath.Glob(filepath.Join(BuildRunPaths(base, runID).FindingDir, "*.json"))
	if err != nil {
		t.Fatalf("glob finding files: %v", err)
	}
	if len(findingFiles) != 1 {
		t.Fatalf("expected 1 finding file, got %d", len(findingFiles))
	}
	findingData, err := os.ReadFile(findingFiles[0])
	if err != nil {
		t.Fatalf("read finding file: %v", err)
	}
	var finding Finding
	if err := json.Unmarshal(findingData, &finding); err != nil {
		t.Fatalf("unmarshal finding: %v", err)
	}
	if finding.Target != "192.168.50.20" {
		t.Fatalf("unexpected finding target: %+v", finding)
	}
	if finding.State != FindingStateCandidate {
		t.Fatalf("expected default finding state %q, got %q", FindingStateCandidate, finding.State)
	}
	if finding.Metadata["finding_state"] != FindingStateCandidate {
		t.Fatalf("expected finding_state metadata %q, got %q", FindingStateCandidate, finding.Metadata["finding_state"])
	}
	if finding.Metadata["dedupe_key"] == "" {
		t.Fatalf("expected dedupe key in finding metadata")
	}
}

func TestManagerIngestEvidenceDedupesFindingsByKey(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-evidence-dedupe"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	now := time.Now().UTC()
	appendFindingEvent := func(eventID string, seq int64, evidence string) {
		t.Helper()
		if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
			EventID:  eventID,
			RunID:    runID,
			WorkerID: "worker-1",
			TaskID:   "task-1",
			Seq:      seq,
			TS:       now.Add(time.Duration(seq) * time.Second),
			Type:     EventTypeTaskFinding,
			Payload: mustJSONRaw(map[string]any{
				"target":       "192.168.50.21",
				"finding_type": "web_vuln",
				"title":        "Reflected XSS at /search",
				"location":     "/search?q",
				"evidence":     []any{evidence},
			}),
		}); err != nil {
			t.Fatalf("append finding event %s: %v", eventID, err)
		}
	}
	appendFindingEvent("e1", 1, "payload one")
	appendFindingEvent("e2", 2, "payload two")

	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	findingFiles, err := filepath.Glob(filepath.Join(BuildRunPaths(base, runID).FindingDir, "*.json"))
	if err != nil {
		t.Fatalf("glob finding files: %v", err)
	}
	if len(findingFiles) != 1 {
		t.Fatalf("expected 1 deduped finding file, got %d", len(findingFiles))
	}
	data, err := os.ReadFile(findingFiles[0])
	if err != nil {
		t.Fatalf("read finding file: %v", err)
	}
	var finding Finding
	if err := json.Unmarshal(data, &finding); err != nil {
		t.Fatalf("unmarshal finding: %v", err)
	}
	if len(finding.Evidence) != 2 {
		t.Fatalf("expected merged evidence size 2, got %d (%v)", len(finding.Evidence), finding.Evidence)
	}
}

func TestManagerIngestEvidenceIsIdempotent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-evidence-idempotent"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	now := time.Now().UTC()
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-artifact",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "task-1",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskArtifact,
		Payload:  mustJSONRaw(map[string]any{"type": "log", "title": "one"}),
	}); err != nil {
		t.Fatalf("append artifact event: %v", err)
	}

	first, err := manager.IngestEvidence(runID)
	if err != nil {
		t.Fatalf("first IngestEvidence: %v", err)
	}
	second, err := manager.IngestEvidence(runID)
	if err != nil {
		t.Fatalf("second IngestEvidence: %v", err)
	}
	if first.ArtifactsWritten != 1 {
		t.Fatalf("expected first write count 1, got %+v", first)
	}
	if second.ArtifactsWritten != 0 || second.FindingsWritten != 0 {
		t.Fatalf("expected idempotent second ingest with no writes, got %+v", second)
	}
}

func TestManagerIngestEvidenceRetainsConflictingEvidenceBySourceAndConfidence(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-evidence-conflicts"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	now := time.Now().UTC()
	appendFindingEvent := func(eventID string, seq int64, source, confidence, severity, tech string) {
		t.Helper()
		if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
			EventID:  eventID,
			RunID:    runID,
			WorkerID: "worker-1",
			TaskID:   "task-1",
			Seq:      seq,
			TS:       now.Add(time.Duration(seq) * time.Second),
			Type:     EventTypeTaskFinding,
			Payload: mustJSONRaw(map[string]any{
				"target":       "192.168.50.30",
				"finding_type": "service_fingerprint",
				"title":        "Service fingerprint mismatch",
				"location":     "443/tcp",
				"severity":     severity,
				"confidence":   confidence,
				"source":       source,
				"metadata": map[string]any{
					"tech": tech,
				},
				"evidence": []any{"fingerprint:" + tech},
			}),
		}); err != nil {
			t.Fatalf("append finding event %s: %v", eventID, err)
		}
	}
	appendFindingEvent("e-low", 1, "nmap", "low", "low", "apache")
	appendFindingEvent("e-high", 2, "tls-scan", "high", "medium", "nginx")

	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	findingFiles, err := filepath.Glob(filepath.Join(BuildRunPaths(base, runID).FindingDir, "*.json"))
	if err != nil {
		t.Fatalf("glob finding files: %v", err)
	}
	if len(findingFiles) != 1 {
		t.Fatalf("expected 1 finding file, got %d", len(findingFiles))
	}
	data, err := os.ReadFile(findingFiles[0])
	if err != nil {
		t.Fatalf("read finding file: %v", err)
	}
	var finding Finding
	if err := json.Unmarshal(data, &finding); err != nil {
		t.Fatalf("unmarshal finding: %v", err)
	}
	if finding.Confidence != "high" {
		t.Fatalf("expected confidence to resolve to high, got %q", finding.Confidence)
	}
	if finding.Severity != "medium" {
		t.Fatalf("expected severity to resolve to medium, got %q", finding.Severity)
	}
	if finding.Metadata["tech"] != "nginx" {
		t.Fatalf("expected metadata.tech to resolve by higher confidence source, got %q", finding.Metadata["tech"])
	}
	if len(finding.Sources) != 2 {
		t.Fatalf("expected 2 sources retained, got %d", len(finding.Sources))
	}
	if len(finding.Conflicts) == 0 {
		t.Fatalf("expected conflict records to be retained")
	}
}

func TestManagerIngestEvidenceResolvesFindingLifecycleState(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-evidence-state-resolution"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	now := time.Now().UTC()
	appendFindingEvent := func(eventID string, seq int64, state string) {
		t.Helper()
		if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
			EventID:  eventID,
			RunID:    runID,
			WorkerID: "worker-1",
			TaskID:   "task-1",
			Seq:      seq,
			TS:       now.Add(time.Duration(seq) * time.Second),
			Type:     EventTypeTaskFinding,
			Payload: mustJSONRaw(map[string]any{
				"target":       "192.168.50.22",
				"finding_type": "web_vuln",
				"title":        "Lifecycle state conflict",
				"location":     "/admin",
				"state":        state,
				"evidence":     []any{"state=" + state},
			}),
		}); err != nil {
			t.Fatalf("append finding event %s: %v", eventID, err)
		}
	}
	appendFindingEvent("e-candidate", 1, FindingStateCandidate)
	appendFindingEvent("e-rejected", 2, FindingStateRejected)

	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	findingFiles, err := filepath.Glob(filepath.Join(BuildRunPaths(base, runID).FindingDir, "*.json"))
	if err != nil {
		t.Fatalf("glob finding files: %v", err)
	}
	if len(findingFiles) != 1 {
		t.Fatalf("expected 1 finding file, got %d", len(findingFiles))
	}
	data, err := os.ReadFile(findingFiles[0])
	if err != nil {
		t.Fatalf("read finding file: %v", err)
	}
	var finding Finding
	if err := json.Unmarshal(data, &finding); err != nil {
		t.Fatalf("unmarshal finding: %v", err)
	}
	if finding.State != FindingStateRejected {
		t.Fatalf("expected merged state %q, got %q", FindingStateRejected, finding.State)
	}
	if finding.Metadata["finding_state"] != FindingStateRejected {
		t.Fatalf("expected finding_state metadata %q, got %q", FindingStateRejected, finding.Metadata["finding_state"])
	}
}
