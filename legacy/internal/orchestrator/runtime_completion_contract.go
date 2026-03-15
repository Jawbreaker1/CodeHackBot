package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func buildCommandCompletionContract(
	task TaskSpec,
	cfg WorkerRunConfig,
	workDir string,
	command string,
	output []byte,
	requiredArtifacts []string,
	producedArtifacts []string,
) (map[string]any, error) {
	producedArtifacts = compactStrings(producedArtifacts)
	if len(producedArtifacts) == 0 {
		return nil, fmt.Errorf("completion contract requires produced_artifacts")
	}
	outputText := strings.TrimSpace(string(output))
	if hasNoCommandOutputMarker(outputText) {
		return nil, fmt.Errorf("completion contract rejected: output indicates no command output captured")
	}

	evidenceRefs := append([]string{}, producedArtifacts...)
	resolvedEvidence := resolveCompletionEvidenceRefs(cfg, task, workDir, evidenceRefs)
	if len(resolvedEvidence) == 0 {
		return nil, fmt.Errorf("completion contract rejected: evidence_refs did not resolve to readable artifacts")
	}
	if outputText == "" && !hasMeaningfulEvidenceForEmptyCommandOutput(resolvedEvidence) {
		return nil, fmt.Errorf("completion contract rejected: command produced no meaningful execution evidence")
	}
	if archiveTaskRequiresPositiveProof(task) && !evidenceShowsArchiveProof(resolvedEvidence) {
		return nil, fmt.Errorf("completion contract rejected: evidence does not prove archive objective")
	}

	whyMet := strings.TrimSpace(firstNonEmpty(
		task.Goal,
		task.Title,
		fmt.Sprintf("command %s completed with evidence-backed artifacts", strings.TrimSpace(filepath.Base(command))),
	))
	if whyMet == "" {
		whyMet = "command action completed with evidence-backed artifacts"
	}

	contract := map[string]any{
		"status_reason":       "action_completed",
		"required_artifacts":  compactStrings(requiredArtifacts),
		"produced_artifacts":  producedArtifacts,
		"required_findings":   []string{"task_execution_result"},
		"produced_findings":   []string{"task_execution_result"},
		"verification_status": "reported_by_worker",
		"objective_met":       true,
		"why_met":             whyMet,
		"evidence_refs":       evidenceRefs,
		"semantic_verifier":   "local_runtime_evidence",
	}
	return contract, nil
}

func hasMeaningfulEvidenceForEmptyCommandOutput(paths []string) bool {
	for _, path := range paths {
		base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
		if base == "" {
			continue
		}
		if base == "resolved_target.json" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(capBytes(data, 8*1024)))
		if content == "" || hasNoCommandOutputMarker(content) {
			continue
		}
		return true
	}
	return false
}

func hasNoCommandOutputMarker(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "no command output captured")
}
