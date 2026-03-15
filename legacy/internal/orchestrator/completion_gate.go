package orchestrator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type CompletionVerificationGateSummary struct {
	TotalCompletedTasks    int      `json:"total_completed_tasks"`
	VerifiedCompletedTasks int      `json:"verified_completed_tasks"`
	UnverifiedTasks        int      `json:"unverified_tasks"`
	UnverifiedTaskIDs      []string `json:"unverified_task_ids,omitempty"`
	VerificationGate       string   `json:"verification_gate"`
	VerificationGateReason string   `json:"verification_gate_reason,omitempty"`
}

func (m *Manager) EvaluateCompletionVerificationGate(runID string) (CompletionVerificationGateSummary, error) {
	if strings.TrimSpace(runID) == "" {
		return CompletionVerificationGateSummary{}, fmt.Errorf("run id is required")
	}
	plan, err := m.LoadRunPlan(runID)
	if err != nil {
		return CompletionVerificationGateSummary{}, err
	}
	leases, err := m.ReadLeases(runID)
	if err != nil {
		return CompletionVerificationGateSummary{}, err
	}
	events, err := m.Events(runID, 0)
	if err != nil {
		return CompletionVerificationGateSummary{}, err
	}
	artifacts, err := m.listArtifactRecords(runID)
	if err != nil {
		return CompletionVerificationGateSummary{}, err
	}

	taskByID := make(map[string]TaskSpec, len(plan.Tasks))
	for _, task := range plan.Tasks {
		taskByID[strings.TrimSpace(task.TaskID)] = task
	}

	latestTaskCompleted := map[string]map[string]any{}
	findingTypesByTask := map[string][]string{}
	artifactPathsByTask := map[string][]string{}
	for _, item := range artifacts {
		taskID := strings.TrimSpace(item.Artifact.TaskID)
		if taskID == "" {
			continue
		}
		if path := strings.TrimSpace(item.Artifact.Path); path != "" {
			artifactPathsByTask[taskID] = appendUnique(artifactPathsByTask[taskID], path)
		}
		if record := strings.TrimSpace(item.RecordPath); record != "" {
			artifactPathsByTask[taskID] = appendUnique(artifactPathsByTask[taskID], record)
		}
	}
	for _, event := range events {
		taskID := strings.TrimSpace(event.TaskID)
		if taskID == "" {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		switch event.Type {
		case EventTypeTaskCompleted:
			latestTaskCompleted[taskID] = payload
		case EventTypeTaskFinding:
			findingType := strings.TrimSpace(toString(payload["finding_type"]))
			if findingType != "" {
				findingTypesByTask[taskID] = appendUnique(findingTypesByTask[taskID], findingType)
			}
		case EventTypeTaskArtifact:
			path := strings.TrimSpace(toString(payload["path"]))
			if path != "" {
				artifactPathsByTask[taskID] = appendUnique(artifactPathsByTask[taskID], path)
			}
		}
	}

	summary := CompletionVerificationGateSummary{
		VerificationGate: "pass",
	}
	issues := make([]string, 0)
	for _, lease := range leases {
		if strings.TrimSpace(lease.Status) != LeaseStatusCompleted {
			continue
		}
		taskID := strings.TrimSpace(lease.TaskID)
		if taskID == "" {
			continue
		}
		summary.TotalCompletedTasks++
		task := taskByID[taskID]

		completedPayload, ok := latestTaskCompleted[taskID]
		if !ok {
			summary.UnverifiedTasks++
			summary.UnverifiedTaskIDs = append(summary.UnverifiedTaskIDs, taskID)
			issues = append(issues, fmt.Sprintf("%s(no task_completed evidence)", taskID))
			continue
		}
		contract, ok := completionContractFromPayload(completedPayload["completion_contract"])
		if !ok {
			summary.UnverifiedTasks++
			summary.UnverifiedTaskIDs = append(summary.UnverifiedTaskIDs, taskID)
			issues = append(issues, fmt.Sprintf("%s(missing completion_contract)", taskID))
			continue
		}
		reasons := make([]string, 0, 3)
		verificationStatus := strings.ToLower(strings.TrimSpace(toString(contract["verification_status"])))
		if verificationStatus == "" || verificationStatus == "failed" {
			reasons = append(reasons, "verification_status="+nonEmptyOrFallback(verificationStatus, "missing"))
		}
		if archiveTaskRequiresPositiveProof(task) {
			if objectiveMet, ok := completionContractObjectiveMet(contract); !ok || !objectiveMet {
				reasons = append(reasons, "objective_met=false")
			}
		}

		requiredArtifacts := compactStrings(sliceFromAny(contract["required_artifacts"]))
		if len(requiredArtifacts) == 0 {
			requiredArtifacts = compactStrings(task.ExpectedArtifacts)
		}
		producedArtifacts := compactStrings(sliceFromAny(contract["produced_artifacts"]))
		if logPath := strings.TrimSpace(toString(completedPayload["log_path"])); logPath != "" {
			producedArtifacts = appendUnique(producedArtifacts, logPath)
		}
		producedArtifacts = appendUnique(producedArtifacts, artifactPathsByTask[taskID]...)
		if missingArtifacts := verifyRequiredArtifacts(requiredArtifacts, producedArtifacts); len(missingArtifacts) > 0 {
			reasons = append(reasons, "missing_artifacts="+strings.Join(missingArtifacts, ","))
		}

		requiredFindings := compactStrings(sliceFromAny(contract["required_findings"]))
		if len(requiredFindings) == 0 && !completionContractAllowsNoFindings(contract) {
			requiredFindings = []string{"task_execution_result"}
		}
		producedFindings := compactStrings(sliceFromAny(contract["produced_findings"]))
		producedFindings = appendUnique(producedFindings, findingTypesByTask[taskID]...)
		if missingFindings := verifyRequiredFindings(requiredFindings, producedFindings); len(missingFindings) > 0 {
			reasons = append(reasons, "missing_findings="+strings.Join(missingFindings, ","))
		}

		if len(reasons) > 0 {
			summary.UnverifiedTasks++
			summary.UnverifiedTaskIDs = append(summary.UnverifiedTaskIDs, taskID)
			issues = append(issues, fmt.Sprintf("%s(%s)", taskID, strings.Join(reasons, "; ")))
			continue
		}
		summary.VerifiedCompletedTasks++
	}

	summary.UnverifiedTaskIDs = compactStrings(summary.UnverifiedTaskIDs)
	sort.Strings(summary.UnverifiedTaskIDs)
	if len(issues) > 0 {
		sort.Strings(issues)
		summary.VerificationGate = "fail"
		if len(issues) > 3 {
			summary.VerificationGateReason = fmt.Sprintf("unverified completion contracts for %d completed tasks: %s ...", len(summary.UnverifiedTaskIDs), strings.Join(issues[:3], ", "))
		} else {
			summary.VerificationGateReason = "unverified completion contracts: " + strings.Join(issues, ", ")
		}
	}
	return summary, nil
}

func completionContractFromPayload(raw any) (map[string]any, bool) {
	switch typed := raw.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out, true
	default:
		return nil, false
	}
}
