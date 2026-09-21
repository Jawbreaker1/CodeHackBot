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

func excerpt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return strings.TrimSpace(s[:max]) + "\n[excerpt truncated; consult the original record]"
}
