package orchestrator

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type completionContractCheck struct {
	Status            string
	Reason            string
	RequiredArtifacts []string
	ProducedArtifacts []string
	VerifiedArtifacts []string
	MissingArtifacts  []string
	RequiredFindings  []string
	ProducedFindings  []string
	MissingFindings   []string
}

func (c *Coordinator) validateTaskCompletionContract(task TaskSpec, taskID, signalWorkerID string, events []EventEnvelope, completionPayload map[string]any) (completionContractCheck, error) {
	contract := map[string]any{}
	if raw, ok := completionPayload["completion_contract"]; ok {
		switch typed := raw.(type) {
		case map[string]any:
			contract = typed
		case map[string]string:
			for k, v := range typed {
				contract[k] = v
			}
		default:
			return completionContractCheck{}, fmt.Errorf("invalid completion_contract payload type %T", raw)
		}
	}

	requiredArtifacts := compactStrings(sliceFromAny(contract["required_artifacts"]))
	if len(requiredArtifacts) == 0 {
		requiredArtifacts = compactStrings(task.ExpectedArtifacts)
	}
	requiredFindings := compactStrings(sliceFromAny(contract["required_findings"]))
	if len(requiredFindings) == 0 {
		requiredFindings = []string{"task_execution_result"}
	}

	producedArtifacts := c.collectProducedArtifacts(taskID, signalWorkerID, events, completionPayload, contract)
	verifiedArtifacts, err := c.verifyProducedArtifacts(producedArtifacts)
	if err != nil {
		return completionContractCheck{}, err
	}
	producedFindings := collectProducedFindingTypes(task, taskID, signalWorkerID, events)

	missingArtifacts := verifyRequiredArtifacts(requiredArtifacts, verifiedArtifacts)
	missingFindings := verifyRequiredFindings(requiredFindings, producedFindings)
	status := "satisfied"
	reason := ""
	if len(missingArtifacts) > 0 || len(missingFindings) > 0 {
		status = "failed"
		reason = "missing_required_artifacts"
	}

	return completionContractCheck{
		Status:            status,
		Reason:            reason,
		RequiredArtifacts: requiredArtifacts,
		ProducedArtifacts: producedArtifacts,
		VerifiedArtifacts: verifiedArtifacts,
		MissingArtifacts:  missingArtifacts,
		RequiredFindings:  requiredFindings,
		ProducedFindings:  producedFindings,
		MissingFindings:   missingFindings,
	}, nil
}

