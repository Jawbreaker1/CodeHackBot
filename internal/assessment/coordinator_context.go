package assessment

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// coordinatorPromptBounded builds one planning view from the durable assessment.
// Recent outcomes and evidence used by findings stay in view. Older task logs
// remain in the session instead of being resent on every planning round.
func coordinatorPromptBounded(state State, maxBytes int) (string, error) {
	return coordinatorPromptMeasured(state, maxBytes, func(prompt string) int { return len(prompt) })
}

// coordinatorPromptMeasured applies the same protected-field and offload
// order with a caller-supplied request measurement. A token-aware caller must
// include its fixed system and correction messages in that measurement.
func coordinatorPromptMeasured(state State, limit int, measure func(string) int) (string, error) {
	if limit <= 0 || measure == nil {
		return "", fmt.Errorf("coordinator context has no input allowance")
	}
	packet := coordinatorPayload(state)
	encode := func() string {
		data, _ := json.Marshal(packet)
		return string(data)
	}
	prompt := encode()
	if measure(prompt) <= limit {
		return prompt, nil
	}
	// Historical project context is useful, but current-run evidence and the
	// operator's latest direction take priority when the request is crowded.
	if packet.ProjectHistory != nil {
		packet.ProjectHistory = nil
		packet.ContextNotes = append(packet.ContextNotes, "Earlier project sessions were omitted from this bounded request; their saved evidence remains available in the project directory.")
		prompt = encode()
		if measure(prompt) <= limit {
			return prompt, nil
		}
	}
	// The assessment remains complete on disk. Only a planning request that
	// exceeds its allowance starts shortening older material.
	protectedEvidence := make(map[string]bool)
	for _, plan := range state.Plans {
		for _, finding := range plan.Findings {
			for _, ref := range finding.Evidence {
				protectedEvidence[ref] = true
			}
			for _, ref := range finding.References {
				protectedEvidence[ref] = true
			}
		}
	}
	for _, lead := range packet.Assessment.OpenLeads {
		for _, ref := range lead.Evidence {
			protectedEvidence[ref] = true
		}
	}
	oldCount := recentBatchStart(packet.Assessment)
	for i := 0; i < oldCount; i++ {
		result := &packet.Assessment.Results[i]
		result.Task.Goal = promptExcerpt(result.Task.Goal, 320)
		result.Task.DoneWhen = promptExcerpt(result.Task.DoneWhen, 240)
		result.OmittedEvidence += len(result.Evidence)
		result.Evidence = nil
		result.EvidenceIndex = nil
		refs := packet.RecordedEvidence[result.Task.ID]
		kept := make([]string, 0, len(refs))
		for _, ref := range refs {
			if protectedEvidence[ref] {
				kept = append(kept, ref)
			}
		}
		if len(kept) == 0 {
			delete(packet.RecordedEvidence, result.Task.ID)
		} else {
			packet.RecordedEvidence[result.Task.ID] = kept
		}
	}
	if oldCount > 0 {
		packet.ContextNotes = append(packet.ContextNotes, "Older workers are compact navigation entries. Full results and evidence remain under tasks/<task-id>/; delegate a focused evidence read when an older detail matters.")
	}
	prompt = encode()
	if measure(prompt) <= limit {
		return prompt, nil
	}
	// Pressure relief stays ordered: older plan prose, assistant replies, then
	// saved worker previews before earlier operator direction. The current goal,
	// scope, task identities, latest operator message, and finding references
	// are never discarded.
	for i := 0; i < len(packet.Assessment.Plans)-2; i++ {
		plan := &packet.Assessment.Plans[i]
		plan.Summary = promptExcerpt(plan.Summary, 160)
		plan.Gaps = nil
		for j := range plan.Tasks {
			plan.Tasks[j].Goal = ""
			plan.Tasks[j].DoneWhen = ""
			plan.Tasks[j].StrategyHints = nil
		}
	}
	packet.ContextNotes = append(packet.ContextNotes, "Older plan prose and gaps were shortened to fit the planning request; the saved assessment retains them.")
	prompt = encode()
	if measure(prompt) <= limit {
		return prompt, nil
	}
	protectedMessage := -1
	for i := len(packet.Assessment.OperatorMessages) - 1; i >= 0; i-- {
		message := strings.ToLower(strings.TrimSpace(packet.Assessment.OperatorMessages[i]))
		if strings.HasPrefix(message, "user:") || strings.HasPrefix(message, "operator answer:") {
			protectedMessage = i
			break
		}
	}
	dropMessages := func(repliesOnly bool) {
		for len(packet.Assessment.OperatorMessages) > 1 && measure(prompt) > limit {
			remove := -1
			for i, entry := range packet.Assessment.OperatorMessages {
				if i == protectedMessage {
					continue
				}
				message := strings.ToLower(strings.TrimSpace(entry))
				if !repliesOnly || strings.HasPrefix(message, "assistant:") || strings.HasPrefix(message, "coordinator:") {
					remove = i
					break
				}
			}
			if remove < 0 {
				break
			}
			packet.Assessment.OperatorMessages = append(packet.Assessment.OperatorMessages[:remove], packet.Assessment.OperatorMessages[remove+1:]...)
			packet.OmittedMessages++
			if protectedMessage > remove {
				protectedMessage--
			}
			prompt = encode()
		}
	}
	// Earlier assistant replies have lower priority than operator direction.
	// Preserve that direction while saved worker prose can still be shortened.
	dropMessages(true)
	for i := 0; i < oldCount; i++ {
		packet.Assessment.Results[i].Summary = promptExcerptEnds(packet.Assessment.Results[i].Summary, 2048)
	}
	prompt = encode()
	if measure(prompt) <= limit {
		return prompt, nil
	}
	for i := oldCount; i < len(packet.Assessment.Results); i++ {
		result := &packet.Assessment.Results[i]
		result.Summary = promptExcerpt(result.Summary, 2048)
		if len(result.Evidence) > 1 {
			result.OmittedEvidence += len(result.Evidence) - 1
			result.Evidence = result.Evidence[len(result.Evidence)-1:]
		}
	}
	packet.ContextNotes = append(packet.ContextNotes, "Recent worker previews were shortened for this request; exact outcomes and registered evidence remain in saved task records.")
	prompt = encode()
	// Only after shortening saved worker prose should older operator turns
	// leave this request. The latest operator direction stays protected.
	dropMessages(false)
	if measure(prompt) > limit {
		for i := 0; i < oldCount; i++ {
			packet.Assessment.Results[i].Summary = promptExcerptEnds(packet.Assessment.Results[i].Summary, 512)
		}
		for i := 0; i < len(packet.Assessment.Plans)-1; i++ {
			packet.Assessment.Plans[i].Findings = nil
		}
		packet.ContextNotes = append(packet.ContextNotes, "Distant worker conclusions and superseded finding revisions are short navigation entries; use saved results and plans for omitted details.")
		prompt = encode()
	}
	if measure(prompt) > limit {
		for i := oldCount; i < len(packet.Assessment.Results); i++ {
			packet.Assessment.Results[i].Summary = promptExcerptEnds(packet.Assessment.Results[i].Summary, 1024)
		}
		packet.ContextNotes = append(packet.ContextNotes, "Recent worker prose was shortened at both ends; exact observations remain in the evidence index and saved task records.")
		prompt = encode()
	}
	if measure(prompt) > limit {
		for len(packet.Assessment.Plans) > 1 && measure(prompt) > limit {
			packet.Assessment.Plans = packet.Assessment.Plans[1:]
			packet.OmittedPlans++
			prompt = encode()
		}
	}
	if measure(prompt) > limit {
		// Older results may become navigation entries, but never silently
		// discard one member of the current batch to fit the request.
		for oldCount > 0 && measure(prompt) > limit {
			remove := -1
			for i, result := range packet.Assessment.Results[:oldCount] {
				if len(packet.RecordedEvidence[result.Task.ID]) == 0 {
					remove = i
					break
				}
			}
			if remove < 0 {
				break
			}
			packet.Assessment.Results = append(packet.Assessment.Results[:remove], packet.Assessment.Results[remove+1:]...)
			packet.OmittedResults++
			oldCount--
			prompt = encode()
		}
	}
	if measure(prompt) > limit {
		// A large parallel batch must still reach the next decision. Keep an
		// outcome card for every worker and offload previews and excess source
		// paths to the already-saved result.json files. Findings and open leads
		// retain their registered references for the next decision.
		compactCoordinatorResultIndex(&packet, oldCount)
		packet.ContextNotes = append(packet.ContextNotes, "Worker previews and uncited evidence paths were offloaded for this request. Every result_ref names a full saved task result; delegate a focused read before relying on omitted observations or citing their evidence.")
		prompt = encode()
	}
	if measure(prompt) > limit {
		return "", fmt.Errorf("coordinator context needs %d budget units after compaction; allowance is %d; narrow the planning question or use a larger model profile", measure(prompt), limit)
	}
	return prompt, nil
}

