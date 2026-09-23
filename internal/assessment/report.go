package assessment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeReport(root string, s State) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Security assessment — %s\n\n", s.ID)
	fmt.Fprintf(&b, "**Status:** **%s**  \n**Objective:** %s  \n**Scope:** %s\n\n", s.Status, s.Goal, s.Scope)
	if !s.StartedAt.IsZero() {
		fmt.Fprintf(&b, "**Started:** %s  \n", s.StartedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if !s.FinishedAt.IsZero() {
		fmt.Fprintf(&b, "**Finished:** %s  \n", s.FinishedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	b.WriteString("\nThis is a model-authored draft for professional review. The runtime does not enforce scope isolation; the authorized lab provides that boundary. Completion does not establish absence of vulnerabilities.\n\n")
	b.WriteString("## Executive summary\n\n")
	if len(s.Plans) > 0 && s.Plans[len(s.Plans)-1].Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", s.Plans[len(s.Plans)-1].Summary)
	} else {
		b.WriteString("No coordinator summary was recorded. Review the work and evidence below before drawing a conclusion.\n\n")
	}
	b.WriteString("## Method and coverage\n\n")
	b.WriteString("The sequence records each coordinator round and the tasks proposed, approved, or skipped. A proposed task does not establish coverage; completed work is detailed in the evidence trail.\n\n")
	for i, d := range s.Plans {
		if len(d.Tasks) == 0 {
			continue
		}
		phase := d.Phase
		if phase == "" {
			phase = "assessment"
		}
		fmt.Fprintf(&b, "### Round %d — %s\n\n", i+1, phase)
		for _, task := range d.Tasks {
			state := "proposed"
			for _, id := range d.ApprovedTaskIDs {
				if id == task.ID {
					state = "approved"
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
	b.WriteString("## Findings\n\n")
	findings := CurrentFindings(s.Plans)
	if len(findings) == 0 {
		b.WriteString("No findings were recorded. This is not a claim that the scoped system has no vulnerabilities.\n\n")
	}
	if len(s.Plans) > 0 {
		for _, f := range findings {
			fmt.Fprintf(&b, "### %s\n\nStatus: %s (model assessment; operator review required)\n\n", f.Title, f.Status)
			if f.Severity != "" {
				fmt.Fprintf(&b, "Severity: **%s**\n\n", f.Severity)
			}
			if f.Confidence != "" {
				fmt.Fprintf(&b, "Confidence: **%s**\n\n", f.Confidence)
			}
			if f.ValidationTask != "" {
				fmt.Fprintf(&b, "Validation task: `%s`\n\n", f.ValidationTask)
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
	}
	b.WriteString("## Limitations and unresolved gaps\n\n")
	if s.Error != "" {
		fmt.Fprintf(&b, "- Run limitation: %s\n", s.Error)
	}
	if len(s.Plans) > 0 {
		for _, gap := range s.Plans[len(s.Plans)-1].Gaps {
			fmt.Fprintf(&b, "- %s\n", gap)
		}
	}
	if s.Error == "" && (len(s.Plans) == 0 || len(s.Plans[len(s.Plans)-1].Gaps) == 0) {
		b.WriteString("No additional gaps were stated by the coordinator; this is not a coverage guarantee.\n")
	}
	b.WriteString("\n## Technical evidence trail\n\n")
	b.WriteString("Invocations, results, and local references below support the findings and record unsuccessful or partial work.\n\n")
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
	b.WriteString("## Assessment metadata\n\n")
	fmt.Fprintf(&b, "Model: `%s`. Calls: %d/%d; failed calls: %d; reported tokens: %d; calls without usage: %d.\n\n", s.Model, s.Usage.Calls, s.Limits.ModelCalls, s.Usage.FailedCalls, s.Usage.ReportedTokens, s.Usage.CallsWithoutUsage)
	if s.ReasoningEffort != "" {
		fmt.Fprintf(&b, "Requested reasoning effort: `%s`.\n\n", s.ReasoningEffort)
	}
	if s.MaxOutputTokens > 0 {
		fmt.Fprintf(&b, "Output budget per model request: %d tokens.\n\n", s.MaxOutputTokens)
	}
	if len(s.OperatorMessages) > 0 {
		b.WriteString("### Operator conversation\n\n")
		for _, message := range s.OperatorMessages {
			fmt.Fprintf(&b, "- %s\n", message)
		}
	}
	return os.WriteFile(filepath.Join(root, "report.md"), []byte(b.String()), 0600)
}

func markdownCode(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}
