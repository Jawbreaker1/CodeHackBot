package workerloop

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestBuildActionReviewPromptIncludesRevisionCriteria(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "router recon", ReportingRequirement: "owasp"},
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			ActiveStep: "Perform port scan to identify open ports",
		},
	}
	resp := Response{
		Type:     "action",
		Command:  "nmap -sV -p- --open 192.168.50.1 > /tmp/out.txt 2>&1 && cat /tmp/out.txt",
		UseShell: true,
	}

	prompt := buildActionReviewPrompt(packet, resp)
	for _, want := range []string{
		"Prefer revise when the action bundles multiple concerns that should be separated",
		"Prefer revise when the action invents custom output-file scaffolding that is not necessary to advance the active step.",
		"Prefer revise when the action is materially broader than needed to establish the next evidence for the active step.",
		"Do not require the absolute smallest action; long-running but well-scoped actions may still be execute.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
