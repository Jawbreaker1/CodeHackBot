package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	memoryMaxKnownFacts    = 20
	memoryMaxOpenQuestions = 20
	memoryProvenanceFile   = "known_facts_provenance.json"
	memoryContractFile     = "shared_memory_contract.json"
	memoryWriterPolicyRef  = "single_writer_orchestrator_events_artifacts_only"
)

type MemoryContext struct {
	UpdatedAt                   time.Time `json:"updated_at"`
	RunID                       string    `json:"run_id"`
	Goal                        string    `json:"goal,omitempty"`
	NormalizedGoal              string    `json:"normalized_goal,omitempty"`
	PlannerMode                 string    `json:"planner_mode,omitempty"`
	PlannerVersion              string    `json:"planner_version,omitempty"`
	PlannerModel                string    `json:"planner_model,omitempty"`
	PlannerPlaybooks            []string  `json:"planner_playbooks,omitempty"`
	PlannerPromptHash           string    `json:"planner_prompt_hash,omitempty"`
	PlannerDecision             string    `json:"planner_decision,omitempty"`
	PlannerRationale            string    `json:"planner_rationale,omitempty"`
	RegenerationCount           int       `json:"regeneration_count,omitempty"`
	HypothesisCount             int       `json:"hypothesis_count"`
	ArtifactCount               int       `json:"artifact_count"`
	FindingCount                int       `json:"finding_count"`
	KnownFactsCount             int       `json:"known_facts_count"`
	OpenQuestionsCount          int       `json:"open_questions_count"`
	KnownFactsRetained          int       `json:"known_facts_retained,omitempty"`
	OpenQuestionsRetained       int       `json:"open_questions_retained,omitempty"`
	KnownFactsDropped           int       `json:"known_facts_dropped,omitempty"`
	OpenQuestionsDropped        int       `json:"open_questions_dropped,omitempty"`
	KnownFactsProvenance        int       `json:"known_facts_provenance_count,omitempty"`
	SharedMemoryWriter          string    `json:"shared_memory_writer,omitempty"`
	SharedMemoryPolicy          string    `json:"shared_memory_policy,omitempty"`
	SharedMemoryViolations      int       `json:"shared_memory_violations,omitempty"`
	SharedMemoryViolationDetail string    `json:"shared_memory_violation_detail,omitempty"`
	Compacted                   bool      `json:"compacted"`
}

type MemoryStateTransition struct {
	From          string `json:"from,omitempty"`
	To            string `json:"to"`
	Resolution    string `json:"resolution,omitempty"`
	SourceEventID string `json:"source_event_id,omitempty"`
	SourceTaskID  string `json:"source_task_id,omitempty"`
	SourceWorker  string `json:"source_worker_id,omitempty"`
}

type MemoryFactProvenance struct {
	DedupeKey            string                  `json:"dedupe_key"`
	TaskID               string                  `json:"task_id,omitempty"`
	Target               string                  `json:"target,omitempty"`
	FindingType          string                  `json:"finding_type,omitempty"`
	Title                string                  `json:"title"`
	CurrentState         string                  `json:"current_state"`
	PromotedToKnownFacts bool                    `json:"promoted_to_known_facts"`
	KnownFact            string                  `json:"known_fact,omitempty"`
	SourceEventIDs       []string                `json:"source_event_ids,omitempty"`
	SourceTaskIDs        []string                `json:"source_task_ids,omitempty"`
	SourceWorkerIDs      []string                `json:"source_worker_ids,omitempty"`
	StateTransitions     []MemoryStateTransition `json:"state_transitions,omitempty"`
}

type memoryProvenanceEnvelope struct {
	RunID        string                 `json:"run_id"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Writer       string                 `json:"writer"`
	Policy       string                 `json:"policy"`
	FindingCount int                    `json:"finding_count"`
	Records      []MemoryFactProvenance `json:"records"`
}

type sharedMemoryContract struct {
	RunID        string            `json:"run_id"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Writer       string            `json:"writer"`
	Policy       string            `json:"policy"`
	ManagedFiles map[string]string `json:"managed_files"`
}

type sharedMemoryViolation struct {
	Reason string
	Files  []string
}

