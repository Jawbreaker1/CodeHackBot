package cli

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

type assistBudget struct {
	goal       string
	used       int
	remaining  int
	currentCap int
	hardCap    int
	extensions int
	stalls     int
	lastReason string

	seenActions  map[string]struct{}
	seenEvidence map[string]struct{}
}

func newAssistBudget(goal string, base int) *assistBudget {
	if base <= 0 {
		base = 6
	}
	extra := estimateAssistBudgetExtra(goal)
	planned := base + extra
	if planned < 3 {
		planned = 3
	}
	if planned > 24 {
		planned = 24
	}
	hardCap := base * 3
	if hardCap < planned+4 {
		hardCap = planned + 4
	}
	if hardCap < 8 {
		hardCap = 8
	}
	if hardCap > 36 {
		hardCap = 36
	}
	return &assistBudget{
		goal:         strings.TrimSpace(goal),
		remaining:    planned,
		currentCap:   planned,
		hardCap:      hardCap,
		lastReason:   fmt.Sprintf("planned budget=%d (hard cap=%d)", planned, hardCap),
		seenActions:  map[string]struct{}{},
		seenEvidence: map[string]struct{}{},
	}
}

func (b *assistBudget) enableLongHorizon() {
	if b == nil {
		return
	}
	if b.currentCap < 12 {
		delta := 12 - b.currentCap
		b.remaining += delta
		b.currentCap = b.used + b.remaining
	}
	if b.hardCap < b.currentCap+18 {
		b.hardCap = b.currentCap + 18
	}
	if b.hardCap > 72 {
		b.hardCap = 72
	}
	b.lastReason = fmt.Sprintf("open-like long-horizon budget=%d (hard cap=%d)", b.currentCap, b.hardCap)
}

func estimateAssistBudgetExtra(goal string) int {
	lower := strings.ToLower(strings.TrimSpace(goal))
	if lower == "" {
		return 0
	}
	extra := 0

	sequencers := []string{" and ", " then ", " after ", " also ", " plus ", " finally "}
	for _, token := range sequencers {
		if strings.Contains(lower, token) {
			extra++
		}
	}
	if strings.Count(lower, ",") >= 2 {
		extra++
	}

	networkHints := []string{"network", "lan", "subnet", "host", "hosts", "port", "ports", "scan", "recon", "service", "fingerprint"}
	networkScore := 0
	for _, hint := range networkHints {
		if strings.Contains(lower, hint) {
			networkScore++
		}
	}
	if networkScore >= 2 {
		extra += 3
	}

	if cidrPattern.MatchString(lower) {
		extra += 3
	}
	if strings.Contains(lower, "report") || strings.Contains(lower, "summary") || strings.Contains(lower, "findings") {
		extra++
	}
	if len(strings.Fields(lower)) > 24 {
		extra++
	}
	if extra > 8 {
		extra = 8
	}
	return extra
}

