package orchestrator

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const findingTypeCVECandidateClaim = "cve_candidate_claim"

var reportCandidateCVEHeadingPattern = regexp.MustCompile(`(?i)^###\s+(CVE-\d{4}-\d{4,})\s+\(([^)]+)\)\s*$`)

type reportCVECandidateClaim struct {
	CVE           string
	Reason        string
	Evidence      []string
	MissingPhases []string
}

func emitReportCandidateFindings(manager *Manager, cfg WorkerRunConfig, task TaskSpec, reportOutput []byte, fallbackEvidence []string) error {
	if !taskRequiresReportSynthesis(task) {
		return nil
	}
	claims := extractReportCandidateCVEClaims(reportOutput)
	if len(claims) == 0 {
		return nil
	}
	target := primaryTaskTarget(task)
	if target == "" {
		target = firstTaskTarget(task.Targets)
	}
	for _, claim := range claims {
		if strings.TrimSpace(claim.CVE) == "" {
			continue
		}
		evidence := append([]string{}, fallbackEvidence...)
		evidence = append(evidence, claim.Evidence...)
		evidence = compactStringSlice(evidence)
		metadata := map[string]any{
			"finding_state":  FindingStateCandidate,
			"cve":            claim.CVE,
			"claim_reason":   claim.Reason,
			"missing_phases": strings.Join(claim.MissingPhases, ","),
			"dedupe_key":     buildCandidateCVEClaimDedupeKey(target, claim.CVE),
		}
		if err := manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFinding, map[string]any{
			"target":       target,
			"finding_type": findingTypeCVECandidateClaim,
			"title":        fmt.Sprintf("%s candidate requires bounded confirmation", claim.CVE),
			"state":        FindingStateCandidate,
			"severity":     "medium",
			"confidence":   "medium",
			"source":       "report_synthesis",
			"evidence":     evidence,
			"metadata":     metadata,
		}); err != nil {
			return err
		}
	}
	return nil
}

func buildCandidateCVEClaimDedupeKey(target, cve string) string {
	parts := []string{
		findingTypeCVECandidateClaim,
		strings.ToLower(strings.TrimSpace(target)),
		strings.ToUpper(strings.TrimSpace(cve)),
	}
	return strings.Join(parts, ":")
}

func extractReportCandidateCVEClaims(reportOutput []byte) []reportCVECandidateClaim {
	text := strings.TrimSpace(string(reportOutput))
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	inFindings := false
	currentState := ""
	current := reportCVECandidateClaim{}
	claims := []reportCVECandidateClaim{}
	flush := func() {
		if strings.TrimSpace(current.CVE) == "" {
			current = reportCVECandidateClaim{}
			return
		}
		current.Evidence = compactStringSlice(current.Evidence)
		current.MissingPhases = compactStringSlice(current.MissingPhases)
		if current.Reason == "" {
			current.Reason = "candidate requires bounded confirmation by policy"
		}
		claims = append(claims, current)
		current = reportCVECandidateClaim{}
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "## ") {
			flush()
			inFindings = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "## ")), "Findings")
			currentState = ""
			continue
		}
		if !inFindings {
			continue
		}
		if strings.HasPrefix(line, "### ") {
			heading := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			switch strings.ToLower(heading) {
			case "validated findings":
				flush()
				currentState = "validated"
				continue
			case "candidate findings":
				flush()
				currentState = "candidate"
				continue
			case "rejected findings":
				flush()
				currentState = "rejected"
				continue
			case "needs review":
				flush()
				currentState = "needs_review"
				continue
			}
			matches := reportCandidateCVEHeadingPattern.FindStringSubmatch(line)
			if len(matches) == 3 {
				flush()
				if currentState != "candidate" {
					continue
				}
				stateToken := strings.ToLower(strings.TrimSpace(matches[2]))
				if stateToken != "candidate" {
					continue
				}
				current.CVE = strings.ToUpper(strings.TrimSpace(matches[1]))
			}
			continue
		}
		if strings.TrimSpace(current.CVE) == "" {
			continue
		}
		if strings.HasPrefix(line, "- Claim reason:") {
			current.Reason = strings.TrimSpace(strings.TrimPrefix(line, "- Claim reason:"))
			continue
		}
		if strings.HasPrefix(line, "- Evidence file:") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "- Evidence file:"))
			if path != "" {
				current.Evidence = append(current.Evidence, path)
			}
			continue
		}
		if strings.HasPrefix(line, "- Phase gate:") {
			current.MissingPhases = append(current.MissingPhases, missingPhasesFromPhaseGateLine(line)...)
			continue
		}
	}
	flush()
	claims = dedupeReportCandidateClaims(claims)
	return claims
}

func missingPhasesFromPhaseGateLine(line string) []string {
	body := strings.TrimSpace(strings.TrimPrefix(line, "- Phase gate:"))
	if body == "" {
		return nil
	}
	missing := make([]string, 0)
	seen := map[string]struct{}{}
	for _, token := range strings.Fields(body) {
		parts := strings.SplitN(strings.TrimSpace(token), "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.ToLower(strings.TrimSpace(parts[1]))
		if key == "" || value != "no" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		missing = append(missing, key)
	}
	sort.Strings(missing)
	return missing
}

func dedupeReportCandidateClaims(claims []reportCVECandidateClaim) []reportCVECandidateClaim {
	if len(claims) == 0 {
		return nil
	}
	out := make([]reportCVECandidateClaim, 0, len(claims))
	seen := map[string]int{}
	for _, claim := range claims {
		key := strings.ToUpper(strings.TrimSpace(claim.CVE))
		if key == "" {
			continue
		}
		if idx, ok := seen[key]; ok {
			merged := out[idx]
			if merged.Reason == "" && claim.Reason != "" {
				merged.Reason = claim.Reason
			}
			merged.Evidence = compactStringSlice(append(merged.Evidence, claim.Evidence...))
			merged.MissingPhases = compactStringSlice(append(merged.MissingPhases, claim.MissingPhases...))
			out[idx] = merged
			continue
		}
		claim.CVE = key
		claim.Evidence = compactStringSlice(claim.Evidence)
		claim.MissingPhases = compactStringSlice(claim.MissingPhases)
		seen[key] = len(out)
		out = append(out, claim)
	}
	return out
}