func (m *Manager) InitializeMemoryBank(runID string, plan RunPlan) error {
	paths := BuildRunPaths(m.SessionsDir, runID)
	if err := os.MkdirAll(paths.MemoryDir, 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}
	hypotheses := plan.Metadata.Hypotheses
	knownFacts := []string{
		fmt.Sprintf("Goal: %s", plan.Metadata.NormalizedGoal),
		fmt.Sprintf("Scope targets: %s", strings.Join(plan.Scope.Targets, ", ")),
		fmt.Sprintf("Scope networks: %s", strings.Join(plan.Scope.Networks, ", ")),
		fmt.Sprintf("Constraints: %s", strings.Join(plan.Constraints, ", ")),
		fmt.Sprintf("Planner decision: %s", plan.Metadata.PlannerDecision),
	}
	openQuestions := memoryOpenQuestions(hypotheses)
	ctx := MemoryContext{
		UpdatedAt:             m.Now(),
		RunID:                 runID,
		Goal:                  plan.Metadata.Goal,
		NormalizedGoal:        plan.Metadata.NormalizedGoal,
		PlannerMode:           plan.Metadata.PlannerMode,
		PlannerVersion:        plan.Metadata.PlannerVersion,
		PlannerModel:          plan.Metadata.PlannerModel,
		PlannerPlaybooks:      append([]string{}, plan.Metadata.PlannerPlaybooks...),
		PlannerPromptHash:     plan.Metadata.PlannerPromptHash,
		PlannerDecision:       plan.Metadata.PlannerDecision,
		PlannerRationale:      plan.Metadata.PlannerRationale,
		RegenerationCount:     plan.Metadata.RegenerationCount,
		HypothesisCount:       len(hypotheses),
		KnownFactsCount:       len(knownFacts),
		OpenQuestionsCount:    len(openQuestions),
		KnownFactsRetained:    len(knownFacts),
		OpenQuestionsRetained: len(openQuestions),
		SharedMemoryWriter:    orchestratorWorkerID,
		SharedMemoryPolicy:    memoryWriterPolicyRef,
	}
	if err := m.writeMemoryFiles(paths.MemoryDir, hypotheses, knownFacts, openQuestions, nil, ctx); err != nil {
		return err
	}
	return nil
}

func (m *Manager) RefreshMemoryBank(runID string) error {
	plan, err := m.LoadRunPlan(runID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	paths := BuildRunPaths(m.SessionsDir, runID)
	if err := os.MkdirAll(paths.MemoryDir, 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}
	findings, err := m.ListFindings(runID)
	if err != nil {
		return err
	}
	sort.Slice(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if left.TaskID != right.TaskID {
			return left.TaskID < right.TaskID
		}
		return left.Title < right.Title
	})
	violationDetail := ""
	violationCount := 0
	if violation, checkErr := detectSharedMemoryViolation(paths.MemoryDir); checkErr != nil {
		violationCount = 1
		violationDetail = "contract_check_error"
		_ = m.EmitEvent(runID, orchestratorWorkerID, "", EventTypeRunWarning, map[string]any{
			"reason": "shared_memory_contract_check_failed",
			"error":  checkErr.Error(),
			"policy": memoryWriterPolicyRef,
		})
	} else if violation != nil {
		violationCount = 1
		violationDetail = violation.Reason
		_ = m.EmitEvent(runID, orchestratorWorkerID, "", EventTypeRunWarning, map[string]any{
			"reason":  "shared_memory_contract_violation",
			"detail":  violation.Reason,
			"files":   violation.Files,
			"policy":  memoryWriterPolicyRef,
			"run_id":  runID,
			"writer":  orchestratorWorkerID,
			"message": "shared-memory drift detected; orchestrator will reconcile from event/artifact evidence",
		})
	}
	knownFacts, knownFactProvenance := memoryKnownFactsWithProvenance(plan, findings)
	openQuestions := memoryOpenQuestions(plan.Metadata.Hypotheses)
	knownFactsTotal := len(knownFacts)
	openQuestionsTotal := len(openQuestions)
	artifactCount, findingCount, err := m.CountEvidence(runID)
	if err != nil {
		return err
	}
	compacted := knownFactsTotal > memoryMaxKnownFacts || openQuestionsTotal > memoryMaxOpenQuestions
	knownFacts = compactKnownFactsEntries(knownFacts, memoryMaxKnownFacts)
	openQuestions = compactTailEntries(openQuestions, memoryMaxOpenQuestions)
	ctx := MemoryContext{
		UpdatedAt:                   m.Now(),
		RunID:                       runID,
		Goal:                        plan.Metadata.Goal,
		NormalizedGoal:              plan.Metadata.NormalizedGoal,
		PlannerMode:                 plan.Metadata.PlannerMode,
		PlannerVersion:              plan.Metadata.PlannerVersion,
		PlannerModel:                plan.Metadata.PlannerModel,
		PlannerPlaybooks:            append([]string{}, plan.Metadata.PlannerPlaybooks...),
		PlannerPromptHash:           plan.Metadata.PlannerPromptHash,
		PlannerDecision:             plan.Metadata.PlannerDecision,
		PlannerRationale:            plan.Metadata.PlannerRationale,
		RegenerationCount:           plan.Metadata.RegenerationCount,
		HypothesisCount:             len(plan.Metadata.Hypotheses),
		ArtifactCount:               artifactCount,
		FindingCount:                findingCount,
		KnownFactsCount:             knownFactsTotal,
		OpenQuestionsCount:          openQuestionsTotal,
		KnownFactsRetained:          len(knownFacts),
		OpenQuestionsRetained:       len(openQuestions),
		KnownFactsDropped:           maxInt(0, knownFactsTotal-len(knownFacts)),
		OpenQuestionsDropped:        maxInt(0, openQuestionsTotal-len(openQuestions)),
		KnownFactsProvenance:        len(knownFactProvenance),
		SharedMemoryWriter:          orchestratorWorkerID,
		SharedMemoryPolicy:          memoryWriterPolicyRef,
		SharedMemoryViolations:      violationCount,
		SharedMemoryViolationDetail: strings.TrimSpace(violationDetail),
		Compacted:                   compacted,
	}
	return m.writeMemoryFiles(paths.MemoryDir, plan.Metadata.Hypotheses, knownFacts, openQuestions, knownFactProvenance, ctx)
}

