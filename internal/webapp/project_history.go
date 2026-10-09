package webapp

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

const (
	maxHistorySessions = 6
	maxHistoryFindings = 8
	maxHistoryGaps     = 3
)

// A target label helps find prior work; it never grants scope or execution.
func canonicalTarget(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "`\"'.,;()[]{}")
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	if value == "" || strings.ContainsAny(value, " \t\n") {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if net.ParseIP(host) == nil {
		if !strings.Contains(host, ".") || len(host) > 253 {
			return ""
		}
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return ""
			}
			for _, ch := range label {
				if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
					return ""
				}
			}
		}
	}
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	if path := strings.TrimSuffix(parsed.EscapedPath(), "/"); path != "" {
		return parsed.Scheme + "://" + host + path
	}
	return host
}

// Historical sessions predate the structured target field. Only an explicit
// "hostname X" clause is migrated on read; arbitrary dotted words in prose
// (for example a .zip filename) are not interpreted as network targets.
func legacyHostname(scope string) string {
	words := strings.Fields(scope)
	target := ""
	for i := 0; i+1 < len(words); i++ {
		if strings.Trim(strings.ToLower(words[i]), "`\"'.,;:()") != "hostname" {
			continue
		}
		candidate := canonicalTarget(words[i+1])
		if candidate == "" {
			continue
		}
		if target != "" && target != candidate {
			return ""
		}
		target = candidate
	}
	return target
}

func assessmentTarget(state assessment.State) string {
	if target := canonicalTarget(state.Target); target != "" {
		return target
	}
	return legacyHostname(state.Scope)
}

// projectHistory is deliberately bounded and contains source references, not
// previous workers' raw output. The coordinator decides which old lead matters.
func (s *Server) projectHistory(project, target, exceptID string) *assessment.ProjectHistory {
	target = canonicalTarget(target)
	if target == "" {
		return nil
	}
	s.mu.RLock()
	runs := make([]*run, 0, len(s.runs))
	for _, current := range s.runs {
		if current.id != exceptID {
			runs = append(runs, current)
		}
	}
	s.mu.RUnlock()
	type entry struct {
		at      time.Time
		session assessment.PriorSession
	}
	var entries []entry
	for _, current := range runs {
		current.mu.RLock()
		state, status, updated, root := current.state, current.status, current.updatedAt, current.root
		if current.customer != project || assessmentTarget(state) != target || len(state.Results) == 0 || status == "running" || status == "starting" {
			current.mu.RUnlock()
			continue
		}
		view := buildAnalysis(current.id, project, state, nil)
		current.mu.RUnlock()
		prior := assessment.PriorSession{ID: current.id, Goal: promptExcerptForProject(state.Goal, 160), Status: status, FinishedAt: state.FinishedAt}
		index := filepath.Join(root, "evidence-index.md")
		if _, err := os.Stat(index); err == nil {
			prior.EvidenceIndex = index
		}
		for _, finding := range view.Findings {
			if len(prior.Findings) == maxHistoryFindings {
				break
			}
			item := assessment.PriorFinding{ID: finding.ID, Title: promptExcerptForProject(finding.Title, 140), Status: finding.Status, Severity: finding.Severity}
			if len(finding.Evidence) > 0 {
				item.EvidenceRef = finding.Evidence[0]
			}
			prior.Findings = append(prior.Findings, item)
		}
		prior.OmittedFindings = len(view.Findings) - len(prior.Findings)
		for _, review := range view.PriorReviews {
			if len(prior.Reviews) == 4 {
				break
			}
			prior.Reviews = append(prior.Reviews, review.PriorReview)
		}
		for _, gap := range view.Gaps {
			if len(prior.Gaps) == maxHistoryGaps {
				break
			}
			prior.Gaps = append(prior.Gaps, promptExcerptForProject(gap, 200))
		}
		prior.OmittedGaps = len(view.Gaps) - len(prior.Gaps)
		if prior.FinishedAt.IsZero() {
			prior.FinishedAt = updated
		}
		entries = append(entries, entry{at: prior.FinishedAt, session: prior})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].at.After(entries[j].at) })
	omittedSessions := 0
	if len(entries) > maxHistorySessions {
		omittedSessions = len(entries) - maxHistorySessions
		entries = entries[:maxHistorySessions]
	}
	if len(entries) == 0 {
		return nil
	}
	history := &assessment.ProjectHistory{Project: project, Target: target, OmittedSessions: omittedSessions}
	for _, item := range entries {
		history.Sessions = append(history.Sessions, item.session)
	}
	return history
}

func promptExcerptForProject(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > limit {
		return value[:limit-1] + "…"
	}
	return value
}
