package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type validatorVerdictRecord struct {
	Target      string   `json:"target"`
	FindingType string   `json:"finding_type"`
	Title       string   `json:"title"`
	Location    string   `json:"location,omitempty"`
	Verdict     string   `json:"verdict"`
	Basis       string   `json:"basis"`
	Evidence    []string `json:"evidence,omitempty"`
}

type validatorVerdictArtifact struct {
	RunID           string                   `json:"run_id"`
	TaskID          string                   `json:"task_id"`
	WorkerID        string                   `json:"worker_id"`
	GeneratedAt     string                   `json:"generated_at"`
	CandidateCount  int                      `json:"candidate_count"`
	VerifiedCount   int                      `json:"verified_count"`
	RejectedCount   int                      `json:"rejected_count"`
	UnchangedCount  int                      `json:"unchanged_count"`
	Verdicts        []validatorVerdictRecord `json:"verdicts"`
	Role            string                   `json:"role"`
	ExecutionMethod string                   `json:"execution_method"`
}

func taskRequiresValidatorRole(task TaskSpec) bool {
	strategy := strings.ToLower(strings.TrimSpace(task.Strategy))
	if strategy == "validator" || strings.HasPrefix(strategy, "validator_") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(task.TaskID)), "validator-") {
		return true
	}
	return false
}

func runWorkerValidatorTask(manager *Manager, cfg WorkerRunConfig, task TaskSpec, workDir string) error {
	signalWorkerID := WorkerSignalID(cfg.WorkerID)
	findings, err := manager.ListFindings(cfg.RunID)
	if err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureInsufficientEvidence, map[string]any{
			"detail": "validator_list_findings_failed",
		})
		return err
	}
	artifacts, err := manager.listArtifactRecords(cfg.RunID)
	if err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureInsufficientEvidence, map[string]any{
			"detail": "validator_list_artifacts_failed",
		})
		return err
	}

	views := buildReportFindings(findings, artifacts)
	records := make([]validatorVerdictRecord, 0, len(views))
	verifiedCount := 0
	rejectedCount := 0
	candidateCount := 0
	for _, view := range views {
		state := normalizeFindingState(view.Finding.State)
		if state != FindingStateCandidate && state != FindingStateHypothesis {
			continue
		}
		candidateCount++
		verdict := "rejected"
		nextState := FindingStateRejected
		basis := "missing_linked_artifact_or_log_evidence"
		evidence := truncateWithOverflow(view.LinkedEvidence, reportMaxEvidenceItems)
		if len(view.LinkedEvidence) > 0 {
			verdict = "verified"
			nextState = FindingStateVerified
			basis = "linked_artifact_or_log_evidence"
		}
		if verdict == "verified" {
			verifiedCount++
		} else {
			rejectedCount++
		}
		metadata := map[string]any{
			"finding_state":       nextState,
			"validator_verdict":   verdict,
			"validator_basis":     basis,
			"validator_role":      "read_only",
			"validator_worker_id": cfg.WorkerID,
			"validator_task_id":   cfg.TaskID,
		}
		if location := strings.TrimSpace(view.Finding.Metadata["location"]); location != "" {
			metadata["location"] = location
		}
		_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskFinding, map[string]any{
			"target":       view.Finding.Target,
			"finding_type": view.Finding.FindingType,
			"title":        view.Finding.Title,
			"state":        nextState,
			"severity":     view.Finding.Severity,
			"confidence":   view.Finding.Confidence,
			"source":       "validator_worker",
			"evidence":     evidence,
			"metadata":     metadata,
		})
		records = append(records, validatorVerdictRecord{
			Target:      view.Finding.Target,
			FindingType: view.Finding.FindingType,
			Title:       view.Finding.Title,
			Location:    strings.TrimSpace(view.Finding.Metadata["location"]),
			Verdict:     verdict,
			Basis:       basis,
			Evidence:    evidence,
		})
	}

	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureArtifactWrite, nil)
		return err
	}
	verdictPath := filepath.Join(artifactDir, "validator_verdicts.json")
	verdictArtifact := validatorVerdictArtifact{
		RunID:           cfg.RunID,
		TaskID:          cfg.TaskID,
		WorkerID:        cfg.WorkerID,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		CandidateCount:  candidateCount,
		VerifiedCount:   verifiedCount,
		RejectedCount:   rejectedCount,
		UnchangedCount:  len(views) - candidateCount,
		Verdicts:        records,
		Role:            "validator_read_only",
		ExecutionMethod: "artifact_evidence_verdict",
	}
	verdictData, err := json.MarshalIndent(verdictArtifact, "", "  ")
	if err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureArtifactWrite, nil)
		return err
	}
	if err := os.WriteFile(verdictPath, append(verdictData, '\n'), 0o644); err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureArtifactWrite, nil)
		return err
	}
	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskArtifact, map[string]any{
		"type":      "validator_verdicts",
		"title":     fmt.Sprintf("validator verdict summary (%s)", cfg.TaskID),
		"path":      verdictPath,
		"step":      1,
		"tool_call": 0,
	})

	summary := fmt.Sprintf("validator role completed read-only review: candidates=%d verified=%d rejected=%d", candidateCount, verifiedCount, rejectedCount)
	logPath, err := writeWorkerActionLog(cfg, fmt.Sprintf("%s-a%d-validator.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt), []byte(summary+"\n"))
	if err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureArtifactWrite, nil)
		return err
	}
	producedArtifacts := []string{logPath, verdictPath}
	derived, derr := writeExpectedCommandArtifacts(cfg, task, workDir, []byte(summary+"\n"), logPath)
	if derr != nil {
		_ = emitWorkerFailure(manager, cfg, task, derr, WorkerFailureArtifactWrite, nil)
		return derr
	}
	producedArtifacts = appendUnique(producedArtifacts, derived...)

	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskFinding, map[string]any{
		"target":       primaryTaskTarget(task),
		"finding_type": "task_execution_result",
		"title":        "validator task completed",
		"state":        FindingStateVerified,
		"severity":     "info",
		"confidence":   "high",
		"source":       "validator_worker",
		"evidence":     producedArtifacts,
		"metadata": map[string]any{
			"attempt":         cfg.Attempt,
			"reason":          "validator_review_completed",
			"validator_role":  "read_only",
			"candidate_count": candidateCount,
			"verified_count":  verifiedCount,
			"rejected_count":  rejectedCount,
		},
	})
	_ = manager.EmitEvent(cfg.RunID, signalWorkerID, cfg.TaskID, EventTypeTaskCompleted, map[string]any{
		"attempt":   cfg.Attempt,
		"worker_id": cfg.WorkerID,
		"reason":    "validator_review_completed",
		"log_path":  logPath,
		"completion_contract": map[string]any{
			"status_reason":       "validator_review_completed",
			"required_artifacts":  requiredArtifactsForCompletion(task),
			"produced_artifacts":  producedArtifacts,
			"required_findings":   []string{"task_execution_result"},
			"produced_findings":   []string{"task_execution_result"},
			"verification_status": "reported_by_worker",
		},
	})
	return nil
}
