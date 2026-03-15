package orchestrator

import (
	"fmt"
	"os"
	"strings"
)

const (
	hardSupportPolicyEnv = "BIRDHACKBOT_HARD_SUPPORT"
	hardSupportPolicyRef = "docs/runbooks/architecture-recovery-plan.md#hard-support-exception-policy"
)

type hardSupportCapability string

const (
	hardSupportReportSynthesis       hardSupportCapability = "report_synthesis"
	hardSupportVulnerabilityEvidence hardSupportCapability = "vulnerability_evidence"
	hardSupportArchiveWorkflow       hardSupportCapability = "archive_workflow"
)

type hardSupportDecision struct {
	Capability hardSupportCapability
	Allowed    bool
	Gate       string
	Reason     string
}

func decideHardSupportException(task TaskSpec, capability hardSupportCapability) hardSupportDecision {
	if decision, ok := decideHardSupportFromEnv(capability); ok {
		return decision
	}
	switch capability {
	case hardSupportReportSynthesis:
		allowed := taskRequiresReportSynthesis(task) && len(task.DependsOn) > 0
		return hardSupportDecision{
			Capability: capability,
			Allowed:    allowed,
			Gate:       "capability_scope",
			Reason:     "report_synthesis_task_with_dependencies",
		}
	case hardSupportVulnerabilityEvidence:
		allowed := taskRequiresVulnerabilityEvidence(task)
		return hardSupportDecision{
			Capability: capability,
			Allowed:    allowed,
			Gate:       "capability_scope",
			Reason:     "vulnerability_evidence_task",
		}
	case hardSupportArchiveWorkflow:
		allowed := taskLikelyLocalFileWorkflow(task)
		return hardSupportDecision{
			Capability: capability,
			Allowed:    allowed,
			Gate:       "capability_scope",
			Reason:     "local_archive_workflow_task",
		}
	default:
		return hardSupportDecision{
			Capability: capability,
			Allowed:    false,
			Gate:       "capability_scope",
			Reason:     "unsupported_capability",
		}
	}
}

func decideHardSupportFromEnv(capability hardSupportCapability) (hardSupportDecision, bool) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(hardSupportPolicyEnv)))
	if raw == "" {
		return hardSupportDecision{}, false
	}
	set := map[string]struct{}{}
	for _, token := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(token)
		if trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	if _, ok := set["none"]; ok {
		return hardSupportDecision{
			Capability: capability,
			Allowed:    false,
			Gate:       "env",
			Reason:     "env_none",
		}, true
	}
	if _, ok := set["all"]; ok {
		return hardSupportDecision{
			Capability: capability,
			Allowed:    true,
			Gate:       "env",
			Reason:     "env_all",
		}, true
	}
	alias := map[hardSupportCapability]string{
		hardSupportReportSynthesis:       "report",
		hardSupportVulnerabilityEvidence: "vuln",
		hardSupportArchiveWorkflow:       "archive",
	}
	_, allowed := set[string(capability)]
	if !allowed {
		if short, ok := alias[capability]; ok {
			_, allowed = set[short]
		}
	}
	return hardSupportDecision{
		Capability: capability,
		Allowed:    allowed,
		Gate:       "env",
		Reason:     "env_capability_list",
	}, true
}

func annotateHardSupportNote(note string, decision hardSupportDecision) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return note
	}
	return fmt.Sprintf("%s [hard-support capability=%s gate=%s reason=%s policy=%s]", note, decision.Capability, decision.Gate, decision.Reason, hardSupportPolicyRef)
}

func annotateHardSupportNotes(notes []string, decision hardSupportDecision) []string {
	if len(notes) == 0 {
		return nil
	}
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		annotated := annotateHardSupportNote(note, decision)
		if strings.TrimSpace(annotated) == "" {
			continue
		}
		out = append(out, annotated)
	}
	return out
}
