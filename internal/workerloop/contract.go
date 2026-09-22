package workerloop

import (
	"encoding/json"
	"fmt"
	"strings"
)

type PlanUpdate struct {
	Summary          string   `json:"summary"`
	Steps            []string `json:"steps"`
	ActiveStep       string   `json:"active_step"`
	ReplanConditions []string `json:"replan_conditions,omitempty"`
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
	case "action":
		if r.Risk != "" && r.Risk != "low" && r.Risk != "dangerous" && r.Risk != "unknown" {
			return r, fmt.Errorf("action risk must be low, dangerous, or unknown")
		}
		if strings.TrimSpace(r.Command) == "" {
			return r, fmt.Errorf("action command is required")
		}
		if r.UseShell && len(r.Args) != 0 {
			return r, fmt.Errorf("shell action must omit args")
		}
	case "step_complete", "blocked":
		if strings.TrimSpace(r.Summary) == "" {
			return r, fmt.Errorf("%s summary is required", r.Type)
		}
	case "ask_user":
		if strings.TrimSpace(r.Question) == "" {
			return r, fmt.Errorf("ask_user question is required")
		}
	case "update_plan":
		if r.Plan == nil {
			return r, fmt.Errorf("update_plan requires plan")
		}
	default:
		return r, fmt.Errorf("unsupported response type %q", r.Type)
	}
	if r.Type != "action" && (r.Command != "" || len(r.Args) != 0 || r.UseShell || r.Impact != "" || r.Target != "" || r.Risk != "") {
		return r, fmt.Errorf("only action may contain execution fields")
	}
	if r.Type != "action" && len(r.Artifacts) != 0 {
		return r, fmt.Errorf("only action may declare artifacts")
	}
	if len(r.Artifacts) > 8 {
		return r, fmt.Errorf("action declares too many artifacts")
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
