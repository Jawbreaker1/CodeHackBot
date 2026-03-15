package orchestrator

import (
	"fmt"
	"strings"
	"testing"
)

func TestAppendObservationCompactsWithSignalPriority(t *testing.T) {
	observations := []string{}
	for i := 0; i < 64; i++ {
		entry := "note: generic progress update with verbose text and no strong anchors " + strings.Repeat("x", 120)
		observations = appendObservation(observations, entry)
		if i == 5 {
			observations = appendObservation(observations, "command failed: nmap -sV 192.168.50.1 | output: timeout while connecting")
		}
		if i == 7 {
			observations = appendObservation(observations, "command ok: read_file /tmp/secret.zip.hash | output: hash loaded")
		}
	}
	if len(observations) > workerAssistObsLimit {
		t.Fatalf("expected compacted observations <= %d, got %d", workerAssistObsLimit, len(observations))
	}
	if got := observationTokens(observations); got > workerAssistObsTokenBudget {
		t.Fatalf("expected token budget <= %d, got %d", workerAssistObsTokenBudget, got)
	}
	joined := strings.Join(observations, "\n")
	if !strings.Contains(joined, "192.168.50.1") {
		t.Fatalf("expected retained target signal in observations")
	}
	if !strings.Contains(joined, "/tmp/secret.zip.hash") {
		t.Fatalf("expected retained path signal in observations")
	}
	if !strings.Contains(strings.ToLower(joined), "timeout") {
		t.Fatalf("expected retained error signal in observations")
	}
}

func TestAppendObservationAddsCompactionSummaryWhenSignalsDropped(t *testing.T) {
	observations := []string{}
	for i := 0; i < 48; i++ {
		path := fmt.Sprintf("/tmp/log-%02d.txt", i)
		target := fmt.Sprintf("10.0.0.%d", (i%8)+1)
		observations = appendObservation(observations, fmt.Sprintf("command failed: check %s on %s | output: timeout", path, target))
	}
	joined := strings.Join(observations, "\n")
	if !strings.Contains(joined, "compaction_summary:") {
		t.Fatalf("expected compaction summary in retained observations")
	}
	if !strings.Contains(joined, "paths=") && !strings.Contains(joined, "targets=") && !strings.Contains(joined, "errors=") {
		t.Fatalf("expected compaction summary to retain at least one signal class")
	}
}

func TestNormalizeObservationEntryCompactsLongStrings(t *testing.T) {
	entry := "command ok: " + strings.Repeat("a", workerAssistObsMaxEntryChars+200) + " /tmp/secret.zip"
	normalized := normalizeObservationEntry(entry)
	if len(normalized) > workerAssistObsMaxEntryChars {
		t.Fatalf("expected normalized entry length <= %d, got %d", workerAssistObsMaxEntryChars, len(normalized))
	}
	if !strings.Contains(normalized, "command ok:") {
		t.Fatalf("expected leading context retained")
	}
	if !strings.Contains(normalized, "/tmp/secret.zip") {
		t.Fatalf("expected trailing anchor retained")
	}
}