func (m *Manager) writeMemoryFiles(memoryDir string, hypotheses []Hypothesis, knownFacts, openQuestions []string, provenance []MemoryFactProvenance, context MemoryContext) error {
	hypothesesPath := filepath.Join(memoryDir, "hypotheses.md")
	if err := os.WriteFile(hypothesesPath, []byte(renderHypothesesMD(hypotheses)), 0o644); err != nil {
		return fmt.Errorf("write hypotheses.md: %w", err)
	}
	planSummaryPath := filepath.Join(memoryDir, "plan_summary.md")
	if err := os.WriteFile(planSummaryPath, []byte(renderPlanSummaryMD(context)), 0o644); err != nil {
		return fmt.Errorf("write plan_summary.md: %w", err)
	}
	knownFactsPath := filepath.Join(memoryDir, "known_facts.md")
	if err := os.WriteFile(knownFactsPath, []byte(renderBulletsMD("# Known Facts\n\n", knownFacts)), 0o644); err != nil {
		return fmt.Errorf("write known_facts.md: %w", err)
	}
	openQuestionsPath := filepath.Join(memoryDir, "open_questions.md")
	if err := os.WriteFile(openQuestionsPath, []byte(renderBulletsMD("# Open Questions\n\n", openQuestions)), 0o644); err != nil {
		return fmt.Errorf("write open_questions.md: %w", err)
	}
	provenanceEnvelope := memoryProvenanceEnvelope{
		RunID:        context.RunID,
		UpdatedAt:    context.UpdatedAt,
		Writer:       orchestratorWorkerID,
		Policy:       memoryWriterPolicyRef,
		FindingCount: context.FindingCount,
		Records:      append([]MemoryFactProvenance{}, provenance...),
	}
	if err := WriteJSONAtomic(filepath.Join(memoryDir, memoryProvenanceFile), provenanceEnvelope); err != nil {
		return fmt.Errorf("write %s: %w", memoryProvenanceFile, err)
	}
	if err := WriteJSONAtomic(filepath.Join(memoryDir, "context.json"), context); err != nil {
		return fmt.Errorf("write context.json: %w", err)
	}
	contract, err := buildSharedMemoryContract(context.RunID, context.UpdatedAt, memoryDir)
	if err != nil {
		return err
	}
	if err := WriteJSONAtomic(filepath.Join(memoryDir, memoryContractFile), contract); err != nil {
		return fmt.Errorf("write %s: %w", memoryContractFile, err)
	}
	return nil
}

func memoryKnownFacts(plan RunPlan, findings []Finding) []string {
	facts, _ := memoryKnownFactsWithProvenance(plan, findings)
	return facts
}

