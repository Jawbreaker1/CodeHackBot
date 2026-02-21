package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	reportFindingVerified   = "VERIFIED"
	reportFindingUnverified = "UNVERIFIED"
)

type artifactRecord struct {
	Artifact   Artifact
	RecordPath string
}

type reportFinding struct {
	Finding             Finding
	Verification        string
	LinkedEvidence      []string
	UnverifiedRationale string
}

func (m *Manager) AssembleRunReport(runID, outPath string) (string, error) {
	if strings.TrimSpace(runID) == "" {
		return "", fmt.Errorf("run id is required")
	}
	paths := BuildRunPaths(m.SessionsDir, runID)
	if strings.TrimSpace(outPath) == "" {
		outPath = filepath.Join(paths.Root, "report.md")
	}

	plan, err := m.LoadRunPlan(runID)
	if err != nil {
		return "", err
	}
	status, err := m.Status(runID)
	if err != nil {
		return "", err
	}
	findings, err := m.ListFindings(runID)
	if err != nil {
		return "", err
	}
	artifacts, err := m.listArtifactRecords(runID)
	if err != nil {
		return "", err
	}

	sort.Slice(findings, func(i, j int) bool {
		si := severityRank(findings[i].Severity)
		sj := severityRank(findings[j].Severity)
		if si != sj {
			return si > sj
		}
		ci := confidenceRank(findings[i].Confidence)
		cj := confidenceRank(findings[j].Confidence)
		if ci != cj {
			return ci > cj
		}
		return findings[i].Title < findings[j].Title
	})
	reportFindings := buildReportFindings(findings, artifacts)
	verifiedCount := 0
	for _, item := range reportFindings {
		if item.Verification == reportFindingVerified {
			verifiedCount++
		}
	}
	unverifiedCount := len(reportFindings) - verifiedCount

	var b strings.Builder
	fmt.Fprintf(&b, "# Orchestrator Run Report\n\n")
	fmt.Fprintf(&b, "- Run ID: `%s`\n", runID)
	fmt.Fprintf(&b, "- Generated: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- State: `%s`\n", status.State)
	fmt.Fprintf(&b, "- Active workers: %d\n", status.ActiveWorkers)
	fmt.Fprintf(&b, "- Queued tasks: %d\n", status.QueuedTasks)
	fmt.Fprintf(&b, "- Running tasks: %d\n", status.RunningTasks)
	fmt.Fprintf(&b, "- Findings: %d\n", len(reportFindings))
	fmt.Fprintf(&b, "- Verified findings: %d\n", verifiedCount)
	fmt.Fprintf(&b, "- Unverified findings: %d\n", unverifiedCount)
	fmt.Fprintf(&b, "- Artifacts: %d\n", len(artifacts))

	fmt.Fprintf(&b, "\n## Scope\n\n")
	if len(plan.Scope.Networks) == 0 && len(plan.Scope.Targets) == 0 && len(plan.Scope.DenyTargets) == 0 {
		fmt.Fprintf(&b, "- Not specified\n")
	} else {
		for _, network := range plan.Scope.Networks {
			fmt.Fprintf(&b, "- Network: `%s`\n", network)
		}
		for _, target := range plan.Scope.Targets {
			fmt.Fprintf(&b, "- Target: `%s`\n", target)
		}
		for _, denied := range plan.Scope.DenyTargets {
			fmt.Fprintf(&b, "- Deny: `%s`\n", denied)
		}
	}

	fmt.Fprintf(&b, "\n## Findings\n\n")
	if len(reportFindings) == 0 {
		fmt.Fprintf(&b, "- No findings were merged for this run.\n")
	} else {
		for _, findingView := range reportFindings {
			finding := findingView.Finding
			fmt.Fprintf(&b, "### [%s] %s (%s / %s)\n\n", findingView.Verification, finding.Title, strings.ToUpper(finding.Severity), strings.ToUpper(finding.Confidence))
			fmt.Fprintf(&b, "- Verification: `%s`\n", findingView.Verification)
			if findingView.Verification == reportFindingUnverified && strings.TrimSpace(findingView.UnverifiedRationale) != "" {
				fmt.Fprintf(&b, "- Claim status: `%s` (%s)\n", reportFindingUnverified, findingView.UnverifiedRationale)
			}
			if finding.Target != "" {
				fmt.Fprintf(&b, "- Target: `%s`\n", finding.Target)
			}
			if finding.FindingType != "" {
				fmt.Fprintf(&b, "- Type: `%s`\n", finding.FindingType)
			}
			if location := finding.Metadata["location"]; location != "" {
				fmt.Fprintf(&b, "- Location: `%s`\n", location)
			}
			if len(finding.Evidence) > 0 {
				fmt.Fprintf(&b, "- Evidence:\n")
				for _, evidence := range finding.Evidence {
					fmt.Fprintf(&b, "  - %s\n", evidence)
				}
			} else if findingView.Verification == reportFindingUnverified {
				fmt.Fprintf(&b, "- Evidence:\n")
				fmt.Fprintf(&b, "  - %s: no direct evidence statements were recorded for this finding.\n", reportFindingUnverified)
			}
			if len(findingView.LinkedEvidence) > 0 {
				fmt.Fprintf(&b, "- Linked artifact/log evidence:\n")
				for _, link := range findingView.LinkedEvidence {
					fmt.Fprintf(&b, "  - %s\n", link)
				}
			} else {
				fmt.Fprintf(&b, "- Linked artifact/log evidence: `%s`\n", reportFindingUnverified)
			}
			if len(finding.Sources) > 0 {
				fmt.Fprintf(&b, "- Sources:\n")
				for _, source := range finding.Sources {
					name := source.Source
					if strings.TrimSpace(name) == "" {
						name = source.WorkerID
					}
					fmt.Fprintf(&b, "  - %s (event `%s`, task `%s`)\n", name, source.EventID, source.TaskID)
				}
			}
			if len(finding.Conflicts) > 0 {
				fmt.Fprintf(&b, "- Conflicts retained:\n")
				for _, conflict := range finding.Conflicts {
					fmt.Fprintf(&b, "  - `%s`: `%s` vs `%s` -> `%s` (event `%s`)\n", conflict.Field, conflict.ExistingValue, conflict.IncomingValue, conflict.Resolution, conflict.IncomingEvent)
				}
			}
			fmt.Fprintf(&b, "\n")
		}
	}

	fmt.Fprintf(&b, "## Artifacts\n\n")
	if len(artifacts) == 0 {
		fmt.Fprintf(&b, "- No artifacts were merged for this run.\n")
	} else {
		for _, item := range artifacts {
			title := strings.TrimSpace(item.Artifact.Title)
			if title == "" {
				title = filepath.Base(item.RecordPath)
			}
			fmt.Fprintf(&b, "- `%s` (%s)\n", title, item.Artifact.Type)
			if item.Artifact.Path != "" {
				fmt.Fprintf(&b, "  - Path: `%s`\n", item.Artifact.Path)
			}
			fmt.Fprintf(&b, "  - Record: `%s`\n", item.RecordPath)
		}
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return outPath, nil
}

func buildReportFindings(findings []Finding, artifacts []artifactRecord) []reportFinding {
	linksByTask := artifactLinksByTask(artifacts)
	out := make([]reportFinding, 0, len(findings))
	for _, finding := range findings {
		links := make([]string, 0, 4)
		links = append(links, linksByTask[strings.TrimSpace(finding.TaskID)]...)
		for _, source := range finding.Sources {
			links = append(links, linksByTask[strings.TrimSpace(source.TaskID)]...)
		}
		for _, evidence := range finding.Evidence {
			link := evidencePathLike(evidence)
			if link != "" {
				links = append(links, link)
			}
		}
		links = dedupeTrimmed(links)
		view := reportFinding{
			Finding:        finding,
			Verification:   reportFindingVerified,
			LinkedEvidence: links,
		}
		if len(links) == 0 {
			view.Verification = reportFindingUnverified
			view.UnverifiedRationale = "no linked artifact/log evidence"
		}
		out = append(out, view)
	}
	return out
}

func artifactLinksByTask(artifacts []artifactRecord) map[string][]string {
	byTask := map[string][]string{}
	for _, item := range artifacts {
		taskID := strings.TrimSpace(item.Artifact.TaskID)
		if taskID == "" {
			continue
		}
		links := byTask[taskID]
		if path := strings.TrimSpace(item.Artifact.Path); path != "" {
			links = append(links, path)
		}
		if record := strings.TrimSpace(item.RecordPath); record != "" {
			links = append(links, record)
		}
		byTask[taskID] = dedupeTrimmed(links)
	}
	return byTask
}

func evidencePathLike(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "://") {
		return text
	}
	if strings.ContainsAny(text, " \t") {
		return ""
	}
	if strings.Contains(text, "/") || strings.Contains(text, "\\") {
		if strings.Contains(lower, "/logs/") || strings.Contains(lower, "/artifact/") ||
			strings.Contains(lower, "\\logs\\") || strings.Contains(lower, "\\artifact\\") {
			return text
		}
		if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "./") || strings.HasPrefix(text, "../") || strings.Contains(text, "\\") {
			if ext := strings.ToLower(filepath.Ext(text)); ext != "" {
				return text
			}
		}
	}
	for _, ext := range []string{".log", ".txt", ".json", ".xml", ".html", ".md", ".pcap"} {
		if strings.HasSuffix(lower, ext) {
			return text
		}
	}
	return ""
}

func dedupeTrimmed(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (m *Manager) listArtifactRecords(runID string) ([]artifactRecord, error) {
	files, err := filepath.Glob(filepath.Join(BuildRunPaths(m.SessionsDir, runID).ArtifactDir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := make([]artifactRecord, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var artifact Artifact
		if err := json.Unmarshal(data, &artifact); err != nil {
			return nil, err
		}
		out = append(out, artifactRecord{
			Artifact:   artifact,
			RecordPath: file,
		})
	}
	return out, nil
}

func (m *Manager) ResolveRunReportPath(runID string) (string, bool, error) {
	if strings.TrimSpace(runID) == "" {
		return "", false, fmt.Errorf("run id is required")
	}
	defaultPath := filepath.Join(BuildRunPaths(m.SessionsDir, runID).Root, "report.md")
	path := defaultPath
	events, err := m.Events(runID, 0)
	if err != nil {
		return "", false, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != EventTypeRunReportGenerated {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if raw, ok := payload["path"].(string); ok && strings.TrimSpace(raw) != "" {
			path = strings.TrimSpace(raw)
			break
		}
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return path, false, nil
		}
		return path, false, statErr
	}
	if info.IsDir() {
		return path, false, nil
	}
	return path, true, nil
}
