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
	Type            string      `json:"type"`
	Command         string      `json:"command,omitempty"`
	Args            []string    `json:"args,omitempty"`
	UseShell        bool        `json:"use_shell,omitempty"`
	Scenario        string      `json:"scenario,omitempty"`
	ArtifactDir     string      `json:"artifact_dir,omitempty"`
	CVE             string      `json:"cve,omitempty"`
	Module          string      `json:"module,omitempty"`
	Impact          string      `json:"impact,omitempty"`
	Target          string      `json:"target,omitempty"`
	Risk            string      `json:"risk,omitempty"`
	Artifacts       []string    `json:"artifacts,omitempty"`
	Summary         string      `json:"summary,omitempty"`
	LeadHints       []string    `json:"lead_hints,omitempty"`
	Question        string      `json:"question,omitempty"`
	Strategy        string      `json:"strategy,omitempty"`
	ContextRef      string      `json:"context_ref,omitempty"`
	ContextQuery    string      `json:"context_query,omitempty"`
	ContextStream   string      `json:"context_stream,omitempty"`
	ContextOffset   int64       `json:"context_offset,omitempty"`
	ContextKeepRefs *[]string   `json:"context_keep_refs,omitempty"`
	Plan            *PlanUpdate `json:"plan,omitempty"`
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
	case "bash", "browser_run":
		if r.Risk != "" && r.Risk != "low" && r.Risk != "dangerous" && r.Risk != "unknown" {
			return r, fmt.Errorf("execution risk must be low, dangerous, or unknown")
		}
		if r.Type == "bash" && strings.TrimSpace(r.Command) == "" {
			return r, fmt.Errorf("bash command is required")
		}
		if r.UseShell && len(r.Args) != 0 {
			return r, fmt.Errorf("bash shell mode must omit args")
		}
		if r.Type == "browser_run" && (strings.TrimSpace(r.Scenario) == "" || strings.TrimSpace(r.ArtifactDir) == "" || strings.TrimSpace(r.Summary) == "" || strings.TrimSpace(r.Target) == "" || strings.TrimSpace(r.Impact) == "") {
			return r, fmt.Errorf("browser_run requires scenario, artifact_dir, summary, target, and impact")
		}
	case "metasploit_lookup":
		if (strings.TrimSpace(r.CVE) == "") == (strings.TrimSpace(r.Module) == "") {
			return r, fmt.Errorf("metasploit_lookup needs exactly one cve or module")
		}
		if err := validateMetasploitLookup(r); err != nil {
			return r, err
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
	case "recall_context":
		if (strings.TrimSpace(r.ContextRef) == "") == (strings.TrimSpace(r.ContextQuery) == "") {
			return r, fmt.Errorf("recall_context needs exactly one context_ref or context_query")
		}
		if r.ContextOffset < 0 || (r.ContextStream != "" && r.ContextStream != "stdout" && r.ContextStream != "stderr") {
			return r, fmt.Errorf("recall_context needs a nonnegative offset and stdout or stderr stream")
		}
		if r.ContextQuery != "" && (r.ContextOffset != 0 || r.ContextStream != "") {
			return r, fmt.Errorf("context_query does not take stream or offset")
		}
	case "update_plan":
		if r.Plan == nil {
			return r, fmt.Errorf("update_plan requires plan")
		}
	default:
		return r, fmt.Errorf("unsupported response type %q", r.Type)
	}
	if r.Type != "bash" && r.Type != "browser_run" && (r.Command != "" || len(r.Args) != 0 || r.UseShell || r.Impact != "" || r.Target != "" || r.Risk != "") {
		return r, fmt.Errorf("only execution actions may contain execution fields")
	}
	if r.Type == "browser_run" && (r.Command != "" || len(r.Args) != 0 || r.UseShell) {
		return r, fmt.Errorf("browser_run uses scenario and artifact_dir, not command or args")
	}
	if r.Type != "browser_run" && (r.Scenario != "" || r.ArtifactDir != "") {
		return r, fmt.Errorf("only browser_run may set scenario or artifact_dir")
	}
	if r.Type != "metasploit_lookup" && (r.CVE != "" || r.Module != "") {
		return r, fmt.Errorf("only metasploit_lookup may set cve or module")
	}
	if r.Type != "bash" && r.Type != "browser_run" && len(r.Artifacts) != 0 {
		return r, fmt.Errorf("only execution actions may declare artifacts")
	}
	if r.Type != "step_complete" && len(r.LeadHints) != 0 {
		return r, fmt.Errorf("only step_complete may contain lead hints")
	}
	if len(r.LeadHints) > 3 {
		return r, fmt.Errorf("step_complete may contain at most three lead hints")
	}
	for _, hint := range r.LeadHints {
		if strings.TrimSpace(hint) == "" || len(hint) > 400 {
			return r, fmt.Errorf("lead hints must be concise nonempty text")
		}
	}
	if r.Type != "load_strategy" && r.Strategy != "" {
		return r, fmt.Errorf("only load_strategy may name a strategy")
	}
	if r.Type != "recall_context" && (r.ContextRef != "" || r.ContextQuery != "" || r.ContextStream != "" || r.ContextOffset != 0) {
		return r, fmt.Errorf("only recall_context may use context retrieval fields")
	}
	if r.ContextKeepRefs != nil {
		if len(*r.ContextKeepRefs) > 2 {
			return r, fmt.Errorf("context_keep_refs may retain at most two prior results")
		}
		seen := map[string]bool{}
		for _, ref := range *r.ContextKeepRefs {
			if strings.TrimSpace(ref) == "" || seen[ref] {
				return r, fmt.Errorf("context_keep_refs must contain distinct nonempty references")
			}
			seen[ref] = true
		}
	}
	if len(r.Artifacts) > 8 {
		return r, fmt.Errorf("action declares %d artifacts; maximum is 8, so keep the most useful references", len(r.Artifacts))
	}
	if r.Type == "browser_run" && len(r.Artifacts) > 5 {
		return r, fmt.Errorf("browser_run declares %d additional artifacts; maximum is 5", len(r.Artifacts))
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