func compactCoordinatorResultIndex(packet *coordinatorModelPacket, oldCount int) {
	protected := make(map[string]bool)
	for _, plan := range packet.Assessment.Plans {
		for _, finding := range plan.Findings {
			for _, ref := range append(append([]string(nil), finding.Evidence...), finding.References...) {
				protected[ref] = true
			}
		}
	}
	for _, lead := range packet.Assessment.OpenLeads {
		for _, ref := range lead.Evidence {
			protected[ref] = true
		}
	}
	for _, result := range packet.Assessment.Results {
		if result.Verification != nil {
			for _, ref := range result.Verification.Evidence {
				protected[ref] = true
			}
		}
	}
	for i := range packet.Assessment.Results {
		result := &packet.Assessment.Results[i]
		result.Task.Goal = promptExcerpt(result.Task.Goal, 240)
		result.Task.DoneWhen = promptExcerpt(result.Task.DoneWhen, 160)
		result.Task.StrategyHints = nil
		result.OmittedEvidence += len(result.Evidence)
		result.Evidence = nil
		result.EvidenceIndex = nil
		limit := 640
		if i < oldCount {
			limit = 320
		}
		result.Summary = promptExcerptEnds(result.Summary, limit)
		result.Error = promptExcerptEnds(result.Error, 240)
		for j := range result.LeadHints {
			result.LeadHints[j] = promptExcerptEnds(result.LeadHints[j], 800)
		}
		if result.Verification != nil {
			verification := *result.Verification
			result.Verification = &verification
			result.Verification.Reason = promptExcerpt(result.Verification.Reason, 320)
			result.Verification.AlternativeResult = promptExcerpt(result.Verification.AlternativeResult, 240)
		}
		refs := packet.RecordedEvidence[result.Task.ID]
		if len(refs) == 0 {
			continue
		}
		kept := make([]string, 0, len(refs))
		for _, ref := range refs {
			if protected[ref] {
				kept = append(kept, ref)
			}
		}
		// One current source pointer helps the coordinator identify which
		// observation to inspect next without resending every path.
		if i >= oldCount && !protected[refs[len(refs)-1]] {
			kept = append(kept, refs[len(refs)-1])
		}
		if len(kept) == 0 {
			delete(packet.RecordedEvidence, result.Task.ID)
		} else {
			packet.RecordedEvidence[result.Task.ID] = kept
		}
	}
}

