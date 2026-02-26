package orchestrator

import (
	"regexp"
	"time"
)

const (
	workerLLMBaseURLEnv                  = "BIRDHACKBOT_LLM_BASE_URL"
	workerLLMModelEnv                    = "BIRDHACKBOT_LLM_MODEL"
	workerLLMAPIKeyEnv                   = "BIRDHACKBOT_LLM_API_KEY"
	workerLLMTimeoutSeconds              = "BIRDHACKBOT_LLM_TIMEOUT_SECONDS"
	workerAssistModeEnv                  = "BIRDHACKBOT_WORKER_ASSIST_MODE"
	workerAssistTraceLLMEnv              = "BIRDHACKBOT_WORKER_ASSIST_TRACE_LLM"
	workerConfigPathEnv                  = "BIRDHACKBOT_CONFIG_PATH"
	workerAssistObsLimit                 = 20
	workerAssistObsTokenBudget           = 1_800
	workerAssistObsMaxEntryChars         = 420
	workerAssistObsCompactionAnchorCap   = 5
	workerAssistObsCompactionErrMaxChars = 96
	workerAssistOutputLimit              = 24_000
	workerAssistReadMaxBytes             = 64_000
	workerAssistWriteMaxBytes            = 512_000
	workerAssistBrowseMaxBody            = 120_000
	workerAssistLoopMaxRepeat            = 3
	workerAssistLLMCallMax               = 90 * time.Second
	workerAssistLLMCallMin               = 15 * time.Second
	workerAssistBudgetReserve            = 10 * time.Second
	workerAssistMinTurns                 = 16
	workerAssistTurnFactor               = 4
	workerAssistMaxLoopBlocks            = 6
	workerAssistMaxRecoveries            = 4
	// Guard against long "tool churn" loops where the model keeps emitting tool
	// actions that do not converge toward completion.
	workerAssistMaxConsecutiveToolTurns        = 6
	workerAssistMaxConsecutiveRecoverToolTurns = 3
	workerAssistNoNewEvidenceResultRepeat      = 3
	workerAssistNoNewEvidenceToolCallCap       = 8
	workerAssistResultFingerprintBytes         = 512
	workerAssistSummaryRecoverStepCap          = 4
	workerAssistSummaryRecoverLoopCap          = 3
	workerAssistSummaryRecoverQuestionCap      = 2
	workerAssistTraceResponseMaxChars          = 12_000
)

var htmlTitlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

type workerToolResult struct {
	command string
	args    []string
	output  []byte
	runErr  error
}