func memoryKnownFactsWithProvenance(plan RunPlan, findings []Finding) ([]string, []MemoryFactProvenance) {
	facts := []string{
		fmt.Sprintf("Goal: %s", plan.Metadata.NormalizedGoal),
		fmt.Sprintf("Planner decision: %s", plan.Metadata.PlannerDecision),
	}
	provenance := make([]MemoryFactProvenance, 0, len(findings))
	verifiedCount := 0
	for _, finding := range findings {
		state := normalizeFindingState(finding.State)
		if state == "" {
			state = normalizeFindingState(finding.Metadata["finding_state"])
		}
		if state == "" {
			state = FindingStateCandidate
		}
		promoted := state == FindingStateVerified
		knownFact := ""
		if promoted {
			verifiedCount++
			knownFact = strings.TrimSpace(fmt.Sprintf("%s | %s | %s | confidence=%s", finding.TaskID, strings.TrimSpace(finding.Target), strings.TrimSpace(finding.Title), strings.TrimSpace(finding.Confidence)))
			facts = append(facts, knownFact)
		}
		if state == FindingStateVerified || state == FindingStateRejected {
			provenance = append(provenance, buildMemoryFactProvenance(finding, state, promoted, knownFact))
		}
	}
	if verifiedCount == 0 {
		facts = append(facts, "No verified findings yet.")
	}
	sort.SliceStable(provenance, func(i, j int) bool {
		if provenance[i].PromotedToKnownFacts != provenance[j].PromotedToKnownFacts {
			return provenance[i].PromotedToKnownFacts
		}
		if provenance[i].CurrentState != provenance[j].CurrentState {
			return provenance[i].CurrentState < provenance[j].CurrentState
		}
		return provenance[i].Title < provenance[j].Title
	})
	return dedupeStrings(facts), provenance
}

func memoryOpenQuestions(hypotheses []Hypothesis) []string {
	questions := make([]string, 0, len(hypotheses))
	for _, hypothesis := range hypotheses {
		statement := strings.TrimSpace(hypothesis.Statement)
		if statement == "" {
			continue
		}
		questions = append(questions, fmt.Sprintf("[%s] What evidence confirms or refutes: %s", hypothesis.ID, statement))
	}
	if len(questions) == 0 {
		questions = append(questions, "No open questions recorded.")
	}
	return dedupeStrings(questions)
}

func compactKnownFactsEntries(values []string, max int) []string {
	if max <= 0 || len(values) == 0 {
		return nil
	}
	anchors := make([]string, 0, len(values))
	dynamic := make([]string, 0, len(values))
	for _, value := range values {
		if isKnownFactAnchor(value) {
			anchors = append(anchors, value)
			continue
		}
		dynamic = append(dynamic, value)
	}
	if len(anchors) > max {
		anchors = anchors[:max]
	}
	dynamicCap := max - len(anchors)
	if dynamicCap <= 0 {
		return append([]string{}, anchors...)
	}
	dynamic = compactTailEntries(dynamic, dynamicCap)
	out := append([]string{}, anchors...)
	out = append(out, dynamic...)
	return out
}

func compactTailEntries(values []string, max int) []string {
	if max <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= max {
		return append([]string{}, values...)
	}
	return append([]string{}, values[len(values)-max:]...)
}

func buildMemoryFactProvenance(finding Finding, state string, promoted bool, knownFact string) MemoryFactProvenance {
	record := MemoryFactProvenance{
		DedupeKey:            strings.TrimSpace(memoryFirstNonEmpty(finding.Metadata["dedupe_key"], FindingDedupeKey(finding))),
		TaskID:               strings.TrimSpace(finding.TaskID),
		Target:               strings.TrimSpace(finding.Target),
		FindingType:          strings.TrimSpace(finding.FindingType),
		Title:                strings.TrimSpace(finding.Title),
		CurrentState:         state,
		PromotedToKnownFacts: promoted,
		KnownFact:            strings.TrimSpace(knownFact),
		SourceEventIDs:       uniqueSortedStrings(sourceEventIDs(finding.Sources)),
		SourceTaskIDs:        uniqueSortedStrings(sourceTaskIDs(finding.Sources)),
		SourceWorkerIDs:      uniqueSortedStrings(sourceWorkerIDs(finding.Sources)),
		StateTransitions:     buildMemoryStateTransitions(finding),
	}
	if len(record.StateTransitions) == 0 && len(finding.Sources) > 0 {
		first := finding.Sources[0]
		record.StateTransitions = []MemoryStateTransition{
			{
				To:            state,
				Resolution:    "single_source",
				SourceEventID: strings.TrimSpace(first.EventID),
				SourceTaskID:  strings.TrimSpace(first.TaskID),
				SourceWorker:  strings.TrimSpace(first.WorkerID),
			},
		}
	}
	return record
}

