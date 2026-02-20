package orchestrator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func isBuiltinListDir(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "list_dir", "ls", "dir":
		return true
	default:
		return false
	}
}

func isBuiltinReadFile(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "read_file", "read":
		return true
	default:
		return false
	}
}

func isBuiltinWriteFile(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "write_file", "write":
		return true
	default:
		return false
	}
}

func isBuiltinBrowse(command string) bool {
	return strings.EqualFold(strings.TrimSpace(command), "browse")
}

func builtinListDir(args []string, workDir string) ([]byte, error) {
	target := "."
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		target = arg
		break
	}
	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []byte(fmt.Sprintf("Path: %s\nType: file\nName: %s\nBytes: %d\n", path, info.Name(), info.Size())), nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Path: %s\nEntries: %d\n", path, len(names))
	for _, name := range names {
		b.WriteString("- " + name + "\n")
	}
	return capBytes([]byte(b.String()), workerAssistOutputLimit), nil
}

func builtinReadFile(args []string, workDir string) ([]byte, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, fmt.Errorf("read_file requires a path argument")
	}
	path := strings.TrimSpace(args[0])
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	path = filepath.Clean(path)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, workerAssistReadMaxBytes))
	if err != nil {
		return nil, err
	}
	return capBytes(data, workerAssistOutputLimit), nil
}

func builtinWriteFile(args []string, workDir string) ([]byte, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("write_file requires <path> <content>")
	}
	target := strings.TrimSpace(args[0])
	if target == "" {
		return nil, fmt.Errorf("write_file target is empty")
	}
	if filepath.IsAbs(target) {
		return nil, fmt.Errorf("write_file requires relative path")
	}
	path := filepath.Clean(filepath.Join(workDir, target))
	if !pathWithinRoot(path, workDir) {
		return nil, fmt.Errorf("write_file path escapes workspace")
	}
	content := strings.Join(args[1:], " ")
	if len(content) > workerAssistWriteMaxBytes {
		return nil, fmt.Errorf("write_file content too large")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("Wrote %d bytes to %s\n", len(content), path)), nil
}

func pathWithinRoot(candidate, root string) bool {
	candidate = filepath.Clean(candidate)
	root = filepath.Clean(root)
	if candidate == root {
		return true
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func builtinBrowse(ctx context.Context, args []string) ([]byte, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, fmt.Errorf("browse requires a URL")
	}
	target := strings.TrimSpace(args[0])
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "BirdHackBot-Orchestrator/1.0")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, workerAssistBrowseMaxBody))
	if err != nil {
		return nil, err
	}
	title := ""
	if match := htmlTitlePattern.FindSubmatch(body); len(match) > 1 {
		title = strings.TrimSpace(strings.ReplaceAll(string(match[1]), "\n", " "))
	}
	snippet := strings.TrimSpace(string(body))
	snippet = strings.Join(strings.Fields(snippet), " ")
	if len(snippet) > 800 {
		snippet = snippet[:800] + "..."
	}
	var out strings.Builder
	_, _ = fmt.Fprintf(&out, "URL: %s\nStatus: %d\n", target, resp.StatusCode)
	if title != "" {
		_, _ = fmt.Fprintf(&out, "Title: %s\n", title)
	}
	if snippet != "" {
		_, _ = fmt.Fprintf(&out, "Snippet: %s\n", snippet)
	}
	return capBytes([]byte(out.String()), workerAssistOutputLimit), nil
}