var cidrPattern = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}/\d{1,2}\b`)

func (b *assistBudget) stepLabel() (int, int) {
	return b.used + 1, b.currentCap
}

func (b *assistBudget) exhausted() bool {
	return b.remaining <= 0
}

func (b *assistBudget) consume(reason string) {
	if b.remaining > 0 {
		b.remaining--
	}
	b.used++
	if strings.TrimSpace(reason) != "" {
		b.lastReason = reason
	}
}

func (b *assistBudget) onStall(reason string) {
	b.stalls++
	if strings.TrimSpace(reason) != "" {
		b.lastReason = reason
	}
}

func (b *assistBudget) onProgress(reason string) {
	b.stalls = 0
	if b.used+b.remaining < b.hardCap {
		b.remaining++
		b.extensions++
		b.currentCap = b.used + b.remaining
	}
	if strings.TrimSpace(reason) != "" {
		b.lastReason = reason
	}
}

func (b *assistBudget) extendForPersistence(delta int, reason string) bool {
	if b == nil || delta <= 0 {
		return false
	}
	extended := false
	for i := 0; i < delta; i++ {
		if b.used+b.remaining >= b.hardCap {
			break
		}
		b.remaining++
		b.extensions++
		extended = true
	}
	if !extended {
		return false
	}
	b.currentCap = b.used + b.remaining
	b.stalls = 0
	if strings.TrimSpace(reason) != "" {
		b.lastReason = reason
	}
	return true
}

func (b *assistBudget) trackProgress(actionKey string, beforeObsSig string, afterObsSig string) (bool, string) {
	progress := false
	reasons := []string{}
	lowValueNoEvidence := false
	actionKey = strings.TrimSpace(actionKey)
	actionNovel := false
	if actionKey != "" {
		if _, exists := b.seenActions[actionKey]; !exists {
			b.seenActions[actionKey] = struct{}{}
			actionNovel = true
		}
	}
	afterObsSig = strings.TrimSpace(afterObsSig)
	evidenceNovel := false
	if afterObsSig != "" && afterObsSig != strings.TrimSpace(beforeObsSig) {
		if _, exists := b.seenEvidence[afterObsSig]; !exists {
			b.seenEvidence[afterObsSig] = struct{}{}
			evidenceNovel = true
		}
	}
	if evidenceNovel {
		progress = true
		reasons = append(reasons, "new evidence")
	}
	if actionNovel {
		if isLowValueActionKey(actionKey) && !evidenceNovel {
			lowValueNoEvidence = true
		} else {
			progress = true
			reasons = append(reasons, "new action")
		}
	}
	if !progress {
		if lowValueNoEvidence {
			return false, "low-value action without new evidence"
		}
		return false, "no new evidence"
	}
	return true, strings.Join(reasons, " + ")
}

func isLowValueActionKey(actionKey string) bool {
	lower := strings.ToLower(strings.TrimSpace(actionKey))
	switch {
	case strings.HasPrefix(lower, "list_dir\x1f"),
		strings.HasPrefix(lower, "read_file\x1f"):
		return true
	default:
		return false
	}
}

func (r *Runner) startAssistRuntime(goal string, mode string, budget *assistBudget) {
	trimmedGoal := strings.TrimSpace(goal)
	if !strings.EqualFold(strings.TrimSpace(r.assistExecGoal), trimmedGoal) {
		r.assistExecApproved = false
		r.assistExecGoal = trimmedGoal
	}
	if budget == nil {
		r.assistRuntime = assistRuntimeStatus{}
		return
	}
	r.assistRuntime = assistRuntimeStatus{
		Active:      true,
		Goal:        trimmedGoal,
		Step:        budget.used,
		Remaining:   budget.remaining,
		CurrentCap:  budget.currentCap,
		HardCap:     budget.hardCap,
		Extensions:  budget.extensions,
		Stalls:      budget.stalls,
		LastReason:  budget.lastReason,
		CurrentMode: strings.TrimSpace(mode),
	}
}

func (r *Runner) updateAssistRuntime(mode string, budget *assistBudget) {
	if budget == nil {
		return
	}
	r.assistRuntime.Active = true
	r.assistRuntime.Step = budget.used
	r.assistRuntime.Remaining = budget.remaining
	r.assistRuntime.CurrentCap = budget.currentCap
	r.assistRuntime.HardCap = budget.hardCap
	r.assistRuntime.Extensions = budget.extensions
	r.assistRuntime.Stalls = budget.stalls
	r.assistRuntime.LastReason = budget.lastReason
	r.assistRuntime.CurrentMode = strings.TrimSpace(mode)
}

func (r *Runner) clearAssistRuntime() {
	r.assistRuntime = assistRuntimeStatus{}
	r.assistExecApproved = false
	r.assistExecGoal = ""
}

func (r *Runner) latestObservationSignature() string {
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return ""
	}
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil || len(state.RecentObservations) == 0 {
		return ""
	}
	item := state.RecentObservations[len(state.RecentObservations)-1]
	parts := []string{
		strings.ToLower(strings.TrimSpace(item.Kind)),
		strings.ToLower(strings.TrimSpace(item.Command)),
		strings.ToLower(strings.TrimSpace(strings.Join(item.Args, " "))),
		collapseWhitespace(strings.ToLower(strings.TrimSpace(item.OutputExcerpt))),
		collapseWhitespace(strings.ToLower(strings.TrimSpace(item.Error))),
	}
	sig := strings.Join(parts, "|")
	if len(sig) > 1200 {
		sig = sig[:1200]
	}
	return sig
}
