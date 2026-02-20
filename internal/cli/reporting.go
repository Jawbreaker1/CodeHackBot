package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/report"
)

func (r *Runner) handleReport(args []string) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: report generation not permitted")
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	outPath := filepath.Join(sessionDir, "report.md")
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		resolved, resolveErr := r.resolveReportWritePath(sessionDir, args[0])
		if resolveErr != nil {
			return resolveErr
		}
		outPath = resolved
	}

	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return err
	}
	summary := readFileTrimmed(artifacts.SummaryPath)
	factsText := readFileTrimmed(artifacts.FactsPath)
	focus := readFileTrimmed(artifacts.FocusPath)
	obsText := r.readRecentObservations(artifacts, 12)
	planText := readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.PlanFilename))
	inventoryText := readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.InventoryFilename))

	scope := append([]string{}, r.cfg.Scope.Targets...)
	if len(scope) == 0 {
		if target := strings.TrimSpace(r.bestKnownTarget()); target != "" {
			scope = append(scope, target)
		}
	}
	findings := reportFindingsFromFacts(artifacts.FactsPath, 6)

	ledger := ""
	if r.cfg.Session.LedgerEnabled {
		ledger = readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.LedgerFilename))
	}

	info := report.Info{
		Date:         time.Now().UTC().Format("2006-01-02"),
		Scope:        scope,
		Findings:     findings,
		SessionID:    r.sessionID,
		Ledger:       ledger,
		Summary:      summary,
		KnownFacts:   factsText,
		Focus:        focus,
		Plan:         planText,
		Inventory:    inventoryText,
		Observations: obsText,
	}
	if err := report.Generate("", outPath, info); err != nil {
		return err
	}
	r.logger.Printf("Report generated: %s", outPath)
	return nil
}

func reportFindingsFromFacts(factsPath string, max int) []string {
	if max <= 0 {
		max = 6
	}
	bullets, err := memory.ReadBullets(factsPath)
	if err != nil {
		return nil
	}
	findings := []string{}
	for _, b := range bullets {
		if strings.TrimSpace(b) == "" || strings.EqualFold(strings.TrimSpace(b), "None recorded.") {
			continue
		}
		findings = append(findings, b)
		if len(findings) >= max {
			break
		}
	}
	return findings
}

func isReportIntent(goal string) bool {
	if goal == "" {
		return false
	}
	lower := strings.ToLower(goal)
	hints := []string{"security report", "assessment report", "pen test report", "pentest report", "write a report", "create a report", "generate a report", "produce a report", "report"}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func (r *Runner) maybeFinalizeReport(goal string, dryRun bool) {
	if dryRun {
		return
	}
	if strings.TrimSpace(r.pendingAssistGoal) != "" {
		// Don't finalize while waiting for user input; the task is still in-flight.
		return
	}
	goal = strings.TrimSpace(goal)
	if !isReportIntent(goal) {
		return
	}
	path, err := r.finalizeReport(goal)
	if err != nil {
		r.logger.Printf("Report finalize failed: %v", err)
		return
	}
	if path != "" {
		r.logger.Printf("Report generated: %s", path)
	}
}

func (r *Runner) finalizeReport(goal string) (string, error) {
	if r.cfg.Permissions.Level == "readonly" {
		return "", nil
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return "", err
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return "", err
	}
	obsText := r.readRecentObservations(artifacts, 12)

	summary := readFileTrimmed(artifacts.SummaryPath)
	factsText := readFileTrimmed(artifacts.FactsPath)
	focus := readFileTrimmed(artifacts.FocusPath)
	planText := readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.PlanFilename))
	inventoryText := readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.InventoryFilename))

	scope := append([]string{}, r.cfg.Scope.Targets...)
	if len(scope) == 0 {
		if target := strings.TrimSpace(r.bestKnownTarget()); target != "" {
			scope = append(scope, target)
		}
	}
	findings := reportFindingsFromFacts(artifacts.FactsPath, 6)

	ledger := ""
	if r.cfg.Session.LedgerEnabled {
		ledger = readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.LedgerFilename))
	}

	outPath := filepath.Join(sessionDir, "report.md")
	if requested, ok := extractRequestedReportPath(goal); ok {
		if resolved, resolveErr := r.resolveReportWritePath(sessionDir, requested); resolveErr == nil {
			outPath = resolved
		}
	}
	timestamp := time.Now().UTC().Format("20060102-150405")
	archiveDir := filepath.Join(sessionDir, "reports")
	archivePath := filepath.Join(archiveDir, fmt.Sprintf("report-%s.md", timestamp))
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", err
	}

	info := report.Info{
		Date:         time.Now().UTC().Format("2006-01-02"),
		Scope:        scope,
		Findings:     findings,
		SessionID:    r.sessionID,
		Ledger:       ledger,
		Summary:      summary,
		KnownFacts:   factsText,
		Focus:        focus,
		Plan:         planText,
		Inventory:    inventoryText,
		Observations: obsText,
	}
	if err := report.Generate("", outPath, info); err != nil {
		return "", err
	}
	_ = report.Generate("", archivePath, info)
	return outPath, nil
}

func extractRequestedReportPath(goal string) (string, bool) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return "", false
	}
	tokens := strings.Fields(goal)
	for _, token := range tokens {
		candidate := strings.Trim(token, "\"'`(),;:[]{}<>")
		if candidate == "" || strings.Contains(candidate, "://") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(candidate))
		switch ext {
		case ".md", ".txt", ".html":
			return candidate, true
		}
	}
	return "", false
}

func (r *Runner) resolveReportWritePath(sessionDir, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("report output path is empty")
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
	return "", fmt.Errorf("report path out of bounds: %s (allowed: session dir or current working dir)", raw)
}
