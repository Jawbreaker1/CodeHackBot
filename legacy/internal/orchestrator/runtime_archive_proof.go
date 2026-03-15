package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func archiveTaskRequiresPositiveProof(task TaskSpec) bool {
	if !taskLikelyLocalFileWorkflow(task) {
		return false
	}
	strategy := strings.ToLower(strings.TrimSpace(task.Strategy))
	if strings.Contains(strategy, "proof") || strings.Contains(strategy, "access_validation") {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		strings.Join(task.DoneWhen, " "),
		strings.Join(task.FailWhen, " "),
		strings.Join(task.ExpectedArtifacts, " "),
	}, " ")))
	return containsAnySubstring(
		text,
		"proof-of-access",
		"proof of access",
		"proof token",
		"recovered password",
		"password found",
		"identify the password",
		"decrypt",
		"decryption",
		"extract contents",
		"validate access",
	)
}

func localWorkflowSensitiveArtifactName(name string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	if base == "" {
		return false
	}
	switch base {
	case "password_found", "password_found.txt", "recovered_password.txt", "proof_of_access.txt", "proof_token.txt":
		return true
	}
	if strings.Contains(base, "proof") || strings.Contains(base, "token") {
		return true
	}
	if !strings.Contains(base, "password") {
		return false
	}
	// Password attempt/status logs are valid operational evidence but are not
	// themselves proof-sensitive outputs. Keep strict concrete-source gating for
	// artifacts that claim recovered credentials or equivalent proof material.
	return containsAnySubstring(
		base,
		"found",
		"recovered",
		"credential",
		"secret",
		"key",
		"decrypted",
		"decrypt",
		"access",
	)
}

func localWorkflowCriticalArtifactName(name string) bool {
	if localWorkflowSensitiveArtifactName(name) {
		return true
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	if base == "" {
		return false
	}
	if containsAnySubstring(
		base,
		"extraction_status",
		"extract_status",
		"extraction_result",
		"decrypt_status",
		"decryption_status",
	) {
		return true
	}
	if strings.Contains(base, "extract") && containsAnySubstring(base, "files", "content", "result", "status", "success") {
		return true
	}
	if strings.Contains(base, "decrypt") && containsAnySubstring(base, "status", "result", "success") {
		return true
	}
	return false
}

func validateAssistCompletionContract(task TaskSpec, cfg WorkerRunConfig, workDir string, suggestion assist.Suggestion) error {
	if !archiveTaskRequiresPositiveProof(task) {
		return nil
	}
	if suggestion.ObjectiveMet == nil || !*suggestion.ObjectiveMet {
		return fmt.Errorf("completion contract requires objective_met=true for proof-sensitive archive workflow")
	}
	refs := compactStrings(suggestion.EvidenceRefs)
	if len(refs) == 0 {
		return fmt.Errorf("completion contract requires evidence_refs for proof-sensitive archive workflow")
	}
	evidencePaths := resolveCompletionEvidenceRefs(cfg, task, workDir, refs)
	if len(evidencePaths) == 0 {
		return fmt.Errorf("completion contract evidence_refs did not resolve to existing artifacts/files")
	}
	if !evidenceShowsArchiveProof(evidencePaths) {
		return fmt.Errorf("completion contract evidence does not yet prove recovered password or successful proof extraction")
	}
	return nil
}

func resolveCompletionEvidenceRefs(cfg WorkerRunConfig, task TaskSpec, workDir string, refs []string) []string {
	artifactRoot := BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir
	taskArtifactRoot := filepath.Join(artifactRoot, task.TaskID)
	out := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			continue
		}
		candidates := []string{}
		if filepath.IsAbs(trimmed) {
			candidates = append(candidates, filepath.Clean(trimmed))
		} else {
			clean := filepath.Clean(trimmed)
			candidates = append(candidates,
				filepath.Clean(filepath.Join(workDir, clean)),
				filepath.Clean(filepath.Join(taskArtifactRoot, clean)),
				filepath.Clean(filepath.Join(artifactRoot, clean)),
			)
		}
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
		}
	}
	return out
}

func evidenceShowsArchiveProof(paths []string) bool {
	for _, path := range paths {
		base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(capBytes(data, 65536)))
		if content == "" {
			continue
		}
		lower := strings.ToLower(content)

		if localWorkflowSensitiveArtifactName(base) && !hasNegativeProofLanguage(lower) {
			return true
		}
		if hasPositiveProofLanguage(lower) && !hasNegativeProofLanguage(lower) {
			return true
		}
	}
	return false
}

func hasPositiveProofLanguage(lower string) bool {
	return containsAnySubstring(lower,
		"password found",
		"recovered password",
		"password recovered",
		"password:",
		"access_validated",
		"proof_of_access",
		"proof token",
		"decryption successful",
		"archive extracted",
	)
}

func hasNegativeProofLanguage(lower string) bool {
	return containsAnySubstring(lower,
		"not_recovered",
		"no password",
		"password not found",
		"0 password hashes cracked",
		"failed",
	)
}
