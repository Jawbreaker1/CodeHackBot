package assessment

import (
	"fmt"
	"os"
	"strings"
)

// ReportReadiness is a derived view of the saved assessment, shared by the
// coordinator, browser, and report renderers. It is not a second evidence store
// or a claim that the assessment or its findings are professionally verified.
type ReportReadiness struct {
	Status string        `json:"status"` // collecting, needs_attention, or ready_for_review
	Checks []ReportCheck `json:"checks"`
}

type ReportCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"` // recorded, pending, or needs_attention
	Detail string `json:"detail"`
}

func (r ReportReadiness) Attention() []string {
	var items []string
	for _, check := range r.Checks {
		if check.Status == "needs_attention" {
			items = append(items, check.Label+": "+check.Detail)
		}
	}
	return items
}

// AssessReportReadiness checks what the runtime can establish from recorded
// state. A missing item remains visible in a draft; it never blocks exploration
// or causes the model to invent a value for a report field.
func AssessReportReadiness(state State) ReportReadiness {
	r := ReportReadiness{Status: "collecting"}
	add := func(id, label, status, detail string) {
		r.Checks = append(r.Checks, ReportCheck{ID: id, Label: label, Status: status, Detail: detail})
	}
	finished := state.Status != "" && state.Status != "running" && state.Status != "starting"
	if strings.TrimSpace(state.Goal) == "" || strings.TrimSpace(state.Scope) == "" {
		add("scope", "Objective and scope", "needs_attention", "Record the objective and exact target boundary.")
	} else {
		add("scope", "Objective and scope", "recorded", "The objective and declared scope are saved.")
	}
	if state.StartedAt.IsZero() || (finished && state.FinishedAt.IsZero()) {
		add("window", "Test window", "needs_attention", "A start or finish time is missing from the saved run.")
	} else if state.FinishedAt.IsZero() {
		add("window", "Test window", "pending", "The finish time will be recorded when the run ends.")
	} else {
		add("window", "Test window", "recorded", "Start and finish times are saved.")
	}

	accounted := map[string]bool{}
	for _, result := range state.Results {
		accounted[result.Task.ID] = true
	}
	missingTasks := 0
	for _, plan := range state.Plans {
		for _, task := range plan.Tasks {
			skipped := false
			for _, id := range plan.SkippedTaskIDs {
				if id == task.ID {
					skipped = true
					break
				}
			}
			if !accounted[task.ID] && !skipped {
				missingTasks++
			}
		}
	}
	switch {
	case missingTasks > 0 && finished:
		add("outcomes", "Test outcomes", "needs_attention", fmt.Sprintf("%d planned task(s) have no recorded outcome or skip decision.", missingTasks))
	case missingTasks > 0 || (len(state.Results) == 0 && !finished):
		add("outcomes", "Test outcomes", "pending", "Worker outcomes will be recorded as tests finish.")
	case len(state.Results) == 0:
		add("outcomes", "Test outcomes", "needs_attention", "No worker outcome was recorded; describe what was and was not tested.")
	default:
		label := "worker outcomes are"
		if len(state.Results) == 1 {
			label = "worker outcome is"
		}
		add("outcomes", "Test outcomes", "recorded", fmt.Sprintf("%d %s recorded; only completed tests establish coverage.", len(state.Results), label))
	}

	findings, legacyClaims := reviewFindings(state)
	registered := map[string]bool{}
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.LogRefs {
				registered[ref] = true
			}
			for _, ref := range evidence.ArtifactRefs {
				registered[ref] = true
			}
		}
	}
	unsupported := 0
	for _, finding := range findings {
		if strings.TrimSpace(finding.Title) == "" || strings.TrimSpace(finding.Impact) == "" || len(finding.Steps) == 0 || len(finding.Remediation) == 0 || len(finding.Evidence) == 0 {
			unsupported++
			continue
		}
		for _, ref := range append(append([]string{}, finding.Evidence...), finding.References...) {
			info, err := os.Stat(ref)
			if !registered[ref] || err != nil || !info.Mode().IsRegular() {
				unsupported++
				break
			}
		}
	}
	switch {
	case unsupported > 0:
		add("findings", "Finding evidence", "needs_attention", fmt.Sprintf("%d finding(s) need required fields or accessible, registered evidence files.", unsupported))
	case legacyClaims:
		add("findings", "Finding evidence", "needs_attention", "A historical reproduction claim lacks a supported challenge and is shown as a candidate requiring recheck.")
	case len(findings) == 0:
		add("findings", "Finding evidence", "recorded", "No finding claims are recorded; this is not evidence of a clean target.")
	default:
		add("findings", "Finding evidence", "recorded", fmt.Sprintf("%d finding(s) have required fields and accessible registered references; claim accuracy still needs review.", len(findings)))
	}

	unresolved := false
	for _, result := range state.Results {
		if result.Status != "done" {
			unresolved = true
			break
		}
	}
	for _, plan := range state.Plans {
		if len(plan.SkippedTaskIDs) > 0 {
			unresolved = true
			break
		}
	}
	if unresolved && finished && len(ReportGaps(state)) == 0 {
		add("limitations", "Limitations", "needs_attention", "Failed, blocked, or skipped work needs an explicit coverage gap.")
	} else if !finished {
		add("limitations", "Limitations", "pending", "Reconcile blocked, failed, and skipped work before the conclusion.")
	} else {
		add("limitations", "Limitations", "recorded", "Unfinished work and stated limitations remain visible in the report.")
	}
	if len(state.Plans) > 0 && state.Plans[len(state.Plans)-1].Complete {
		add("conclusion", "Coordinator conclusion", "recorded", "The final decision reconciles the worker results.")
	} else if finished {
		add("conclusion", "Coordinator conclusion", "needs_attention", "The run ended without a final coordinator review of its results.")
	} else {
		add("conclusion", "Coordinator conclusion", "pending", "The coordinator will review the latest results before ending the run.")
	}
	if finished {
		r.Status = "ready_for_review"
		if len(r.Attention()) > 0 {
			r.Status = "needs_attention"
		}
	}
	return r
}
