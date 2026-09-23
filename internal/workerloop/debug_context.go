package workerloop

import ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"

// applyDebugOmissions changes only the model projection. The authoritative
// packet and recorded evidence remain available for later turns and reports.
func applyDebugOmissions(packet *ctxpacket.WorkerPacket, sections []string) {
	for _, section := range sections {
		switch section {
		case "plan_history":
			packet.PlanHistory = nil
		case "recent_conversation":
			packet.RecentConversation = nil
		case "older_conversation_summary":
			packet.OlderConversationSummary = ""
		case "running_summary":
			packet.RunningSummary = ""
		case "relevant_recent_results":
			packet.RelevantRecentResults = nil
		case "memory_bank_retrievals":
			packet.MemoryBankRetrievals = nil
		case "strategy_guidance":
			packet.StrategyGuidance = nil
		case "capability_inputs":
			packet.CapabilityInputs = nil
		case "context_notes":
			packet.ContextNotes = nil
		}
	}
}
