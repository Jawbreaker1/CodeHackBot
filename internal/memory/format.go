package memory

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	maxSummaryItems = 40
	maxFactItems    = 80
	maxFocusItems   = 20
)

func ReadBullets(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	items := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "- ") {
			item := strings.TrimSpace(strings.TrimPrefix(line, "- "))
			if item != "" {
				items = append(items, item)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func WriteSummary(path string, bullets []string) error {
	bullets = limitTail(normalizeLines(bullets), maxSummaryItems)
	return writeBullets(path, "Session Summary", bullets, "Summary pending.")
}

func WriteKnownFacts(path string, bullets []string) error {
	bullets = limitTail(normalizeLines(bullets), maxFactItems)
	return writeBullets(path, "Known Facts", bullets, "None recorded.")
}

func WriteFocus(path string, bullets []string) error {
	bullets = limitTail(normalizeLines(bullets), maxFocusItems)
	return writeBullets(path, "Current Focus", bullets, "Not set.")
}

func writeBullets(path, title string, bullets []string, fallback string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	lines := []string{
		fmt.Sprintf("# %s", title),
		"",
	}
	if len(bullets) == 0 {
		lines = append(lines, "- "+fallback)
	} else {
		for _, bullet := range normalizeLines(bullets) {
			lines = append(lines, "- "+bullet)
		}
	}
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func normalizeLines(lines []string) []string {
	seen := map[string]struct{}{}
	normalized := []string{}
	for _, line := range lines {
		item := strings.TrimSpace(line)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func mergeLines(existing, additions []string) []string {
	merged := append([]string{}, existing...)
	for _, item := range additions {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		already := false
		for _, existingItem := range merged {
			if existingItem == item {
				already = true
				break
			}
		}
		if !already {
			merged = append(merged, item)
		}
	}
	return merged
}

func limitTail(lines []string, max int) []string {
	if max <= 0 || len(lines) <= max {
		return lines
	}
	return lines[len(lines)-max:]
}