func buildMemoryStateTransitions(finding Finding) []MemoryStateTransition {
	sourceByEvent := map[string]FindingSource{}
	for _, source := range finding.Sources {
		eventID := strings.TrimSpace(source.EventID)
		if eventID == "" {
			continue
		}
		sourceByEvent[eventID] = source
	}
	out := make([]MemoryStateTransition, 0, len(finding.Conflicts))
	for _, conflict := range finding.Conflicts {
		if strings.TrimSpace(conflict.Field) != "state" {
			continue
		}
		eventID := strings.TrimSpace(conflict.IncomingEvent)
		source := sourceByEvent[eventID]
		out = append(out, MemoryStateTransition{
			From:          normalizeFindingState(conflict.ExistingValue),
			To:            normalizeFindingState(conflict.IncomingValue),
			Resolution:    strings.TrimSpace(conflict.Resolution),
			SourceEventID: eventID,
			SourceTaskID:  strings.TrimSpace(source.TaskID),
			SourceWorker:  strings.TrimSpace(source.WorkerID),
		})
	}
	return out
}

func sourceEventIDs(sources []FindingSource) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.EventID)
	}
	return out
}

func sourceTaskIDs(sources []FindingSource) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.TaskID)
	}
	return out
}

func sourceWorkerIDs(sources []FindingSource) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.WorkerID)
	}
	return out
}

func renderHypothesesMD(hypotheses []Hypothesis) string {
	var b strings.Builder
	b.WriteString("# Hypotheses\n\n")
	if len(hypotheses) == 0 {
		b.WriteString("- None generated.\n")
		return b.String()
	}
	for _, hypothesis := range hypotheses {
		b.WriteString(fmt.Sprintf("- **%s** (%s/%s, score=%d): %s\n", hypothesis.ID, hypothesis.Impact, hypothesis.Confidence, hypothesis.Score, strings.TrimSpace(hypothesis.Statement)))
		if len(hypothesis.EvidenceRequired) > 0 {
			b.WriteString(fmt.Sprintf("  - Evidence: %s\n", strings.Join(hypothesis.EvidenceRequired, ", ")))
		}
	}
	return b.String()
}

func renderPlanSummaryMD(ctx MemoryContext) string {
	var b strings.Builder
	b.WriteString("# Plan Summary\n\n")
	b.WriteString(fmt.Sprintf("- Run: `%s`\n", ctx.RunID))
	b.WriteString(fmt.Sprintf("- Goal: %s\n", ctx.NormalizedGoal))
	b.WriteString(fmt.Sprintf("- Planner: %s (%s)\n", ctx.PlannerVersion, ctx.PlannerMode))
	if strings.TrimSpace(ctx.PlannerModel) != "" {
		b.WriteString(fmt.Sprintf("- Planner model: %s\n", ctx.PlannerModel))
	}
	if len(ctx.PlannerPlaybooks) > 0 {
		b.WriteString(fmt.Sprintf("- Planner playbooks: %s\n", strings.Join(ctx.PlannerPlaybooks, ", ")))
	}
	b.WriteString(fmt.Sprintf("- Prompt hash: `%s`\n", ctx.PlannerPromptHash))
	b.WriteString(fmt.Sprintf("- Decision: %s\n", ctx.PlannerDecision))
	if strings.TrimSpace(ctx.PlannerRationale) != "" {
		b.WriteString(fmt.Sprintf("- Rationale: %s\n", ctx.PlannerRationale))
	}
	b.WriteString(fmt.Sprintf("- Regenerations: %d\n", ctx.RegenerationCount))
	b.WriteString(fmt.Sprintf("- Hypotheses: %d\n", ctx.HypothesisCount))
	b.WriteString(fmt.Sprintf("- Artifacts: %d\n", ctx.ArtifactCount))
	b.WriteString(fmt.Sprintf("- Findings: %d\n", ctx.FindingCount))
	if ctx.KnownFactsRetained > 0 || ctx.OpenQuestionsRetained > 0 {
		b.WriteString(fmt.Sprintf("- Known facts (retained/total): %d/%d\n", maxInt(0, ctx.KnownFactsRetained), maxInt(0, ctx.KnownFactsCount)))
		b.WriteString(fmt.Sprintf("- Open questions (retained/total): %d/%d\n", maxInt(0, ctx.OpenQuestionsRetained), maxInt(0, ctx.OpenQuestionsCount)))
	}
	if ctx.KnownFactsDropped > 0 || ctx.OpenQuestionsDropped > 0 {
		b.WriteString(fmt.Sprintf("- Compaction dropped: known_facts=%d, open_questions=%d\n", maxInt(0, ctx.KnownFactsDropped), maxInt(0, ctx.OpenQuestionsDropped)))
	}
	if strings.TrimSpace(ctx.SharedMemoryWriter) != "" {
		b.WriteString(fmt.Sprintf("- Shared memory writer: %s\n", strings.TrimSpace(ctx.SharedMemoryWriter)))
	}
	if strings.TrimSpace(ctx.SharedMemoryPolicy) != "" {
		b.WriteString(fmt.Sprintf("- Shared memory policy: %s\n", strings.TrimSpace(ctx.SharedMemoryPolicy)))
	}
	if ctx.KnownFactsProvenance > 0 {
		b.WriteString(fmt.Sprintf("- Known fact provenance records: %d\n", maxInt(0, ctx.KnownFactsProvenance)))
	}
	if ctx.SharedMemoryViolations > 0 {
		b.WriteString(fmt.Sprintf("- Shared memory violations observed: %d\n", maxInt(0, ctx.SharedMemoryViolations)))
		if strings.TrimSpace(ctx.SharedMemoryViolationDetail) != "" {
			b.WriteString(fmt.Sprintf("- Shared memory violation detail: %s\n", strings.TrimSpace(ctx.SharedMemoryViolationDetail)))
		}
	}
	return b.String()
}

