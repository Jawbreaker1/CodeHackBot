package assessment

import "strings"

// reviewFindings keeps historical session state intact while preventing an
// older, unchallenged reproduction claim from being presented as confirmed in
// a newly rendered report.
func reviewFindings(state State) ([]Finding, bool) {
	findings := append([]Finding(nil), CurrentFindings(state.Plans)...)
	legacyClaims := false
	for i := range findings {
		if findings[i].Status == "reproduced" && SupportedChallengeForFinding(state.Results, findings[i]) == nil {
			findings[i].Status = "candidate"
			legacyClaims = true
		}
	}
	return findings, legacyClaims
}

// SupportedChallengeForFinding returns the completed worker result only when
// its challenge verdict cites its own registered execution log and the finding
// cites that same log. It does not prove the model's interpretation is correct.
func SupportedChallengeForFinding(results []Result, finding Finding) *Result {
	if finding.ValidationTask == "" {
		return nil
	}
	for i := range results {
		result := &results[i]
		request, verdict := result.Task.Verification, result.Verification
		if result.Task.ID != finding.ValidationTask || result.Status != "done" || request == nil || verdict == nil {
			continue
		}
		if strings.TrimSpace(request.Claim) == "" || strings.TrimSpace(request.Alternative) == "" || verdict.Verdict != "supported" || strings.TrimSpace(verdict.AlternativeResult) == "" || strings.TrimSpace(verdict.Reason) == "" {
			continue
		}
		logs := map[string]bool{}
		for _, execution := range result.Evidence {
			for _, ref := range execution.LogRefs {
				logs[ref] = true
			}
		}
		for _, verdictRef := range verdict.Evidence {
			if !logs[verdictRef] {
				continue
			}
			for _, findingRef := range finding.Evidence {
				if verdictRef == findingRef {
					return result
				}
			}
		}
	}
	return nil
}
