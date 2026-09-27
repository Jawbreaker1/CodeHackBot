package webapp

import (
	"sort"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

// Coverage describes recorded work against a declared scope. It is an
// assessment map, not an inferred network or application topology.
type analysisCoverage struct {
	Scope      string                    `json:"scope"`
	SessionIDs []string                  `json:"session_ids"`
	Tests      []analysisCoverageTest    `json:"tests"`
	WeakPoints []analysisCoverageFinding `json:"weak_points"`
	Challenges []analysisChallenge       `json:"challenges"`
	Gaps       []string                  `json:"gaps"`
}

type analysisCoverageTest struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	Goal      string `json:"goal"`
	Status    string `json:"status"`
}

type analysisCoverageFinding struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Severity  string `json:"severity,omitempty"`
}

type analysisCorrelation struct {
	Label   string                     `json:"label"`
	Basis   string                     `json:"basis"`
	Matches []analysisCorrelationMatch `json:"matches"`
}

type analysisCorrelationMatch struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Scope     string `json:"scope"`
}

func coverageForAssessment(id string, state assessment.State, findings []analysisFinding, challenges []analysisChallenge) analysisCoverage {
	coverage := analysisCoverage{Scope: state.Scope, SessionIDs: []string{id}, Gaps: uniqueStrings(assessment.ReportGaps(state))}
	for _, result := range state.Results {
		coverage.Tests = append(coverage.Tests, analysisCoverageTest{SessionID: id, TaskID: result.Task.ID, Goal: result.Task.Goal, Status: result.Status})
	}
	for _, finding := range findings {
		coverage.WeakPoints = append(coverage.WeakPoints, analysisCoverageFinding{SessionID: id, Title: finding.Title, Status: finding.Status, Severity: finding.Severity})
	}
	coverage.Challenges = append(coverage.Challenges, challenges...)
	return coverage
}

func mergeCoverage(sessions []analysisView) []analysisCoverage {
	var merged []analysisCoverage
	indices := map[string]int{}
	for _, session := range sessions {
		for _, entry := range session.Coverage {
			key := entry.Scope
			if strings.TrimSpace(key) == "" {
				key = "missing:" + session.ID // Unknown scopes must not be merged.
			}
			index, found := indices[key]
			if !found {
				index = len(merged)
				indices[key] = index
				merged = append(merged, analysisCoverage{Scope: entry.Scope})
			}
			target := &merged[index]
			target.SessionIDs = append(target.SessionIDs, entry.SessionIDs...)
			target.Tests = append(target.Tests, entry.Tests...)
			target.WeakPoints = append(target.WeakPoints, entry.WeakPoints...)
			target.Challenges = append(target.Challenges, entry.Challenges...)
			target.Gaps = append(target.Gaps, entry.Gaps...)
		}
	}
	for i := range merged {
		merged[i].SessionIDs = uniqueStrings(merged[i].SessionIDs)
		merged[i].Gaps = uniqueStrings(merged[i].Gaps)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Scope < merged[j].Scope })
	return merged
}

// Correlations are exact recorded overlaps for review, never an inferred
// attack chain or a merged finding. Every source session remains visible.
func correlateFindings(findings []analysisFinding) []analysisCorrelation {
	type group struct {
		label, basis string
		matches      []analysisCorrelationMatch
	}
	groups := map[string]*group{}
	add := func(key, label, basis string, finding analysisFinding) {
		item := groups[key]
		if item == nil {
			item = &group{label: label, basis: basis}
			groups[key] = item
		}
		item.matches = append(item.matches, analysisCorrelationMatch{SessionID: finding.SessionID, Title: finding.Title, Status: finding.Status, Scope: finding.Scope})
	}
	for _, finding := range findings {
		if strings.TrimSpace(finding.Scope) != "" {
			add("scope:"+finding.Scope, finding.Scope, "Findings on the same declared scope; review whether they interact.", finding)
		}
		for _, cve := range finding.CVEIDs {
			cve = strings.TrimSpace(cve)
			if cve != "" {
				add("cve:"+cve, cve, "Same recorded CVE identifier; applicability and impact remain separate for each session.", finding)
			}
		}
		for _, software := range finding.AffectedSoftware {
			software = strings.TrimSpace(software)
			if software != "" {
				add("software:"+strings.ToLower(software), software, "Same recorded affected-software label; verify versions, deployments, and impact separately.", finding)
			}
		}
	}
	var result []analysisCorrelation
	for _, group := range groups {
		sessions := map[string]bool{}
		for _, match := range group.matches {
			sessions[match.SessionID] = true
		}
		if len(sessions) < 2 {
			continue
		}
		result = append(result, analysisCorrelation{Label: group.label, Basis: group.basis, Matches: group.matches})
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].Matches) != len(result[j].Matches) {
			return len(result[i].Matches) > len(result[j].Matches)
		}
		return result[i].Label < result[j].Label
	})
	return result
}
