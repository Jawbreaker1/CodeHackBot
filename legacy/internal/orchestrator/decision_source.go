package orchestrator

import "strings"

const (
	decisionSourceLLMDirect    = "llm_direct"
	decisionSourceLLMRepair    = "llm_repair"
	decisionSourceRuntimeAdapt = "runtime_adapt"
	decisionSourceStatic       = "static_fallback"
)

func normalizeDecisionSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case decisionSourceLLMDirect:
		return decisionSourceLLMDirect
	case decisionSourceLLMRepair:
		return decisionSourceLLMRepair
	case decisionSourceRuntimeAdapt:
		return decisionSourceRuntimeAdapt
	case decisionSourceStatic:
		return decisionSourceStatic
	default:
		return ""
	}
}

func assistDecisionSourceFromTurnMeta(turnMeta workerAssistantTurnMeta) string {
	if turnMeta.FallbackUsed {
		return decisionSourceStatic
	}
	if turnMeta.ParseRepairUsed {
		return decisionSourceLLMRepair
	}
	return decisionSourceLLMDirect
}
