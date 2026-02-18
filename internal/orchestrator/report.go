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

type artifactRecord struct {
	Artifact   Artifact
	RecordPath string
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

	var b strings.Builder
	fmt.Fprintf(&b, "# Orchestrator Run Report\n\n")
	fmt.Fprintf(&b, "- Run ID: `%s`\n", runID)
	fmt.Fprintf(&b, "- Generated: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- State: `%s`\n", status.State)
	fmt.Fprintf(&b, "- Active workers: %d\n", status.ActiveWorkers)
	fmt.Fprintf(&b, "- Queued tasks: %d\n", status.QueuedTasks)
	fmt.Fprintf(&b, "- Running tasks: %d\n", status.RunningTasks)
	fmt.Fprintf(&b, "- Findings: %d\n", len(findings))
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
	if len(findings) == 0 {
		fmt.Fprintf(&b, "- No findings were merged for this run.\n")
	} else {
		for _, finding := range findings {
			fmt.Fprintf(&b, "### %s (%s / %s)\n\n", finding.Title, strings.ToUpper(finding.Severity), strings.ToUpper(finding.Confidence))
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
