package assessment

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Lead is a model-authored, evidence-linked question for later planning. It is
// deliberately not a finding: the cited observations may support several
// explanations, and the proposed weakness has not been established.
type Lead struct {
	ID         string   `json:"id"`
	Hypothesis string   `json:"hypothesis"`
	Evidence   []string `json:"evidence"`
	NextCheck  string   `json:"next_check"`
}

// Closing a lead is an explicit coordinator decision, retained in plan history.
// Untested or out-of-scope questions stay open and appear in report gaps.
type LeadClosure struct {
	ID       string   `json:"id"`
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence"`
}

func CurrentOpenLeads(plans []Decision) []Lead {
	if len(plans) == 0 {
		return nil
	}
	return append([]Lead(nil), plans[len(plans)-1].OpenLeads...)
}

// A correction request must retain the rejected proposal's lead section.
// It often comes after long task and finding descriptions, beyond the short
// response excerpt sent back to the model for repair.
func leadCorrectionContext(state State, rejected Decision) string {
	if len(state.Plans) == 0 && len(rejected.OpenLeads) == 0 && len(rejected.ClosedLeads) == 0 {
		return ""
	}
	priorIDs := make([]string, 0, len(CurrentOpenLeads(state.Plans)))
	for _, lead := range CurrentOpenLeads(state.Plans) {
		priorIDs = append(priorIDs, lead.ID)
	}
	proposed, _ := json.Marshal(struct {
		Open   []Lead        `json:"rejected_open_leads"`
		Closed []LeadClosure `json:"rejected_closed_leads"`
	}{rejected.OpenLeads, rejected.ClosedLeads})
	return "\nLead repair: previous accepted open-lead IDs are " + strings.Join(priorIDs, ", ") +
		". Only these IDs may appear in closed_leads; a new question belongs in open_leads. Keep each prior lead open unless recorded evidence supports its closure. Preserve supported new leads from the rejected proposal while fixing validation errors. Each hypothesis is at most 400 characters and each next_check at most 300 characters. Rejected lead proposals (not accepted evidence): " + promptExcerptEnds(string(proposed), 14000)
}

func validateLeadContinuity(d Decision, state State, refs map[string]bool) error {
	if len(d.OpenLeads) > 8 {
		return fmt.Errorf("coordinator has more than eight open leads; keep only material questions")
	}
	previous := map[string]bool{}
	for _, lead := range CurrentOpenLeads(state.Plans) {
		previous[lead.ID] = true
	}
	open := map[string]bool{}
	for _, lead := range d.OpenLeads {
		if !validID(lead.ID) || open[lead.ID] || strings.TrimSpace(lead.Hypothesis) == "" || strings.TrimSpace(lead.NextCheck) == "" || len(lead.Evidence) == 0 || len(lead.Evidence) > 4 {
			return fmt.Errorf("open lead needs a unique ID, hypothesis, next check, and one to four evidence references")
		}
		if len(lead.Hypothesis) > 400 || len(lead.NextCheck) > 300 {
			return fmt.Errorf("open lead %s is too long for the planning register", lead.ID)
		}
		for _, ref := range lead.Evidence {
			if !refs[ref] {
				return fmt.Errorf("open lead %s cites unrecorded evidence: %s", lead.ID, ref)
			}
		}
		open[lead.ID] = true
	}
	closed := map[string]bool{}
	for _, closure := range d.ClosedLeads {
		if !previous[closure.ID] || open[closure.ID] || closed[closure.ID] || strings.TrimSpace(closure.Reason) == "" || len(closure.Evidence) == 0 || len(closure.Evidence) > 4 {
			return fmt.Errorf("closed lead %s needs a prior open lead, a reason, and recorded evidence", closure.ID)
		}
		for _, ref := range closure.Evidence {
			if !refs[ref] {
				return fmt.Errorf("closed lead %s cites unrecorded evidence: %s", closure.ID, ref)
			}
		}
		closed[closure.ID] = true
	}
	for id := range previous {
		if !open[id] && !closed[id] {
			return fmt.Errorf("previous open lead %s was dropped without a closure", id)
		}
	}
	return nil
}
