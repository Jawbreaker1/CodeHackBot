package assessment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeReport(root string, s State) error {
	var b strings.Builder
	findings, legacyClaims := reviewFindings(s)
	fmt.Fprintf(&b, "# Security assessment — %s\n\n", s.ID)
	fmt.Fprintf(&b, "**Status:** **%s**  \n**Objective:** %s  \n**Scope:** %s\n\n", strings.ReplaceAll(s.Status, "_", " "), s.Goal, s.Scope)
	if !s.StartedAt.IsZero() {
		fmt.Fprintf(&b, "**Started:** %s  \n", s.StartedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if !s.FinishedAt.IsZero() {
		fmt.Fprintf(&b, "**Finished:** %s  \n", s.FinishedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if len(s.ContinuationRequests) > 0 {
		b.WriteString("\n**Additional operator requests in this session:**\n\n")
		for _, request := range s.ContinuationRequests {
			fmt.Fprintf(&b, "- %s\n", request)
		}
	}
	b.WriteString("\nThis draft is compiled from model-authored findings and recorded evidence for professional review. A reproduced status means a later worker challenged the claim and recorded a supported verdict with its own execution log; it is not independent professional confirmation that every claim is correct. The operator is responsible for authorization and target boundaries; the runtime does not enforce network scope isolation. Completion does not establish absence of vulnerabilities.\n\n")
	if legacyClaims {
		b.WriteString("Earlier reproduction claims without a supported challenge and cited execution log are presented here as candidates requiring recheck. The saved session record is unchanged.\n\n")
	}
	b.WriteString("## Executive summary\n\n")
	summary, unreviewed := reportOutcome(s)
	if len(findings) > 0 {
		b.WriteString("**Findings recorded for review**\n\n")
		for _, finding := range findings {
			label := finding.Status
			if finding.Severity != "" {
				label = finding.Severity + " · " + label
			}
			fmt.Fprintf(&b, "- **%s** — %s\n", label, finding.Title)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%s\n\n", summary)
	if leads := CurrentOpenLeads(s.Plans); len(leads) > 0 {
		b.WriteString("## Recommended continuation\n\nThese are open questions, not completed tests or confirmed vulnerabilities. Continue in this session after reviewing scope and the recorded evidence.\n\n")
		for i, lead := range leads {
			fmt.Fprintf(&b, "%d. **Question:** %s\n   **Next check:** %s\n", i+1, lead.Hypothesis, lead.NextCheck)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Method and coverage\n\n")
	b.WriteString("Each round shows its purpose and recorded task outcome. A proposed or approved task does not establish coverage. Full task goals, completion criteria, worker conclusions, and execution references are in `evidence-index.md`.\n\n")
	for _, round := range reportRounds(s) {
		fmt.Fprintf(&b, "### Round %d — %s\n\n", round.Number, round.Phase)
		if round.Purpose != "" {
			fmt.Fprintf(&b, "%s\n\n", round.Purpose)
		}
		for _, task := range round.Tasks {
			fmt.Fprintf(&b, "- `%s` — **%s**\n", task.ID, task.Status)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Findings\n\n")
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
				for _, result := range s.Results {
					if result.Task.ID == f.ValidationTask && result.Task.Verification != nil && result.Verification != nil {
						fmt.Fprintf(&b, "Challenge: %s\n\nAlternative checked: %s\n\nWorker verdict: **%s** — %s\n\n", result.Task.Verification.Claim, result.Verification.AlternativeResult, result.Verification.Verdict, result.Verification.Reason)
						break
					}
				}
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
			if len(f.SourceLocations) > 0 {
				b.WriteString("\nRecorded source locations (confirm the deployed revision separately):\n\n")
				for _, source := range f.SourceLocations {
					end := ""
					if source.EndLine > 0 && source.EndLine != source.StartLine {
						end = fmt.Sprintf("–%d", source.EndLine)
					}
					fmt.Fprintf(&b, "- %s:%d%s · %s @ %s · source artifact: %s\n", source.Path, source.StartLine, end, source.Repository, source.Revision, source.ArtifactRef)
				}
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
	priorReviews := CurrentPriorReviews(s.Plans)
	if len(priorReviews) > 0 {
		b.WriteString("## Follow-up on earlier project findings\n\nThese conclusions concern findings from earlier sessions. They rely on this session's cited worker checks; absence from a later scan alone is not evidence of remediation.\n\n")
		for _, review := range priorReviews {
			fmt.Fprintf(&b, "- **%s** — %s. %s Worker: `%s`. Evidence: %s\n", review.Verdict, review.FindingID, review.Reason, review.ValidationTask, strings.Join(review.Evidence, ", "))
		}
		b.WriteString("\n")
	}
	deniedCount := 0
	for _, result := range s.Results {
		if result.DeniedExecution != nil {
			deniedCount++
		}
	}
	if deniedCount > 0 {
		b.WriteString("## Operator-denied actions\n\nThese proposed actions did not execute and provide no test coverage. The record below identifies the action that was denied; it does not mean every later test in the worker's goal was proposed or denied.\n\n")
		for _, result := range s.Results {
			if denied := result.DeniedExecution; denied != nil {
				fmt.Fprintf(&b, "- `%s`: %s (target: %s). Approval record: %s\n", result.Task.ID, denied.Summary, denied.Target, denied.AuditRef)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("## Limitations and unresolved gaps\n\n")
	if unreviewed {
		b.WriteString("The gaps below are from the last coordinator plan and were not reconciled with subsequent worker results.\n\n")
	}
	if s.Error != "" {
		fmt.Fprintf(&b, "- Run limitation: %s\n", s.Error)
	}
	gaps := ReportGaps(s)
	for _, gap := range gaps {
		fmt.Fprintf(&b, "- %s\n", gap)
	}
	if s.Error == "" && len(gaps) == 0 {
		b.WriteString("No additional gaps were stated by the coordinator; this is not a coverage guarantee.\n")
	}
	b.WriteString("\n## Report record checks\n\n")
	readiness := AssessReportReadiness(s)
	fmt.Fprintf(&b, "**Status:** %s. These checks cover recorded fields and evidence references, not the factual accuracy of model-authored claims.\n\n", readiness.Status)
	for _, check := range readiness.Checks {
		fmt.Fprintf(&b, "- **%s (%s):** %s\n", check.Label, check.Status, check.Detail)
	}
	b.WriteString("\n## Evidence record\n\n")
	b.WriteString("Detailed worker conclusions, unsuccessful attempts, and local evidence references are in `evidence-index.md` beside this report. Raw tool output and approval records remain in the task logs. Review model-authored text for sensitive content before sharing.\n\n")
	b.WriteString("## Assessment metadata\n\n")
	fmt.Fprintf(&b, "Model: `%s`. Calls: %d/%d; failed calls: %d; reported tokens: %d; calls without usage: %d.\n\n", s.Model, s.Usage.Calls, s.Limits.ModelCalls, s.Usage.FailedCalls, s.Usage.ReportedTokens, s.Usage.CallsWithoutUsage)
	if s.ReasoningEffort != "" {
		fmt.Fprintf(&b, "Requested reasoning effort: `%s`.\n\n", s.ReasoningEffort)
	}
	if s.MaxOutputTokens > 0 {
		fmt.Fprintf(&b, "Output budget per model request: %d tokens.\n\n", s.MaxOutputTokens)
	}
	if len(s.OperatorMessages) > 0 {
		b.WriteString("The operator conversation is retained in local session state and omitted from this review draft.\n\n")
	}
	if err := writeEvidenceIndex(root, s); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "report.md"), []byte(b.String()), 0600)
}

type reportRound struct {
	Number  int
	Phase   string
	Purpose string
	Tasks   []reportTask
}

type reportTask struct {
	ID     string
	Status string
}

// The overview records what ran, while the evidence index retains full task
// instructions and results. All report formats use this same view.
func reportRounds(s State) []reportRound {
	resultStatus := make(map[string]string, len(s.Results))
	for _, result := range s.Results {
		resultStatus[result.Task.ID] = result.Status
	}
	var rounds []reportRound
	for i, decision := range s.Plans {
		if len(decision.Tasks) == 0 {
			continue
		}
		round := reportRound{Number: i + 1, Phase: decision.Phase, Purpose: strings.TrimSpace(decision.PlainSummary)}
		if round.Phase == "" {
			round.Phase = "assessment"
		}
		for _, task := range decision.Tasks {
			status := "proposed"
			for _, id := range decision.ApprovedTaskIDs {
				if id == task.ID {
					status = "approved"
				}
			}
			for _, id := range decision.SkippedTaskIDs {
				if id == task.ID {
					status = "skipped by operator"
				}
			}
			if recorded, ok := resultStatus[task.ID]; ok {
				status = recorded
			}
			round.Tasks = append(round.Tasks, reportTask{ID: task.ID, Status: status})
		}
		rounds = append(rounds, round)
	}
	return rounds
}

func writeEvidenceIndex(root string, s State) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Evidence index — %s\n\n", s.ID)
	b.WriteString("Worker-authored conclusions and local references are retained here for review; they may quote commands or results and require sensitive-content review. Full raw invocations, output, and approvals remain in the referenced task logs.\n\n")
	for _, r := range s.Results {
		fmt.Fprintf(&b, "## %s — %s\n\n%s\n\nCompletion criterion: %s\n\n%s\n\n", r.Task.ID, r.Status, r.Task.Goal, r.Task.DoneWhen, r.Summary)
		if denied := r.DeniedExecution; denied != nil {
			fmt.Fprintf(&b, "Operator-denied proposal (not executed): %s\n\nApproval record: %s\n\n", denied.Summary, denied.AuditRef)
		}
		if r.Error != "" {
			fmt.Fprintf(&b, "Limitation: %s\n\n", r.Error)
		}
		for i, e := range r.Evidence {
			if e.ExitStatus != "" {
				fmt.Fprintf(&b, "Execution %d exit status: `%s`\n\n", i+1, e.ExitStatus)
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
	return os.WriteFile(filepath.Join(root, "evidence-index.md"), []byte(b.String()), 0600)
}
