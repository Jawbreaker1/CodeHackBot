package workerloop

import (
	"encoding/json"
	"fmt"
	"strings"
)

type PlanUpdate struct {
	Summary          string            `json:"summary"`
	Steps            []string          `json:"steps"`
	StepPurposes     map[string]string `json:"step_purposes,omitempty"`
	ActiveStep       string            `json:"active_step"`
	ReplanConditions []string          `json:"replan_conditions,omitempty"`
}

// A plan may accompany any decision. It never changes the original goal,
// done condition, scope, permissions or remaining budget.
type Response struct {
	Type      string      `json:"type"`
	Command   string      `json:"command,omitempty"`
	Args      []string    `json:"args,omitempty"`
	UseShell  bool        `json:"use_shell,omitempty"`
	Impact    string      `json:"impact,omitempty"`
	Target    string      `json:"target,omitempty"`
	Risk      string      `json:"risk,omitempty"`
	Artifacts []string    `json:"artifacts,omitempty"`
	Summary   string      `json:"summary,omitempty"`
	Question  string      `json:"question,omitempty"`
	Strategy  string      `json:"strategy,omitempty"`
	Plan      *PlanUpdate `json:"plan,omitempty"`
}

func ParseResponse(text string) (Response, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```json"), "```"))
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```"), "```"))
	}
	var r Response
	if !json.Valid([]byte(text)) {
		return r, fmt.Errorf("expected one complete JSON decision")
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return r, fmt.Errorf("parse worker decision: %w", err)
	}
	switch r.Type {
	case "bash":
		if r.Risk != "" && r.Risk != "low" && r.Risk != "dangerous" && r.Risk != "unknown" {
			return r, fmt.Errorf("bash risk must be low, dangerous, or unknown")
		}
		if strings.TrimSpace(r.Command) == "" {
			return r, fmt.Errorf("bash command is required")
		}
		if r.UseShell && len(r.Args) != 0 {
			return r, fmt.Errorf("bash shell mode must omit args")
		}
	case "step_complete", "blocked":
		if strings.TrimSpace(r.Summary) == "" {
			return r, fmt.Errorf("%s summary is required", r.Type)
		}
	case "ask_user":
		if strings.TrimSpace(r.Question) == "" {
			return r, fmt.Errorf("ask_user question is required")
		}
	case "load_strategy":
		if strings.TrimSpace(r.Strategy) == "" {
			return r, fmt.Errorf("load_strategy strategy is required")
		}
	case "update_plan":
		if r.Plan == nil {
			return r, fmt.Errorf("update_plan requires plan")
		}
	default:
		return r, fmt.Errorf("unsupported response type %q", r.Type)
	}
	if r.Type != "bash" && (r.Command != "" || len(r.Args) != 0 || r.UseShell || r.Impact != "" || r.Target != "" || r.Risk != "") {
		return r, fmt.Errorf("only bash may contain execution fields")
	}
	if r.Type != "bash" && len(r.Artifacts) != 0 {
		return r, fmt.Errorf("only bash may declare artifacts")
	}
	if r.Type != "load_strategy" && r.Strategy != "" {
		return r, fmt.Errorf("only load_strategy may name a strategy")
	}
	if len(r.Artifacts) > 8 {
		return r, fmt.Errorf("bash declares %d artifacts; maximum is 8, so keep the most useful references", len(r.Artifacts))
	}
	for _, artifact := range r.Artifacts {
		if strings.TrimSpace(artifact) == "" {
			return r, fmt.Errorf("artifact paths must be nonempty")
		}
	}
	if r.Plan != nil {
		if err := validatePlan(*r.Plan); err != nil {
			return r, err
		}
	}
	return r, nil
}

func validatePlan(p PlanUpdate) error {
	if strings.TrimSpace(p.Summary) == "" || len(p.Steps) == 0 || len(p.Steps) > 6 {
		return fmt.Errorf("plan requires a summary and one to six steps")
	}
	seen, active := map[string]bool{}, false
	for _, step := range p.Steps {
		step = strings.TrimSpace(step)
		if step == "" || seen[step] {
			return fmt.Errorf("plan steps must be nonempty and unique")
		}
		seen[step] = true
		active = active || step == strings.TrimSpace(p.ActiveStep)
	}
	for step, purpose := range p.StepPurposes {
		if !seen[step] || strings.TrimSpace(purpose) == "" {
			return fmt.Errorf("plan step purposes must describe listed steps")
		}
	}
	if !active {
		return fmt.Errorf("plan active_step must match a step")
	}
	if len(p.ReplanConditions) > 6 {
		return fmt.Errorf("plan has too many replan conditions")
	}
	for _, condition := range p.ReplanConditions {
		if strings.TrimSpace(condition) == "" {
			return fmt.Errorf("replan conditions must be nonempty")
		}
	}
	return nil
}
