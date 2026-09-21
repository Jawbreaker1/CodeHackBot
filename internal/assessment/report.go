package assessment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeReport(root string, s State) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Assessment %s\n\nStatus: **%s**\n\n## Scope\n\n%s\n\n## Goal\n\n%s\n\n", s.ID, s.Status, s.Scope, s.Goal)
	b.WriteString("Lab preview: scope isolation is externally supplied, not enforced by this runtime. Findings are model-authored drafts requiring operator review. Assessment completion does not establish absence of vulnerabilities.\n\n")
	fmt.Fprintf(&b, "Model: `%s`. Calls: %d/%d; failed calls: %d; reported tokens: %d; calls without usage: %d.\n\n", s.Model, s.Usage.Calls, s.Limits.ModelCalls, s.Usage.FailedCalls, s.Usage.ReportedTokens, s.Usage.CallsWithoutUsage)
	if s.ReasoningEffort != "" {
		fmt.Fprintf(&b, "Requested reasoning effort: `%s`.\n\n", s.ReasoningEffort)
	}
	if s.MaxOutputTokens > 0 {
		fmt.Fprintf(&b, "Output budget per model request: %d tokens.\n\n", s.MaxOutputTokens)
	}
	if s.Error != "" {
		fmt.Fprintf(&b, "Run limitation: %s\n\n", s.Error)
	}
	if len(s.OperatorMessages) > 0 {
		b.WriteString("## Operator conversation\n\n")
		for _, message := range s.OperatorMessages {
			fmt.Fprintf(&b, "- %s\n", message)
		}
		b.WriteString("\n")
	}
	if len(s.Plans) > 0 {
		d := s.Plans[len(s.Plans)-1]
		fmt.Fprintf(&b, "## Summary\n\n%s\n\n", d.Summary)
		if len(d.Tasks) > 0 {
			b.WriteString("## Proposed test sequence\n\n")
			for _, task := range d.Tasks {
				state := "proposed"
				for _, id := range d.ApprovedTaskIDs {
					if id == task.ID {
						state = "approved and run"
					}
				}
				for _, id := range d.SkippedTaskIDs {
					if id == task.ID {
						state = "skipped by operator"
					}
				}
				fmt.Fprintf(&b, "- `%s` — **%s** — %s (done when: %s)\n", task.ID, state, task.Goal, task.DoneWhen)
			}
			b.WriteString("\n")
		}
		for _, f := range d.Findings {
			fmt.Fprintf(&b, "## %s\n\nStatus: %s (model assessment; operator review required)\n\n", f.Title, f.Status)
			if f.Severity != "" {
				fmt.Fprintf(&b, "Severity: **%s**\n\n", f.Severity)
			}
			if f.Confidence != "" {
				fmt.Fprintf(&b, "Confidence: **%s**\n\n", f.Confidence)
			}
			if len(f.CVEIDs) > 0 {
				fmt.Fprintf(&b, "CVE references: %s\n\n", strings.Join(f.CVEIDs, ", "))
			}
			if len(f.AffectedSoftware) > 0 {
				fmt.Fprintf(&b, "Affected software: %s\n\n", strings.Join(f.AffectedSoftware, ", "))
			}
			fmt.Fprintf(&b, "Impact: %s\n\nSteps to reproduce:\n\n", f.Impact)
			for i, step := range f.Steps {
				fmt.Fprintf(&b, "%d. %s\n", i+1, step)
			}
			b.WriteString("\nRemediation:\n\n")
			for _, step := range f.Remediation {
				fmt.Fprintf(&b, "- %s\n", step)
			}
			b.WriteString("\nEvidence:\n\n")
			for _, ref := range f.Evidence {
				fmt.Fprintf(&b, "- %s\n", ref)
			}
			if len(f.References) > 0 {
				b.WriteString("\nResearch references:\n\n")
				for _, ref := range f.References {
					fmt.Fprintf(&b, "- %s\n", ref)
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("## Assessment gaps\n\n")
		if len(d.Gaps) == 0 {
			b.WriteString("No additional gaps stated by the coordinator; this is not a coverage guarantee.\n")
		}
		for _, gap := range d.Gaps {
			fmt.Fprintf(&b, "- %s\n", gap)
		}
	}
	b.WriteString("\n## Work and evidence\n\n")
	for _, r := range s.Results {
		fmt.Fprintf(&b, "### %s — %s\n\n%s\n\nCompletion criterion: %s\n\n%s\n\n", r.Task.ID, r.Status, r.Task.Goal, r.Task.DoneWhen, r.Summary)
		if r.Error != "" {
			fmt.Fprintf(&b, "Limitation: %s\n\n", r.Error)
		}
		for _, e := range r.Evidence {
			if e.ActualExec != "" {
				fmt.Fprintf(&b, "Invocation: `%s`\n\nExit status: `%s`\n\n", markdownCode(e.ActualExec), markdownCode(e.ExitStatus))
			}
			if e.OutputSummary != "" {
				fmt.Fprintf(&b, "Observed result: %s\n\n", e.OutputSummary)
			}
			for _, ref := range e.LogRefs {
				fmt.Fprintf(&b, "- %s\n", ref)
			}
			for _, ref := range e.ArtifactRefs {
				fmt.Fprintf(&b, "- %s\n", ref)
			}
		}
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(root, "report.md"), []byte(b.String()), 0600)
}

func markdownCode(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}
