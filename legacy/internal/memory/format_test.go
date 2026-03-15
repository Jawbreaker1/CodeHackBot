package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSummaryLimitsItems(t *testing.T) {
	temp := t.TempDir()
	path := filepath.Join(temp, "summary.md")
	items := []string{}
	for i := 0; i < maxSummaryItems+5; i++ {
		items = append(items, "item")
		items[i] = items[i] + string(rune('A'+(i%26)))
	}
	if err := WriteSummary(path, items); err != nil {
		t.Fatalf("WriteSummary error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	count := strings.Count(string(data), "\n- ")
	if count > maxSummaryItems {
		t.Fatalf("expected <= %d items, got %d", maxSummaryItems, count)
	}
}

func TestWriteKnownFactsLimitsItems(t *testing.T) {
	temp := t.TempDir()
	path := filepath.Join(temp, "facts.md")
	items := []string{}
	for i := 0; i < maxFactItems+3; i++ {
		items = append(items, "fact")
		items[i] = items[i] + string(rune('A'+(i%26)))
	}
	if err := WriteKnownFacts(path, items); err != nil {
		t.Fatalf("WriteKnownFacts error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	count := strings.Count(string(data), "\n- ")
	if count > maxFactItems {
		t.Fatalf("expected <= %d items, got %d", maxFactItems, count)
	}
}
