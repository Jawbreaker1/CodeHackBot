// Package assessment coordinates bounded work through the shared worker engine.
package assessment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

type Limits struct {
	Workers      int `json:"workers"`
	Rounds       int `json:"rounds"`
	Tasks        int `json:"tasks"`
	StepsPerTask int `json:"steps_per_task"`
	ModelCalls   int `json:"model_calls"`
}

func DefaultLimits() Limits {
	return Limits{Workers: 2, Rounds: 4, Tasks: 8, StepsPerTask: 6, ModelCalls: 96}
}

type Task struct {
	ID        string   `json:"id"`
	Goal      string   `json:"goal"`
	DoneWhen  string   `json:"done_when"`
	DependsOn []string `json:"depends_on"`
}

// Event is an observational transition for UI adapters. It carries enough
// task metadata for a client to render a useful run view without reading
// worker internals or guessing from prose.
type Event struct {
	TaskID          string   `json:"task_id,omitempty"`
	Kind            string   `json:"kind"`
	Message         string   `json:"message,omitempty"`
	Goal            string   `json:"goal,omitempty"`
	DoneWhen        string   `json:"done_when,omitempty"`
	DependsOn       []string `json:"depends_on,omitempty"`
	Step            int      `json:"step,omitempty"`
	ActiveStep      string   `json:"active_step,omitempty"`
	Action          string   `json:"action,omitempty"`
	ExitStatus      string   `json:"exit_status,omitempty"`
	EvidenceCount   int      `json:"evidence_count,omitempty"`
	RemainingBudget string   `json:"remaining_budget,omitempty"`
	ContextUsage    string   `json:"context_usage,omitempty"`
}

type Result struct {
	Task     Task                        `json:"task"`
	Status   string                      `json:"status"`
	Summary  string                      `json:"summary"`
	Error    string                      `json:"error,omitempty"`
	Evidence []ctxpacket.ExecutionResult `json:"evidence"`
}

// Findings are model-authored drafts, never independent verification claims.
type Finding struct {
	Title          string   `json:"title"`
	Status         string   `json:"status"` // candidate or reproduced
	ValidationTask string   `json:"validation_task,omitempty"`
	Impact         string   `json:"impact"`
	Steps          []string `json:"steps"`
	Evidence       []string `json:"evidence"`
	Remediation    []string `json:"remediation"`
}

type Decision struct {
	Summary  string    `json:"summary"`
	Tasks    []Task    `json:"tasks"`
	Complete bool      `json:"complete"`
	Findings []Finding `json:"findings"`
	Gaps     []string  `json:"gaps"`
}

type State struct {
	Version         int        `json:"version"`
	ID              string     `json:"id"`
	Goal            string     `json:"goal"`
	Scope           string     `json:"scope"`
	Model           string     `json:"model"`
	ReasoningEffort string     `json:"reasoning_effort,omitempty"`
	MaxOutputTokens int        `json:"max_output_tokens,omitempty"`
	MaxInputBytes   int        `json:"max_input_bytes,omitempty"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      time.Time  `json:"finished_at,omitempty"`
	Limits          Limits     `json:"limits"`
	Plans           []Decision `json:"plans"`
	Results         []Result   `json:"results"`
	Usage           Usage      `json:"usage"`
	Error           string     `json:"error,omitempty"`
}

func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".assessment-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func parseDecision(raw string) (Decision, error) {
	text := strings.TrimSpace(raw)
	if i := strings.LastIndex(text, "</think>"); i >= 0 {
		text = strings.TrimSpace(text[i+8:])
	}
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```"), "```"))
	var d Decision
	err := json.Unmarshal([]byte(text), &d)
	return d, err
}

func validateDecision(d Decision, state State) error {
	if strings.TrimSpace(d.Summary) == "" {
		return fmt.Errorf("coordinator summary is missing")
	}
	if d.Complete {
		if len(d.Tasks) != 0 || len(state.Results) == 0 {
			return fmt.Errorf("completion needs prior results and no new tasks")
		}
	} else if len(d.Tasks) == 0 || len(d.Tasks) > state.Limits.Workers || len(state.Results)+len(d.Tasks) > state.Limits.Tasks {
		return fmt.Errorf("coordinator task batch exceeds the assessment budget or is empty")
	}
	known := map[string]Result{}
	for _, r := range state.Results {
		known[r.Task.ID] = r
	}
	seen := map[string]bool{}
	for _, task := range d.Tasks {
		if !validID(task.ID) || seen[task.ID] || known[task.ID].Task.ID != "" || strings.TrimSpace(task.Goal) == "" || strings.TrimSpace(task.DoneWhen) == "" {
			return fmt.Errorf("invalid or reused task identity, goal, or done condition")
		}
		seen[task.ID] = true
		for _, id := range task.DependsOn {
			if known[id].Status != "done" {
				return fmt.Errorf("task %s depends on unfinished task %s", task.ID, id)
			}
		}
	}
	refs := map[string]bool{}
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range append(append([]string{}, evidence.LogRefs...), evidence.ArtifactRefs...) {
				refs[ref] = true
			}
		}
	}
	for _, f := range d.Findings {
		if f.Title == "" || f.Impact == "" || len(f.Steps) == 0 || len(f.Remediation) == 0 || len(f.Evidence) == 0 {
			return fmt.Errorf("finding lacks reproducible reporting fields")
		}
		if f.Status != "candidate" && f.Status != "reproduced" {
			return fmt.Errorf("unknown finding status %q", f.Status)
		}
		for _, ref := range f.Evidence {
			if !refs[ref] {
				return fmt.Errorf("finding references unrecorded evidence: %s", ref)
			}
		}
		if f.Status == "reproduced" {
			r := known[f.ValidationTask]
			if r.Status != "done" || len(r.Task.DependsOn) == 0 || len(r.Evidence) == 0 {
				return fmt.Errorf("reproduced finding needs a completed dependent validation task")
			}
			ownEvidence := false
			for _, e := range r.Evidence {
				for _, ref := range append(append([]string{}, e.LogRefs...), e.ArtifactRefs...) {
					for _, cited := range f.Evidence {
						if ref == cited {
							ownEvidence = true
						}
					}
				}
			}
			if !ownEvidence {
				return fmt.Errorf("reproduced finding must cite its validation task")
			}
		}
	}
	return nil
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 48 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
