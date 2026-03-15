package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func DefaultPlanContent() string {
	return fmt.Sprintf("# Session Plan\n\n- Started (UTC): %s\n- Scope:\n- Targets:\n- Notes:\n\n## Phases\n- Recon:\n- Enumeration:\n- Validation:\n- Escalation:\n- Reporting:\n", time.Now().UTC().Format(time.RFC3339))
}

func EnsurePlan(sessionDir, filename string) (string, error) {
	if filename == "" {
		filename = "plan.md"
	}
	if sessionDir == "" {
		return "", fmt.Errorf("session dir is empty")
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}
	planPath := filepath.Join(sessionDir, filename)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		if err := os.WriteFile(planPath, []byte(DefaultPlanContent()), 0o644); err != nil {
			return "", fmt.Errorf("write plan: %w", err)
		}
	}
	return planPath, nil
}

func AppendPlan(sessionDir, filename, content string) (string, error) {
	planPath, err := EnsurePlan(sessionDir, filename)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", fmt.Errorf("plan content is empty")
	}
	update := fmt.Sprintf("\n\n## Update (UTC): %s\n\n%s\n", time.Now().UTC().Format(time.RFC3339), trimmed)
	file, err := os.OpenFile(planPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("open plan: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(update); err != nil {
		return "", fmt.Errorf("append plan: %w", err)
	}
	return planPath, nil
}