func (c *Coordinator) collectProducedArtifacts(taskID, signalWorkerID string, events []EventEnvelope, completionPayload map[string]any, contract map[string]any) []string {
	out := []string{}
	for _, value := range sliceFromAny(contract["produced_artifacts"]) {
		out = append(out, value)
	}
	if path := strings.TrimSpace(toString(completionPayload["log_path"])); path != "" {
		out = append(out, path)
	}
	for _, event := range events {
		if event.Type != EventTypeTaskArtifact || event.TaskID != taskID || event.WorkerID != signalWorkerID {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if path := strings.TrimSpace(toString(payload["path"])); path != "" {
			out = append(out, path)
		}
	}
	return compactStrings(out)
}

func (c *Coordinator) verifyProducedArtifacts(paths []string) ([]string, error) {
	verified := make([]string, 0, len(paths))
	for _, path := range paths {
		resolved, ok, err := c.resolveReadableArtifactPath(path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if info.Size() <= 0 {
			continue
		}
		verified = append(verified, path)
	}
	return compactStrings(verified), nil
}

func verifyRequiredArtifacts(required, verified []string) []string {
	if len(required) == 0 {
		return nil
	}
	available := make([]string, 0, len(verified))
	for _, item := range verified {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		available = append(available, trimmed)
	}
	used := make([]bool, len(available))
	missing := make([]string, 0)
	for _, item := range required {
		requirement := strings.TrimSpace(item)
		if requirement == "" {
			continue
		}
		matchIdx := -1
		for i, candidate := range available {
			if used[i] {
				continue
			}
			if artifactRequirementMatches(requirement, candidate) {
				matchIdx = i
				break
			}
		}
		if matchIdx >= 0 {
			used[matchIdx] = true
			continue
		}
		// Backward-compatible fallback for non-concrete artifact labels
		// (e.g. "command log"): allow any remaining verified artifact.
		if !artifactRequirementNeedsConcreteMatch(requirement) {
			for i := range available {
				if used[i] {
					continue
				}
				used[i] = true
				matchIdx = i
				break
			}
			if matchIdx >= 0 {
				continue
			}
		}
		missing = append(missing, requirement)
	}
	return compactStrings(missing)
}

func artifactRequirementMatches(requirement, candidate string) bool {
	required := strings.ToLower(strings.TrimSpace(requirement))
	current := strings.ToLower(strings.TrimSpace(candidate))
	if required == "" || current == "" {
		return false
	}
	if required == current {
		return true
	}
	requiredBase := strings.ToLower(strings.TrimSpace(filepath.Base(required)))
	candidateBase := strings.ToLower(strings.TrimSpace(filepath.Base(current)))
	if requiredBase != "" && requiredBase == candidateBase {
		return true
	}
	// Handle relative required paths where verified artifacts are absolute.
	if strings.Contains(required, string(filepath.Separator)) && strings.HasSuffix(current, required) {
		return true
	}
	return false
}

func artifactRequirementNeedsConcreteMatch(requirement string) bool {
	trimmed := strings.TrimSpace(requirement)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, string(filepath.Separator)) {
		return true
	}
	return filepath.Ext(trimmed) != ""
}

func verifyRequiredFindings(required, produced []string) []string {
	if len(required) == 0 {
		return nil
	}
	producedSet := map[string]struct{}{}
	for _, finding := range produced {
		producedSet[strings.ToLower(strings.TrimSpace(finding))] = struct{}{}
	}
	missing := make([]string, 0)
	for _, requiredType := range required {
		if _, ok := producedSet[strings.ToLower(strings.TrimSpace(requiredType))]; ok {
			continue
		}
		missing = append(missing, requiredType)
	}
	return compactStrings(missing)
}

func collectProducedFindingTypes(task TaskSpec, taskID, signalWorkerID string, events []EventEnvelope) []string {
	out := []string{}
	for _, event := range events {
		if event.Type != EventTypeTaskFinding || event.TaskID != taskID || event.WorkerID != signalWorkerID {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		findingType := strings.TrimSpace(toString(payload["finding_type"]))
		if findingType == "" {
			continue
		}
		target := strings.TrimSpace(toString(payload["target"]))
		if !findingMatchesTaskTarget(task.Targets, target) {
			continue
		}
		out = append(out, findingType)
	}
	return compactStrings(out)
}

func findingMatchesTaskTarget(taskTargets []string, findingTarget string) bool {
	trimmedFinding := strings.TrimSpace(strings.ToLower(findingTarget))
	if len(taskTargets) == 0 || trimmedFinding == "" {
		return true
	}
	findingIP := net.ParseIP(trimmedFinding)
	for _, rawTarget := range taskTargets {
		target := strings.TrimSpace(strings.ToLower(rawTarget))
		if target == "" {
			continue
		}
		if target == trimmedFinding {
			return true
		}
		if findingIP != nil && strings.Contains(target, "/") {
			if _, cidr, err := net.ParseCIDR(target); err == nil && cidr.Contains(findingIP) {
				return true
			}
		}
	}
	return false
}

func (c *Coordinator) resolveReadableArtifactPath(path string) (string, bool, error) {
	candidate := strings.TrimSpace(path)
	if candidate == "" {
		return "", false, nil
	}
	paths := []string{candidate}
	if !filepath.IsAbs(candidate) {
		paths = append(paths, filepath.Join(c.manager.SessionsDir, candidate))
	}
	for _, value := range paths {
		info, err := os.Stat(value)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, err
		}
		if info.IsDir() {
			continue
		}
		return value, true, nil
	}
	return "", false, nil
}

func latestWorkerTaskCompletedPayload(events []EventEnvelope, taskID, workerID string) (map[string]any, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != EventTypeTaskCompleted || event.TaskID != taskID || event.WorkerID != workerID {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		return payload, true
	}
	return nil, false
}

func sliceFromAny(v any) []string {
	switch typed := v.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, toString(item))
		}
		return out
	default:
		return nil
	}
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if slices.Contains(out, trimmed) {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
