package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/report"
)

func (r *Runner) handleReport(args []string) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: report generation not permitted")
	}
	if len(args) == 1 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "profiles", "profile", "types", "list":
			r.logger.Printf("Report profiles: %s", strings.Join(report.AvailableProfiles(), ", "))
			r.logger.Printf("Usage: /report [profile] [output_path]")
			return nil
		}
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	profile, outArg, err := parseReportArgs(args)
	if err != nil {
		return err
	}
	outPath := filepath.Join(sessionDir, "report.md")
	if outArg != "" {
		resolved, resolveErr := r.resolveReportWritePath(sessionDir, outArg)
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
	runtimeGoal := strings.TrimSpace(r.assistRuntime.Goal)
	goal := runtimeGoal
	if goal == "" {
		goal = strings.TrimSpace(focus)
	}
	primaryTarget := reportPrimaryTarget(goal, r.bestKnownTarget())
	objectiveStatus, objectiveReason := reportObjectiveStatus(runtimeGoal, r.assistObjectiveMet)
	targetStatus, targetReason := reportTargetStatus(obsText, primaryTarget)

	ledger := ""
	if r.cfg.Session.LedgerEnabled {
		ledger = readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.LedgerFilename))
	}

	info := report.Info{
		Date:            time.Now().UTC().Format("2006-01-02"),
		Scope:           scope,
		Findings:        findings,
		SessionID:       r.sessionID,
		Goal:            goal,
		Ledger:          ledger,
		Summary:         summary,
		KnownFacts:      factsText,
		Focus:           focus,
		Plan:            planText,
		Inventory:       inventoryText,
		Observations:    obsText,
		PrimaryTarget:   primaryTarget,
		ObjectiveStatus: objectiveStatus,
		ObjectiveReason: objectiveReason,
		TargetStatus:    targetStatus,
		TargetReason:    targetReason,
	}
	if err := report.GenerateWithProfile(profile, "", outPath, info); err != nil {
		return err
	}
	r.logger.Printf("Report generated (%s): %s", profile, outPath)
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
	if r.openLikeAssistLoopEnabled() && r.isActionGoal(goal) && !r.assistObjectiveMet {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Skipping report finalize: objective not yet met for action goal.")
		}
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
	primaryTarget := reportPrimaryTarget(goal, r.bestKnownTarget())
	objectiveStatus, objectiveReason := reportObjectiveStatus(goal, r.assistObjectiveMet)
	targetStatus, targetReason := reportTargetStatus(obsText, primaryTarget)

	ledger := ""
	if r.cfg.Session.LedgerEnabled {
		ledger = readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.LedgerFilename))
	}

	outPath := filepath.Join(sessionDir, "report.md")
	profile := extractRequestedReportProfile(goal)
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
		Date:            time.Now().UTC().Format("2006-01-02"),
		Scope:           scope,
		Findings:        findings,
		SessionID:       r.sessionID,
		Goal:            goal,
		Ledger:          ledger,
		Summary:         summary,
		KnownFacts:      factsText,
		Focus:           focus,
		Plan:            planText,
		Inventory:       inventoryText,
		Observations:    obsText,
		PrimaryTarget:   primaryTarget,
		ObjectiveStatus: objectiveStatus,
		ObjectiveReason: objectiveReason,
		TargetStatus:    targetStatus,
		TargetReason:    targetReason,
	}
	if err := report.GenerateWithProfile(profile, "", outPath, info); err != nil {
		return "", err
	}
	_ = report.GenerateWithProfile(profile, "", archivePath, info)
	return outPath, nil
}

var expectedAtGoalTargetPattern = regexp.MustCompile(`(?i)\bexpected\s+at\s+([^\s,;:()]+)`)

func reportPrimaryTarget(goal, fallback string) string {
	goal = strings.TrimSpace(goal)
	if goal != "" {
		if match := expectedAtGoalTargetPattern.FindStringSubmatch(goal); len(match) == 2 {
			if token := normalizeGoalTargetToken(match[1]); token != "" {
				return token
			}
		}
		if token := normalizeGoalTargetToken(extractHostLikeToken(goal)); token != "" {
			return token
		}
	}
	return normalizeGoalTargetToken(fallback)
}

