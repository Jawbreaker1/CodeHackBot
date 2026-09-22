package webapp

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func TestAnalysisPreservesLatestUnresolvedGap(t *testing.T) {
	state := assessment.State{Status: "incomplete", Plans: []assessment.Decision{
		{Gaps: []string{"No evidence yet"}},
		{Gaps: []string{"One role still needs validation"}},
	}}
	view := buildAnalysis("fixture", "lab", state, nil)
	if len(view.Gaps) != 1 || view.Gaps[0] != "One role still needs validation" || view.Conclusion != "" {
		t.Fatalf("latest unresolved gap not preserved: %+v", view)
	}
}
