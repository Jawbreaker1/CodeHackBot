package playbook

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Name     string
	Path     string
	Title    string
	Content  string
	Keywords []string
}

func Load(dir string) ([]Entry, error) {
	entries := []Entry{}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, fmt.Errorf("stat playbooks: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("playbook path is not a directory: %s", dir)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read playbooks: %w", err)
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(file.Name()), ".md") {
			continue
		}
		path := filepath.Join(dir, file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		title := extractTitle(content, file.Name())
		keywords := buildKeywords(file.Name(), title)
		entries = append(entries, Entry{
			Name:     file.Name(),
			Path:     path,
			Title:    title,
			Content:  content,
			Keywords: keywords,
		})
	}
	return entries, nil
}

func Match(entries []Entry, text string, max int) []Entry {
	text = strings.ToLower(text)
	if strings.TrimSpace(text) == "" || len(entries) == 0 {
		return nil
	}
	type scored struct {
		entry Entry
		score int
	}
	scoredList := []scored{}
	for _, entry := range entries {
		score := 0
		for _, keyword := range entry.Keywords {
			if keyword == "" {
				continue
			}
			if strings.Contains(text, keyword) {
				score++
			}
		}
		if score > 0 {
			scoredList = append(scoredList, scored{entry: entry, score: score})
		}
	}
	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].score == scoredList[j].score {
			return scoredList[i].entry.Name < scoredList[j].entry.Name
		}
		return scoredList[i].score > scoredList[j].score
	})
	if max > 0 && len(scoredList) > max {
		scoredList = scoredList[:max]
	}
	out := make([]Entry, 0, len(scoredList))
	for _, item := range scoredList {
		out = append(out, item.entry)
	}
	return out
}

func Render(entries []Entry, maxLines int) string {
	if len(entries) == 0 {
		return ""
	}
	builder := strings.Builder{}
	for _, entry := range entries {
		builder.WriteString(fmt.Sprintf("- %s (%s)\n", entry.Title, entry.Name))
		builder.WriteString(snippet(entry.Content, maxLines))
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func List(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	builder := strings.Builder{}
	for _, entry := range entries {
		builder.WriteString(fmt.Sprintf("- %s (%s)\n", entry.Title, entry.Name))
	}
	return strings.TrimSpace(builder.String())
}

func snippet(content string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 40
	}
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n")
}

func extractTitle(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			title := strings.TrimSpace(strings.TrimLeft(line, "#"))
			if title != "" {
				return title
			}
		}
	}
	return strings.TrimSuffix(fallback, filepath.Ext(fallback))
}

func buildKeywords(filename, title string) []string {
	words := []string{}
	words = append(words, splitTokens(strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename))))...)
	words = append(words, splitTokens(strings.ToLower(title))...)
	unique := map[string]struct{}{}
	out := []string{}
	for _, word := range words {
		if word == "" || word == "playbook" || word == "lab" {
			continue
		}
		if _, ok := unique[word]; ok {
			continue
		}
		unique[word] = struct{}{}
		out = append(out, word)
	}
	return out
}

func splitTokens(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= '0' && r <= '9' {
			return false
		}
		return true
	})
}
