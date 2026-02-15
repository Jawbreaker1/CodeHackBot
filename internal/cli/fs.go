package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	readFileMaxBytes   = 32_000
	listDirMaxEntries  = 250
	listDirNameMaxLen  = 120
	readFileLineBudget = 200
)

func (r *Runner) handleReadFile(args []string) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: /read <path>")
	}

	path, err := r.resolveAllowedPath(sessionDir, args[0])
	if err != nil {
		return err
	}

	r.setTask("read_file")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read_file: %w", err)
	}
	if len(data) > readFileMaxBytes {
		data = data[:readFileMaxBytes]
	}
	excerpt := firstLinesBytes(data, readFileLineBudget)

	logPath, logErr := writeFSLog(sessionDir, "read", fmt.Sprintf("Path: %s\nBytes: %d\n\n%s\n", path, len(data), excerpt))
	if logErr != nil {
		r.logger.Printf("read_file log save failed: %v", logErr)
	}

	fmt.Printf("Path: %s\n", path)
	if logPath != "" {
		fmt.Printf("Log saved: %s\n", logPath)
	}
	if strings.TrimSpace(excerpt) != "" {
		fmt.Println("Excerpt:")
		fmt.Println(excerpt)
	}

	r.recordActionArtifact(logPath)
	r.recordObservation("read_file", []string{path}, logPath, truncate(excerpt, 900), nil)
	r.maybeAutoSummarize(logPath, "read_file")
	return nil
}

func (r *Runner) handleListDir(args []string) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	target := "."
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		target = args[0]
	}
	path, err := r.resolveAllowedPath(sessionDir, target)
	if err != nil {
		return err
	}

	r.setTask("list_dir")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("list_dir: %w", err)
	}
	if len(entries) > listDirMaxEntries {
		entries = entries[:listDirMaxEntries]
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	_, _ = fmt.Fprintf(&buf, "Path: %s\nEntries: %d\n\n", path, len(names))
	for _, name := range names {
		if len(name) > listDirNameMaxLen {
			name = name[:listDirNameMaxLen] + "..."
		}
		_, _ = io.WriteString(&buf, name+"\n")
	}

	logPath, logErr := writeFSLog(sessionDir, "ls", buf.String())
	if logErr != nil {
		r.logger.Printf("list_dir log save failed: %v", logErr)
	}

	fmt.Printf("Path: %s\n", path)
	if logPath != "" {
		fmt.Printf("Log saved: %s\n", logPath)
	}
	if len(names) > 0 {
		fmt.Println("Entries:")
		for i, name := range names {
			if i >= 60 {
				fmt.Println("...")
				break
			}
			fmt.Println("- " + name)
		}
	}

	r.recordActionArtifact(logPath)
	r.recordObservation("list_dir", []string{path}, logPath, fmt.Sprintf("entries=%d", len(names)), nil)
	r.maybeAutoSummarize(logPath, "list_dir")
	return nil
}

func (r *Runner) resolveAllowedPath(sessionDir, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is empty")
	}
	wd := r.currentWorkingDir()
	if wd == "" {
		wd = "."
	}
	candidate := raw
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(wd, candidate)
	}
	candidate = filepath.Clean(candidate)

	allowedRoots := []string{filepath.Clean(sessionDir)}
	if wdClean := filepath.Clean(wd); wdClean != "" {
		allowedRoots = append(allowedRoots, wdClean)
	}

	for _, root := range allowedRoots {
		if pathWithinRoot(candidate, root) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("path out of bounds: %s (allowed: session dir or current working dir)", raw)
}

func pathWithinRoot(path string, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func writeFSLog(sessionDir, prefix, content string) (string, error) {
	logDir := filepath.Join(sessionDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	timestamp := time.Now().UTC().Format("20060102-150405.000000000")
	path := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", prefix, timestamp))
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func firstLinesBytes(data []byte, maxLines int) string {
	if maxLines <= 0 {
		maxLines = readFileLineBudget
	}
	lines := 0
	for i, b := range data {
		if b == '\n' {
			lines++
			if lines >= maxLines {
				return string(bytes.TrimSpace(data[:i]))
			}
		}
	}
	return string(bytes.TrimSpace(data))
}