func normalizeGoalTargetToken(token string) string {
	token = strings.TrimSpace(strings.Trim(token, "\"'`(),;[]{}<>"))
	if token == "" {
		return ""
	}
	if strings.Contains(token, "/") || strings.Contains(token, "\\") {
		return ""
	}
	if ip := net.ParseIP(token); ip != nil {
		return ip.String()
	}
	if strings.Count(token, ".") >= 1 && !strings.HasPrefix(token, ".") && !strings.HasSuffix(token, ".") {
		return strings.ToLower(token)
	}
	return ""
}

func reportObjectiveStatus(goal string, objectiveMet bool) (string, string) {
	goal = strings.TrimSpace(goal)
	if goal == "" || !looksLikeAction(goal) {
		return "unknown", "Report generated without an active action-goal completion contract."
	}
	if objectiveMet {
		return "met", "Assistant completion contract reported objective_met=true with evidence refs."
	}
	return "not_met", "Objective was not verified by completion contract before report synthesis."
}

func reportTargetStatus(observations, target string) (string, string) {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return "unknown", "No primary target could be derived from goal or session context."
	}
	lower := strings.ToLower(observations)
	if !strings.Contains(lower, target) {
		return "not_verified", fmt.Sprintf("No captured observation references target %s.", target)
	}
	if strings.Contains(lower, target) && strings.Contains(lower, "host seems down") {
		return "not_verified", fmt.Sprintf("Target %s was observed as down/non-responsive in scan output.", target)
	}
	if strings.Contains(lower, "nmap scan report for") && strings.Contains(lower, "("+target+")") && strings.Contains(lower, "host is up") {
		return "verified", fmt.Sprintf("Nmap evidence confirms target %s responded as a live host.", target)
	}
	return "unknown", fmt.Sprintf("Target %s appears in observations, but verification evidence is inconclusive.", target)
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

func extractRequestedReportProfile(goal string) string {
	lower := strings.ToLower(goal)
	switch {
	case strings.Contains(lower, "owasp") || strings.Contains(lower, "wstg") || strings.Contains(lower, "asvs"):
		return string(report.ProfileOWASP)
	case strings.Contains(lower, "nis2"):
		return string(report.ProfileNIS2)
	case strings.Contains(lower, "internal lab"), strings.Contains(lower, "lab report"), strings.Contains(lower, "internal report"):
		return string(report.ProfileInternal)
	default:
		return string(report.ProfileStandard)
	}
}

func parseReportArgs(args []string) (profile string, outArg string, err error) {
	profile = string(report.ProfileStandard)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "--output="):
			value := strings.TrimSpace(trimmed[len("--output="):])
			if value == "" || outArg != "" {
				return "", "", fmt.Errorf("usage: /report [profile] [output_path]")
			}
			outArg = value
			continue
		case strings.HasPrefix(lower, "output="):
			value := strings.TrimSpace(trimmed[len("output="):])
			if value == "" || outArg != "" {
				return "", "", fmt.Errorf("usage: /report [profile] [output_path]")
			}
			outArg = value
			continue
		case lower == "--output":
			if i+1 >= len(args) || outArg != "" {
				return "", "", fmt.Errorf("usage: /report [profile] [output_path]")
			}
			next := strings.TrimSpace(args[i+1])
			if next == "" {
				return "", "", fmt.Errorf("usage: /report [profile] [output_path]")
			}
			outArg = next
			i++
			continue
		}
		if looksLikePathArg(trimmed) {
			if outArg == "" {
				outArg = trimmed
				continue
			}
			return "", "", fmt.Errorf("usage: /report [profile] [output_path]")
		}
		if resolved, ok := tryNormalizeProfile(trimmed); ok {
			profile = resolved
			continue
		}
		if outArg == "" {
			outArg = trimmed
			continue
		}
		return "", "", fmt.Errorf("usage: /report [profile] [output_path]")
	}
	return profile, outArg, nil
}

func looksLikePathArg(arg string) bool {
	if strings.Contains(arg, "/") || strings.Contains(arg, "\\") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(arg))
	return ext == ".md" || ext == ".txt" || ext == ".html"
}

func tryNormalizeProfile(value string) (string, bool) {
	resolved, err := report.NormalizeProfile(value)
	if err != nil {
		return "", false
	}
	return resolved, true
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
