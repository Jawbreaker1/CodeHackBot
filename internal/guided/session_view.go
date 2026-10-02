package guided

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

// These are terminal projections of the saved assessment. They never change
// plans, findings, or evidence and use the same state as the browser.
func printSessionView(c *Console, command, root string, state assessment.State) bool {
	switch command {
	case "/plan":
		if len(state.Plans) == 0 {
			c.Print("No coordinator plan has been saved yet.\n")
			return true
		}
		plan := state.Plans[len(state.Plans)-1]
		c.Print("Plan %d · %s\n", len(state.Plans), strings.TrimSpace(plan.Summary))
		if plan.Review != "" {
			c.Print("Previous round: %s\n", strings.TrimSpace(plan.Review))
		}
		if plan.PlainSummary != "" {
			c.Print("Purpose: %s\n", strings.TrimSpace(plan.PlainSummary))
		}
		for _, task := range plan.Tasks {
			status := "proposed"
			for _, result := range state.Results {
				if result.Task.ID == task.ID {
					status = result.Status
					break
				}
			}
			for _, id := range plan.SkippedTaskIDs {
				if id == task.ID {
					status = "skipped"
				}
			}
			c.Print("  %s [%s] %s\n    Done when: %s\n", task.ID, status, task.Goal, task.DoneWhen)
		}
		if plan.Complete {
			c.Print("Coordinator marked this assessment complete.\n")
		}
		return true
	case "/findings":
		findings := assessment.CurrentFindings(state.Plans)
		if len(findings) == 0 {
			c.Print("No current findings. Earlier plan drafts are not current findings.\n")
			return true
		}
		c.Print("Current findings (%d) · model-authored drafts\n", len(findings))
		for i, finding := range findings {
			severity := finding.Severity
			if severity == "" {
				severity = "unrated"
			}
			c.Print("  %d. [%s · %s] %s\n     %s\n", i+1, finding.Status, severity, finding.Title, strings.TrimSpace(finding.Impact))
		}
		return true
	case "/artifacts":
		refs := savedArtifacts(root, state)
		if len(refs) == 0 {
			c.Print("No saved worker artifacts yet. Execution logs are available in each task's logs directory.\n")
			return true
		}
		c.Print("Saved worker artifacts (%d)\n", len(refs))
		for _, ref := range refs {
			c.Print("  %s\n", ref)
		}
		return true
	case "/workers":
		if len(state.Results) == 0 {
			c.Print("No completed worker results yet.\n")
			return true
		}
		c.Print("Worker results\n")
		for _, result := range state.Results {
			c.Print("  %s [%s] %s\n", result.Task.ID, result.Status, strings.TrimSpace(result.Summary))
		}
		return true
	}
	return false
}

func savedArtifacts(root string, state assessment.State) []string {
	seen := make(map[string]bool)
	logs := make(map[string]bool)
	refs := []string{}
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.LogRefs {
				logs[ref] = true
			}
		}
	}
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.ArtifactRefs {
				if !filepath.IsAbs(ref) || seen[ref] || logs[ref] {
					continue
				}
				rel, err := filepath.Rel(root, ref)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					continue
				}
				parts := strings.Split(rel, string(filepath.Separator))
				if len(parts) >= 4 && parts[0] == "tasks" && parts[2] == "logs" {
					continue
				}
				info, err := os.Stat(ref)
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				seen[ref] = true
				refs = append(refs, fmt.Sprintf("%s · %s · %d bytes", result.Task.ID, ref, info.Size()))
			}
		}
	}
	sort.Strings(refs)
	return refs
}
