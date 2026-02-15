package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (r *Runner) handleScript(args []string) error {
	r.setTask("script")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: scripts not permitted")
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: /script <py|sh> <name>")
	}
	lang := strings.ToLower(args[0])
	name := sanitizeFilename(args[1])
	if name == "" {
		return fmt.Errorf("invalid script name")
	}
	var ext string
	var interpreter string
	switch lang {
	case "py", "python":
		ext = ".py"
		interpreter = "python"
	case "sh", "bash":
		ext = ".sh"
		interpreter = "bash"
	default:
		return fmt.Errorf("unsupported script type: %s", lang)
	}

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	artifactsDir := filepath.Join(sessionDir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return fmt.Errorf("create artifacts dir: %w", err)
	}
	scriptPath := filepath.Join(artifactsDir, name+ext)

	r.logger.Printf("Enter %s script content. End with a single '.' line.", lang)
	lines := []string{}
	for {
		line, err := r.readLine("> ")
		if err != nil && err != io.EOF {
			return err
		}
		if strings.TrimSpace(line) == "." {
			break
		}
		lines = append(lines, line)
		if err == io.EOF {
			break
		}
	}
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("script content is empty")
	}
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write script: %w", err)
	}
	r.logger.Printf("Script saved: %s", scriptPath)

	runArgs := []string{interpreter, scriptPath}
	return r.handleRun(runArgs)
}

func (r *Runner) handleClean(args []string) error {
	r.setTask("clean")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: clean not permitted")
	}

	days := 0
	if len(args) > 0 {
		value, err := strconv.Atoi(args[0])
		if err != nil || value < 0 {
			return fmt.Errorf("usage: /clean [days]")
		}
		days = value
	}

	root := r.cfg.Session.LogDir
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			r.logger.Printf("No sessions to clean.")
			return nil
		}
		return fmt.Errorf("read sessions: %w", err)
	}

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if days == 0 || info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(path); err != nil {
				r.logger.Printf("Failed to remove %s: %v", path, err)
				continue
			}
			removed++
		}
	}
	r.logger.Printf("Cleaned %d session(s).", removed)
	return nil
}
