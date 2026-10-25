package webapp

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func TestAnalysisPresentsConfirmedExposureWithoutClaimingExploit(t *testing.T) {
	claim := assessment.Finding{Title: "Affected installed server", Status: "confirmed_exposure", Severity: "high", Confidence: "high", AdvisoryAssessmentID: "verify:notice", CVEIDs: []string{"CVE-2026-1234"}, Impact: "The installed build is affected; exploit not tested", Remediation: []string{"Apply the vendor update"}}
	score, priority, _ := findingPriority(claim)
	if score == 0 || priority != "high" {
		t.Fatalf("confirmed exposure lost its risk priority: score=%d priority=%s", score, priority)
	}
	findings := prioritizeFindings([]analysisFindingInput{{SessionID: "fixture", Finding: claim}})
	risk := summarizeRisk(findings)
	if risk.Exposures != 1 || risk.Reproduced != 0 || risk.Candidates != 0 || risk.High != 1 || findings[0].AdvisoryAssessmentID != "verify:notice" {
		t.Fatalf("analysis conflated exposure and exploitation: risk=%+v findings=%+v", risk, findings)
	}
	if actions := nextActions(findings, nil, nil, nil); len(actions) != 1 || actions[0].Kind != "fix" {
		t.Fatalf("confirmed exposure did not produce an actionable fix: %+v", actions)
	}
	report, err := renderCustomerAnalysisReport(analysisView{Kind: "customer", ID: "fixture", SessionCount: 1, Risk: risk, Findings: findings}, assessment.OWASPReport)
	if err != nil || !strings.Contains(string(report), "confirmed software exposures without exploitation") || !strings.Contains(string(report), "Exploit status:** Not tested or reproduced") || strings.Contains(string(report), "## 3. Reproduced findings for review") {
		t.Fatalf("project report overstated or hid exposure: err=%v report=%s", err, report)
	}
}
