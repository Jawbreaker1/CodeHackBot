package assist

import "testing"

func TestExtractLikelyPathRecognizesRelativeArtifacts(t *testing.T) {
	t.Parallel()

	got := extractLikelyPath("please inspect ./artifacts/recon.log and summarize")
	if got != "artifacts/recon.log" && got != "./artifacts/recon.log" {
		t.Fatalf("unexpected extracted path: %q", got)
	}
}

func TestIsReportGoalRequiresReportKeywordAndActionHint(t *testing.T) {
	t.Parallel()

	if isReportGoal("tell me about owasp") {
		t.Fatalf("expected non-report intent without report keyword")
	}
	if !isReportGoal("generate owasp report for this run") {
		t.Fatalf("expected report intent when report keyword and action hint exist")
	}
}