func renderBulletsMD(header string, values []string) string {
	var b strings.Builder
	b.WriteString(header)
	if len(values) == 0 {
		b.WriteString("- None.\n")
		return b.String()
	}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		b.WriteString("- " + trimmed + "\n")
	}
	return b.String()
}

func dedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func uniqueSortedStrings(values []string) []string {
	trimmed := dedupeStrings(values)
	sort.Strings(trimmed)
	return trimmed
}

func memoryFirstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func detectSharedMemoryViolation(memoryDir string) (*sharedMemoryViolation, error) {
	contract, ok, err := readSharedMemoryContract(filepath.Join(memoryDir, memoryContractFile))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	if len(contract.ManagedFiles) == 0 {
		return &sharedMemoryViolation{
			Reason: "managed_files_empty",
		}, nil
	}
	mismatched := make([]string, 0)
	for name, expectedHash := range contract.ManagedFiles {
		path := filepath.Join(memoryDir, name)
		actualHash, err := sha256FileHex(path)
		if err != nil {
			mismatched = append(mismatched, fmt.Sprintf("%s:missing_or_unreadable", name))
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(actualHash), strings.TrimSpace(expectedHash)) {
			mismatched = append(mismatched, fmt.Sprintf("%s:hash_mismatch", name))
		}
	}
	if len(mismatched) == 0 {
		return nil, nil
	}
	sort.Strings(mismatched)
	return &sharedMemoryViolation{
		Reason: "managed_file_drift",
		Files:  mismatched,
	}, nil
}

func buildSharedMemoryContract(runID string, now time.Time, memoryDir string) (sharedMemoryContract, error) {
	managed := map[string]string{}
	for _, fileName := range memoryManagedFiles() {
		path := filepath.Join(memoryDir, fileName)
		hash, err := sha256FileHex(path)
		if err != nil {
			return sharedMemoryContract{}, fmt.Errorf("hash managed memory file %s: %w", fileName, err)
		}
		managed[fileName] = hash
	}
	return sharedMemoryContract{
		RunID:        strings.TrimSpace(runID),
		UpdatedAt:    now,
		Writer:       orchestratorWorkerID,
		Policy:       memoryWriterPolicyRef,
		ManagedFiles: managed,
	}, nil
}

func readSharedMemoryContract(path string) (sharedMemoryContract, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sharedMemoryContract{}, false, nil
		}
		return sharedMemoryContract{}, false, fmt.Errorf("read shared memory contract: %w", err)
	}
	var contract sharedMemoryContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return sharedMemoryContract{}, false, fmt.Errorf("parse shared memory contract: %w", err)
	}
	return contract, true, nil
}

func memoryManagedFiles() []string {
	return []string{
		"hypotheses.md",
		"plan_summary.md",
		"known_facts.md",
		"open_questions.md",
		"context.json",
		memoryProvenanceFile,
	}
}

func sha256FileHex(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func readMemoryContext(path string) (MemoryContext, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MemoryContext{}, err
	}
	var ctx MemoryContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return MemoryContext{}, err
	}
	return ctx, nil
}
