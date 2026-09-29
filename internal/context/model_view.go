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
	// Rebuild the working set on every request. Retain all bounded result cards
	// while the selected model's request budget has room; age alone is not a
	// reason to discard evidence. The authoritative packet and logs remain full.
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
	size := func() int { return len(v.Render()) }
	if size() <= maxBytes {
		return v, nil
	}
	v.ContextNotes = []string{"This request reached its context budget. Older details were shortened or offloaded; full records remain in session state and referenced logs. Use recall_context to search and read prior output, conversation, or plan revisions."}
	// Only a request that exceeds its allowance starts compaction. Shorten older
	// results first, preserving their observations and references where possible.
	for i := len(v.RelevantRecentResults) - 1; i >= 0 && size() > maxBytes; i-- {
		if pinnedResult(v.PinnedResultRefs, v.RelevantRecentResults[i]) {
			continue
		}
		v.RelevantRecentResults[i].Action = excerpt(v.RelevantRecentResults[i].Action, 512)
		v.RelevantRecentResults[i].ActualExec = excerpt(v.RelevantRecentResults[i].ActualExec, 512)
		v.RelevantRecentResults[i].OutputEvidence = excerpt(v.RelevantRecentResults[i].OutputEvidence, 2048)
		v.RelevantRecentResults[i].OutputSummary = excerpt(v.RelevantRecentResults[i].OutputSummary, 768)
		if len(v.RelevantRecentResults[i].ArtifactRefs) > 2 {
			v.RelevantRecentResults[i].ArtifactRefs = v.RelevantRecentResults[i].ArtifactRefs[:2]
		}
	}
	for i := 0; i < len(v.PlanHistory)-1 && size() > maxBytes; i++ {
		revision := &v.PlanHistory[i]
		revision.Plan.Summary = excerpt(revision.Plan.Summary, 256)
		revision.Plan.Steps = nil
		revision.Plan.StepPurposes = nil
		revision.Plan.ReplanConditions = nil
	}
	for i := len(v.RelevantRecentResults) - 1; i >= 0 && size() > maxBytes; i-- {
		if pinnedResult(v.PinnedResultRefs, v.RelevantRecentResults[i]) {
			continue
		}
		v.RelevantRecentResults[i].OutputEvidence = "(omitted; consult log_refs)"
		v.RelevantRecentResults[i].OutputSummary = excerpt(v.RelevantRecentResults[i].OutputSummary, 256)
	}
	// The most recent operator direction remains authoritative even when an
	// assistant response follows it in the transcript.
	protectedTurn := -1
	for i := len(v.RecentConversation) - 1; i >= 0; i-- {
		if strings.HasPrefix(v.RecentConversation[i], "User: ") || strings.HasPrefix(v.RecentConversation[i], "Operator answer: ") {
			protectedTurn = i
			break
		}
	}
	for len(v.RecentConversation) > 1 && size() > maxBytes {
		remove := -1
		for i := range v.RecentConversation {
			if i != protectedTurn {
				remove = i
				break
			}
		}
		if remove < 0 {
			break
		}
		v.RecentConversation = append(v.RecentConversation[:remove], v.RecentConversation[remove+1:]...)
		if protectedTurn > remove {
			protectedTurn--
		}
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
		v.LatestExecutionResult.Action = excerpt(v.LatestExecutionResult.Action, 1024)
		v.LatestExecutionResult.ActualExec = excerpt(v.LatestExecutionResult.ActualExec, 2048)
		v.LatestExecutionResult.OutputEvidence = excerpt(v.LatestExecutionResult.OutputEvidence, 4096)
		v.LatestExecutionResult.OutputSummary = excerpt(v.LatestExecutionResult.OutputSummary, 512)
	}
	// Historical plan revisions have a fixed-size floor after field pruning.
	// Keep the latest revision and count earlier ones omitted from this view.
	for size() > maxBytes && len(v.PlanHistory) > 1 {
		v.PlanHistory = v.PlanHistory[1:]
		v.OffloadedPlanCount++
	}
	// Pinned results earn space before unpinned cards; no pin overrides the
	// hard limit. Full results remain in the authoritative packet and logs.
	for size() > maxBytes && pruneOldestResult(&v, false) {
	}
	if size() > maxBytes {
		v.ContextRecall.Content = excerpt(v.ContextRecall.Content, 2048)
	}
	for i := len(v.RelevantRecentResults) - 1; i >= 0 && size() > maxBytes; i-- {
		if !pinnedResult(v.PinnedResultRefs, v.RelevantRecentResults[i]) {
			continue
		}
		v.RelevantRecentResults[i].Action = excerpt(v.RelevantRecentResults[i].Action, 512)
		v.RelevantRecentResults[i].ActualExec = excerpt(v.RelevantRecentResults[i].ActualExec, 512)
		v.RelevantRecentResults[i].OutputEvidence = excerpt(v.RelevantRecentResults[i].OutputEvidence, 2048)
		v.RelevantRecentResults[i].OutputSummary = excerpt(v.RelevantRecentResults[i].OutputSummary, 768)
	}
	for i := len(v.RelevantRecentResults) - 1; i >= 0 && size() > maxBytes; i-- {
		if pinnedResult(v.PinnedResultRefs, v.RelevantRecentResults[i]) {
			v.RelevantRecentResults[i].OutputEvidence = "(omitted; consult log_refs)"
			v.RelevantRecentResults[i].OutputSummary = excerpt(v.RelevantRecentResults[i].OutputSummary, 256)
		}
	}
	for size() > maxBytes && pruneOldestResult(&v, true) {
	}
	if size() > maxBytes {
		return WorkerPacket{}, fmt.Errorf("worker context needs %d bytes after compaction; allowance is %d; shorten the task or supporting material", size(), maxBytes)
	}
	return v, nil
}

func pruneOldestResult(view *WorkerPacket, includePinned bool) bool {
	for i := len(view.RelevantRecentResults) - 1; i >= 0; i-- {
		if !includePinned && pinnedResult(view.PinnedResultRefs, view.RelevantRecentResults[i]) {
			continue
		}
		view.RelevantRecentResults = append(view.RelevantRecentResults[:i], view.RelevantRecentResults[i+1:]...)
		view.OffloadedResultCount++
		return true
	}
	return false
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
