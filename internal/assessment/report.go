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
	if len(s.Plans) > 0 {
		d := s.Plans[len(s.Plans)-1]
		fmt.Fprintf(&b, "## Summary\n\n%s\n\n", d.Summary)
		for _, f := range d.Findings {
			fmt.Fprintf(&b, "## %s\n\nStatus: %s (model assessment; operator review required)\n\nImpact: %s\n\nSteps to reproduce:\n\n", f.Title, f.Status, f.Impact)
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
			for _, ref := range e.LogRefs {
				fmt.Fprintf(&b, "- %s\n", ref)
			}
		}
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(root, "report.md"), []byte(b.String()), 0600)
}
