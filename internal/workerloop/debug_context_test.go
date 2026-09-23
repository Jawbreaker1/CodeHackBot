package workerloop

import (
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestDebugOmissionsChangeOnlyModelProjection(t *testing.T) {
	authoritative := ctxpacket.WorkerPacket{RunningSummary: "retained", StrategyGuidance: []ctxpacket.StrategyDocument{{Content: "guide"}}, CurrentStep: ctxpacket.Step{Objective: "scope"}}
	input := authoritative.Clone()
	applyDebugOmissions(&input, []string{"strategy_guidance", "running_summary", "current_step", "behavior_frame"})
	if len(input.StrategyGuidance) != 0 || input.RunningSummary != "" {
		t.Fatal("optional sections remain in debug projection")
	}
	if input.CurrentStep.Objective != "scope" || authoritative.RunningSummary != "retained" || len(authoritative.StrategyGuidance) != 1 {
		t.Fatal("debug omission changed safety or authoritative context")
	}
}
