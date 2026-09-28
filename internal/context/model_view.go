package context

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Clone prevents live UI snapshots and compact model views from sharing mutable
// slices or maps with the worker's authoritative state.
func (p WorkerPacket) Clone() WorkerPacket {
	data, _ := json.Marshal(p)
	var copy WorkerPacket
	_ = json.Unmarshal(data, &copy)
	return copy
}

// ModelView bounds the complete rendered packet without changing persisted
// observations. Goals, policy, scope, plan, current feedback and the latest
// operator message are protected. If they alone exceed the allowance, stop.
// This is a byte bound, not a provider-specific tokenizer or a semantic summary.
func (p WorkerPacket) ModelView(maxBytes int) (WorkerPacket, error) {
	v := p.Clone()
	v.TaskRuntime.CurrentTarget, v.TaskRuntime.MissingFact = "", ""
	v.ContextNotes = nil
	// Rebuild the working set on every request, including when the provider has
	// spare room. Older results stay in the authoritative packet and log files;
	// recall_context can search or retrieve them when a later question needs one.
	const recentResultLimit = 8
	if len(v.RelevantRecentResults) > recentResultLimit {
		all := v.RelevantRecentResults
		selected := append([]ExecutionResult(nil), all[:recentResultLimit]...)
		for _, ref := range v.PinnedResultRefs {
			for _, result := range all[recentResultLimit:] {
				if pinnedResult([]string{ref}, result) {
					selected = append(selected, result)
					break
				}
			}
		}
		v.OffloadedResultCount += len(all) - len(selected)
		v.RelevantRecentResults = selected
	}
	// The authoritative packet retains every copy of the task contract. The
	// model needs one canonical goal, not the same long assignment repeated in
	// the current step, plan and first conversation entry on every turn.
	if v.CurrentStep.Objective == v.SessionFoundation.Goal {
		v.CurrentStep.Objective = "(see session_foundation.goal)"
	}
	if v.PlanState.WorkerGoal == v.SessionFoundation.Goal {
		v.PlanState.WorkerGoal = "(see session_foundation.goal)"
	}
	if len(v.RecentConversation) > 0 && v.RecentConversation[0] == "User: "+v.SessionFoundation.Goal {
		v.RecentConversation = v.RecentConversation[1:]
	}
	for i := range v.PlanHistory {
		if v.PlanHistory[i].Plan.WorkerGoal == v.SessionFoundation.Goal {
			v.PlanHistory[i].Plan.WorkerGoal = "(see session_foundation.goal)"
		}
	}
	// Keep the current plan and newest observations readable. Older full command
	// bodies and output live in the durable session/log files, so do not resend
	// them merely because the provider's hard ceiling has not been reached yet.
	shortened := false
	if v.OffloadedResultCount > 0 {
		shortened = true
	}
	latest := &v.LatestExecutionResult
	if len(latest.Action) > 1024 || len(latest.ActualExec) > 1024 || len(latest.OutputEvidence) > 4096 || len(latest.OutputSummary) > 1024 {
		shortened = true
	}
	latest.Action = excerpt(latest.Action, 1024)
	latest.ActualExec = excerpt(latest.ActualExec, 1024)
	latest.OutputEvidence = excerpt(latest.OutputEvidence, 4096)
	latest.OutputSummary = excerpt(latest.OutputSummary, 1024)
	if len(v.RelevantRecentResults) > 0 {
		r := &v.RelevantRecentResults[0]
		if len(r.Action) > 512 || len(r.ActualExec) > 512 || len(r.OutputEvidence) > 2048 || len(r.OutputSummary) > 768 {
			shortened = true
		}
		r.Action = excerpt(r.Action, 512)
		r.ActualExec = excerpt(r.ActualExec, 512)
		r.OutputEvidence = excerpt(r.OutputEvidence, 2048)
		r.OutputSummary = excerpt(r.OutputSummary, 768)
	}
	for i := 1; i < len(v.RelevantRecentResults); i++ {
		r := &v.RelevantRecentResults[i]
		if pinnedResult(v.PinnedResultRefs, *r) {
			r.Action = excerpt(r.Action, 512)
			r.ActualExec = excerpt(r.ActualExec, 512)
			r.OutputEvidence = excerpt(r.OutputEvidence, 2048)
			r.OutputSummary = excerpt(r.OutputSummary, 768)
			continue
		}
		if len(r.Action) > 160 || len(r.ActualExec) > 160 || len(r.OutputEvidence) > 0 || len(r.OutputSummary) > 256 || len(r.ArtifactRefs) > 2 {
			shortened = true
		}
		r.Action = excerpt(r.Action, 160)
		r.ActualExec = excerpt(r.ActualExec, 160)
		if r.OutputEvidence != "" {
			r.OutputEvidence = "(omitted from model view; consult log_refs)"
		}
		r.OutputSummary = excerpt(r.OutputSummary, 256)
		if len(r.ArtifactRefs) > 2 {
			r.ArtifactRefs = r.ArtifactRefs[:2]
		}
	}
	for i := 0; i < len(v.PlanHistory)-1; i++ {
		revision := &v.PlanHistory[i]
		if len(revision.Plan.Steps) > 0 || len(revision.Plan.ReplanConditions) > 0 {
			shortened = true
		}
		revision.Plan.Summary = excerpt(revision.Plan.Summary, 256)
		revision.Plan.Steps = nil
		revision.Plan.StepPurposes = nil
		revision.Plan.ReplanConditions = nil
	}
	if shortened {
		v.ContextNotes = []string{"Older results or oversized execution bodies and plan details were omitted from this model view. Complete records remain in the local session and command logs; use recall_context for an exact prior result when needed."}
	}
	v.ContextRecall.Content = excerpt(v.ContextRecall.Content, 8192)
	size := func() int { return len(v.Render()) }
	if size() <= maxBytes {
		return v, nil
	}
	v.ContextNotes = []string{"Context shortened to fit the request. Full observations remain in session state and referenced logs. Excerpts are not complete evidence or instructions."}
	// Keep every execution identity and reference; remove older output bodies
	// first. Command bodies can be large shell scripts too, so retain a bounded
	// executable excerpt alongside the log references. Repeated commands remain
	// separate observations, even with equal exits.
	for i := len(v.RelevantRecentResults) - 1; i >= 0 && size() > maxBytes; i-- {
		v.RelevantRecentResults[i].Action = excerpt(v.RelevantRecentResults[i].Action, 512)
		v.RelevantRecentResults[i].ActualExec = excerpt(v.RelevantRecentResults[i].ActualExec, 512)
		v.RelevantRecentResults[i].OutputEvidence = "(omitted; consult log_refs)"
		v.RelevantRecentResults[i].OutputSummary = excerpt(v.RelevantRecentResults[i].OutputSummary, 256)
	}
	for len(v.RecentConversation) > 1 && size() > maxBytes {
		v.RecentConversation = v.RecentConversation[1:]
	}
	if size() > maxBytes {
		v.OlderConversationSummary = "(omitted for context budget; retained in session state)"
	}
	// Retrievals are supporting material, never higher-priority instructions.
	for i := range v.MemoryBankRetrievals {
		if size() <= maxBytes {
			break
		}
		v.MemoryBankRetrievals[i] = excerpt(v.MemoryBankRetrievals[i], 1024)
	}
	if size() > maxBytes {
		v.ContextRecall.Content = excerpt(v.ContextRecall.Content, 2048)
	}
	if size() > maxBytes {
		v.LatestExecutionResult.Action = excerpt(v.LatestExecutionResult.Action, 1024)
		v.LatestExecutionResult.ActualExec = excerpt(v.LatestExecutionResult.ActualExec, 2048)
		v.LatestExecutionResult.OutputEvidence = excerpt(v.LatestExecutionResult.OutputEvidence, 4096)
		v.LatestExecutionResult.OutputSummary = excerpt(v.LatestExecutionResult.OutputSummary, 512)
	}
	if size() > maxBytes {
		return WorkerPacket{}, fmt.Errorf("worker context needs %d bytes after compaction; allowance is %d; shorten the task or supporting material", size(), maxBytes)
	}
	return v, nil
}

func pinnedResult(refs []string, result ExecutionResult) bool {
	for _, ref := range refs {
		for _, saved := range result.LogRefs {
			if saved == ref {
				return true
			}
		}
	}
	return false
}

func excerpt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return strings.TrimSpace(s[:max]) + "\n[excerpt truncated; consult the original record]"
}
