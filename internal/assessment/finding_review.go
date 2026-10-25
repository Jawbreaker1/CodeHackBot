package assessment

import "strings"

// DisplayFindings applies the same evidence check used by formal reports to
// findings shown in chat and session views. The saved plan remains
// intact so reviewers can inspect the historical claim.
func DisplayFindings(state State) []Finding {
	findings, _ := reviewFindings(state)
	return findings
}

// reviewFindings keeps historical session state intact while preventing an
// older, unchallenged reproduction claim from being presented as confirmed in
// a newly rendered report.
func reviewFindings(state State) ([]Finding, bool) {
	findings := ReportableFindings(state)
	legacyClaims := false
	for i := range findings {
		if (findings[i].Status == "reproduced" || findings[i].Status == "confirmed_exposure") && (SupportedChallengeForFinding(state.Results, findings[i]) == nil || findings[i].Status == "confirmed_exposure" && !confirmedExposureRecord(state, findings[i])) {
			findings[i].Status = "candidate"
			legacyClaims = true
		}
	}
	return findings, legacyClaims
}

// reportFindingGroups keeps unconfirmed observations available for follow-up
// without presenting them as established vulnerabilities in formal reports.
func reportFindingGroups(state State) (verified, candidates []Finding, legacyClaims bool) {
	findings, legacyClaims := reviewFindings(state)
	for _, finding := range findings {
		if finding.Status == "reproduced" || finding.Status == "confirmed_exposure" {
			verified = append(verified, finding)
		} else {
			candidates = append(candidates, finding)
		}
	}
	return verified, candidates, legacyClaims
}

// ReportableFindings omits claims whose own completed validation check
// refuted them. The saved plan and verification evidence remain available
// for the test history, but the claim is not an actionable finding.
func ReportableFindings(state State) []Finding {
	var findings []Finding
	for _, finding := range CurrentFindings(state.Plans) {
		refuted := false
		if finding.ValidationTask != "" {
			for _, result := range state.Results {
				if result.Task.ID == finding.ValidationTask && result.Status == "done" && result.Task.Verification != nil && result.Verification != nil && result.Verification.Verdict == "refuted" {
					refuted = true
					break
				}
			}
		}
		if !refuted {
			findings = append(findings, finding)
		}
	}
	return findings
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