// Keep every result from the most recent completed worker batch in the same
// priority tier. A batch may eventually contain more than two workers; its
// first result is no less current than its last result.
func recentBatchStart(state coordinatorPromptState) int {
	for planIndex := len(state.Plans) - 1; planIndex >= 0; planIndex-- {
		plan := state.Plans[planIndex]
		ids := make(map[string]bool, len(plan.ApprovedTaskIDs))
		for _, id := range plan.ApprovedTaskIDs {
			ids[id] = true
		}
		if len(ids) == 0 && len(plan.SkippedTaskIDs) == 0 {
			for _, task := range plan.Tasks {
				ids[task.ID] = true
			}
		}
		if len(ids) == 0 {
			continue
		}
		for i, result := range state.Results {
			if ids[result.Task.ID] {
				return i
			}
		}
	}
	// Without a matching plan we cannot know which outcomes are historical.
	// Preserve them rather than guessing that only the last two are current.
	return 0
}

// A historical result may place its next lead after a lengthy preamble.
// Preserve both ends when the request must shed that result's middle prose.
func promptExcerptEnds(value string, limit int) string {
	const marker = "\n[excerpt middle omitted; consult the saved result]\n"
	if len(value) <= limit || limit <= len(marker)+2 {
		return value
	}
	headBytes := (limit - len(marker)) * 3 / 4
	tailBytes := limit - len(marker) - headBytes
	for headBytes > 0 && !utf8.RuneStart(value[headBytes]) {
		headBytes--
	}
	tailStart := len(value) - tailBytes
	for tailStart < len(value) && !utf8.RuneStart(value[tailStart]) {
		tailStart++
	}
	return strings.TrimSpace(value[:headBytes]) + marker + strings.TrimSpace(value[tailStart:])
}
