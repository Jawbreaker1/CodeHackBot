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
	writeFileMaxBytes  = 512_000
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

func (r *Runner) handleWriteFile(args []string) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: write_file not permitted")
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: /write <relative_path_under_tools_dir> [content]")
	}

	relPath := strings.TrimSpace(args[0])
	content := ""
	if len(args) > 1 {
		// Allow multi-line content inside a single JSON arg.
		content = strings.Join(args[1:], " ")
	} else {
		// Interactive mode for manual usage; end with '.' like /script.
		r.logger.Printf("Enter file content. End with a single '.' line.")
		lines := []string{}
		for {
			line, readErr := r.readLine("> ")
			if readErr != nil && readErr != io.EOF {
				return readErr
			}
			if strings.TrimSpace(line) == "." {
				break
			}
			lines = append(lines, line)
			if readErr == io.EOF {
				break
			}
		}
		content = strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("write_file: content is empty")
	}
	if len(content) > writeFileMaxBytes {
		return fmt.Errorf("write_file: content too large (%d bytes > %d)", len(content), writeFileMaxBytes)
	}

	outPath, err := r.resolveToolWritePath(sessionDir, relPath)
	if err != nil {
		return err
	}

	requireApproval := r.cfg.Permissions.Level == "default" && r.cfg.Permissions.RequireApproval
	if requireApproval {
		ok, err := r.confirm(fmt.Sprintf("Write file: %s?", outPath))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("write not approved")
		}
	}

	r.setTask("write_file")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("write_file mkdir: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write_file: %w", err)
	}

	excerpt := content
	if len(excerpt) > 2000 {
		excerpt = excerpt[:2000] + "..."
	}
	logPath, logErr := writeFSLog(sessionDir, "write", fmt.Sprintf("Path: %s\nBytes: %d\n\n%s\n", outPath, len(content), excerpt))
	if logErr != nil {
		r.logger.Printf("write_file log save failed: %v", logErr)
	}

	fmt.Printf("Wrote: %s\n", outPath)
	if logPath != "" {
		fmt.Printf("Log saved: %s\n", logPath)
	}
	r.recordActionArtifact(logPath)
	r.recordObservation("write_file", []string{outPath}, logPath, fmt.Sprintf("bytes=%d", len(content)), nil)
	r.maybeAutoSummarize(logPath, "write_file")
	return nil
}

func (r *Runner) handleListDir(args []string) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	target := listDirTarget(args)
	path, err := r.resolveAllowedPath(sessionDir, target)
	if err != nil {
		return err
	}

	r.setTask("list_dir")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("list_dir: %w", err)
	}
	if !info.IsDir() {
		content := renderListFileLog(path, info)
		logPath, logErr := writeFSLog(sessionDir, "ls", content)
		if logErr != nil {
			r.logger.Printf("list_dir log save failed: %v", logErr)
		}
		fmt.Printf("Path: %s\n", path)
		if logPath != "" {
			fmt.Printf("Log saved: %s\n", logPath)
		}
		fmt.Println("Entry:")
		fmt.Printf("- %s (%d bytes)\n", filepath.Base(path), info.Size())

		r.recordActionArtifact(logPath)
		r.recordObservation("list_dir", []string{path}, logPath, fmt.Sprintf("file=%s bytes=%d", filepath.Base(path), info.Size()), nil)
		r.maybeAutoSummarize(logPath, "list_dir")
		return nil
	}

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

func renderListFileLog(path string, info os.FileInfo) string {
	var buf bytes.Buffer
	_, _ = fmt.Fprintf(&buf, "Path: %s\nType: file\nName: %s\nBytes: %d\nMode: %s\nModified: %s\n",
		path,
		info.Name(),
		info.Size(),
		info.Mode().String(),
		info.ModTime().UTC().Format(time.RFC3339),
	)
	return buf.String()
}

func listDirTarget(args []string) string {
	target := "."
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		// Ignore ls-style flags (-la, --all) and keep bounded dir listing semantics.
		if isFlagLike(arg) {
			continue
		}
		target = arg
		break
	}
	return target
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

func (r *Runner) resolveToolWritePath(sessionDir, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("write_file requires a relative path under the session tools dir (got absolute path)")
	}
	toolsRoot := filepath.Join(sessionDir, "artifacts", "tools")
	candidate := raw
	// Only allow relative paths under toolsRoot.
	candidate = filepath.Clean(filepath.Join(toolsRoot, candidate))
	if !pathWithinRoot(candidate, toolsRoot) {
		return "", fmt.Errorf("write_file path out of bounds: %s (allowed under %s)", raw, toolsRoot)
	}
	return candidate, nil
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
