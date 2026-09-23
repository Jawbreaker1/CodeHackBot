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
	if maxBytes <= 0 {
		return "", fmt.Errorf("coordinator context has no input allowance")
	}
	packet := coordinatorPayload(state)
	protectedEvidence := make(map[string]bool)
	for _, plan := range state.Plans {
		for _, finding := range plan.Findings {
			for _, ref := range finding.Evidence {
				protectedEvidence[ref] = true
			}
		}
	}
	oldCount := len(packet.Assessment.Results) - 2
	if oldCount < 0 {
		oldCount = 0
	}
	for i := 0; i < oldCount; i++ {
		result := &packet.Assessment.Results[i]
		result.Task.Goal = promptExcerpt(result.Task.Goal, 320)
		result.Task.DoneWhen = promptExcerpt(result.Task.DoneWhen, 240)
		result.OmittedEvidence += len(result.Evidence)
		result.Evidence = nil
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
	if len(packet.Assessment.OperatorMessages) > 12 {
		packet.Assessment.OperatorMessages = packet.Assessment.OperatorMessages[len(packet.Assessment.OperatorMessages)-12:]
		packet.ContextNotes = append(packet.ContextNotes, "Only the latest operator conversation excerpts are in this planning view; the durable conversation is retained locally.")
	}
	encode := func() string {
		data, _ := json.Marshal(packet)
		return string(data)
	}
	prompt := encode()
	if len(prompt) <= maxBytes {
		return prompt, nil
	}
	// Pressure relief stays ordered: historical discussion, then older result
	// previews, then recent previews. The current goal, scope, task identities,
	// latest operator message, and finding references are never discarded.
	for i := 0; i < len(packet.Assessment.Plans)-2; i++ {
		plan := &packet.Assessment.Plans[i]
		plan.Summary = promptExcerpt(plan.Summary, 160)
		plan.Gaps = nil
	}
	packet.ContextNotes = append(packet.ContextNotes, "Older plan prose and gaps were shortened to fit the planning request; the saved assessment retains them.")
	prompt = encode()
	if len(prompt) <= maxBytes {
		return prompt, nil
	}
	if len(packet.Assessment.OperatorMessages) > 6 {
		packet.Assessment.OperatorMessages = packet.Assessment.OperatorMessages[len(packet.Assessment.OperatorMessages)-6:]
	}
	for i := 0; i < oldCount; i++ {
		packet.Assessment.Results[i].Summary = promptExcerptEnds(packet.Assessment.Results[i].Summary, 2048)
	}
	prompt = encode()
	if len(prompt) <= maxBytes {
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
	if len(prompt) > maxBytes {
		for i := 0; i < oldCount; i++ {
			packet.Assessment.Results[i].Summary = promptExcerptEnds(packet.Assessment.Results[i].Summary, 512)
		}
		for i := 0; i < len(packet.Assessment.Plans)-1; i++ {
			packet.Assessment.Plans[i].Findings = nil
		}
		packet.ContextNotes = append(packet.ContextNotes, "Distant worker conclusions and superseded finding revisions are short navigation entries; use saved results and plans for omitted details.")
		prompt = encode()
	}
	if len(prompt) > maxBytes {
		return "", fmt.Errorf("coordinator context needs %d bytes after compaction; allowance is %d; narrow the planning question or use a larger model profile", len(prompt), maxBytes)
	}
	return prompt, nil
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
