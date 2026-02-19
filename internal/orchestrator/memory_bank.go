package orchestrator

import (
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
)

type MemoryContext struct {
	UpdatedAt          time.Time `json:"updated_at"`
	RunID              string    `json:"run_id"`
	Goal               string    `json:"goal,omitempty"`
	NormalizedGoal     string    `json:"normalized_goal,omitempty"`
	PlannerVersion     string    `json:"planner_version,omitempty"`
	PlannerPromptHash  string    `json:"planner_prompt_hash,omitempty"`
	PlannerDecision    string    `json:"planner_decision,omitempty"`
	PlannerRationale   string    `json:"planner_rationale,omitempty"`
	RegenerationCount  int       `json:"regeneration_count,omitempty"`
	HypothesisCount    int       `json:"hypothesis_count"`
	ArtifactCount      int       `json:"artifact_count"`
	FindingCount       int       `json:"finding_count"`
	KnownFactsCount    int       `json:"known_facts_count"`
	OpenQuestionsCount int       `json:"open_questions_count"`
	Compacted          bool      `json:"compacted"`
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
		UpdatedAt:          m.Now(),
		RunID:              runID,
		Goal:               plan.Metadata.Goal,
		NormalizedGoal:     plan.Metadata.NormalizedGoal,
		PlannerVersion:     plan.Metadata.PlannerVersion,
		PlannerPromptHash:  plan.Metadata.PlannerPromptHash,
		PlannerDecision:    plan.Metadata.PlannerDecision,
		PlannerRationale:   plan.Metadata.PlannerRationale,
		RegenerationCount:  plan.Metadata.RegenerationCount,
		HypothesisCount:    len(hypotheses),
		KnownFactsCount:    len(knownFacts),
		OpenQuestionsCount: len(openQuestions),
	}
	if err := m.writeMemoryFiles(paths.MemoryDir, hypotheses, knownFacts, openQuestions, ctx); err != nil {
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
	knownFacts := memoryKnownFacts(plan, findings)
	openQuestions := memoryOpenQuestions(plan.Metadata.Hypotheses)
	artifactCount, findingCount, err := m.CountEvidence(runID)
	if err != nil {
		return err
	}
	compacted := len(knownFacts) > memoryMaxKnownFacts || len(openQuestions) > memoryMaxOpenQuestions
	if len(knownFacts) > memoryMaxKnownFacts {
		knownFacts = knownFacts[:memoryMaxKnownFacts]
	}
	if len(openQuestions) > memoryMaxOpenQuestions {
		openQuestions = openQuestions[:memoryMaxOpenQuestions]
	}
	ctx := MemoryContext{
		UpdatedAt:          m.Now(),
		RunID:              runID,
		Goal:               plan.Metadata.Goal,
		NormalizedGoal:     plan.Metadata.NormalizedGoal,
		PlannerVersion:     plan.Metadata.PlannerVersion,
		PlannerPromptHash:  plan.Metadata.PlannerPromptHash,
		PlannerDecision:    plan.Metadata.PlannerDecision,
		PlannerRationale:   plan.Metadata.PlannerRationale,
		RegenerationCount:  plan.Metadata.RegenerationCount,
		HypothesisCount:    len(plan.Metadata.Hypotheses),
		ArtifactCount:      artifactCount,
		FindingCount:       findingCount,
		KnownFactsCount:    len(knownFacts),
		OpenQuestionsCount: len(openQuestions),
		Compacted:          compacted,
	}
	return m.writeMemoryFiles(paths.MemoryDir, plan.Metadata.Hypotheses, knownFacts, openQuestions, ctx)
}

func (m *Manager) writeMemoryFiles(memoryDir string, hypotheses []Hypothesis, knownFacts, openQuestions []string, context MemoryContext) error {
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
	if err := WriteJSONAtomic(filepath.Join(memoryDir, "context.json"), context); err != nil {
		return fmt.Errorf("write context.json: %w", err)
	}
	return nil
}

func memoryKnownFacts(plan RunPlan, findings []Finding) []string {
	facts := []string{
		fmt.Sprintf("Goal: %s", plan.Metadata.NormalizedGoal),
		fmt.Sprintf("Planner decision: %s", plan.Metadata.PlannerDecision),
	}
	for _, finding := range findings {
		entry := fmt.Sprintf("%s | %s | %s | confidence=%s", finding.TaskID, strings.TrimSpace(finding.Target), strings.TrimSpace(finding.Title), strings.TrimSpace(finding.Confidence))
		facts = append(facts, strings.TrimSpace(entry))
	}
	return dedupeStrings(facts)
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
	b.WriteString(fmt.Sprintf("- Planner: %s\n", ctx.PlannerVersion))
	b.WriteString(fmt.Sprintf("- Prompt hash: `%s`\n", ctx.PlannerPromptHash))
	b.WriteString(fmt.Sprintf("- Decision: %s\n", ctx.PlannerDecision))
	if strings.TrimSpace(ctx.PlannerRationale) != "" {
		b.WriteString(fmt.Sprintf("- Rationale: %s\n", ctx.PlannerRationale))
	}
	b.WriteString(fmt.Sprintf("- Regenerations: %d\n", ctx.RegenerationCount))
	b.WriteString(fmt.Sprintf("- Hypotheses: %d\n", ctx.HypothesisCount))
	b.WriteString(fmt.Sprintf("- Artifacts: %d\n", ctx.ArtifactCount))
	b.WriteString(fmt.Sprintf("- Findings: %d\n", ctx.FindingCount))
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
