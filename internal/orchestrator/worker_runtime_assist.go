package orchestrator

import (
	"regexp"
	"time"
)

const (
	workerLLMBaseURLEnv       = "BIRDHACKBOT_LLM_BASE_URL"
	workerLLMModelEnv         = "BIRDHACKBOT_LLM_MODEL"
	workerLLMAPIKeyEnv        = "BIRDHACKBOT_LLM_API_KEY"
	workerLLMTimeoutSeconds   = "BIRDHACKBOT_LLM_TIMEOUT_SECONDS"
	workerAssistModeEnv       = "BIRDHACKBOT_WORKER_ASSIST_MODE"
	workerConfigPathEnv       = "BIRDHACKBOT_CONFIG_PATH"
	workerAssistObsLimit      = 12
	workerAssistOutputLimit   = 24_000
	workerAssistReadMaxBytes  = 64_000
	workerAssistWriteMaxBytes = 512_000
	workerAssistBrowseMaxBody = 120_000
	workerAssistLoopMaxRepeat = 3
	workerAssistLLMCallMax    = 90 * time.Second
	workerAssistLLMCallMin    = 15 * time.Second
	workerAssistBudgetReserve = 10 * time.Second
	workerAssistMinTurns      = 16
	workerAssistTurnFactor    = 4
	workerAssistMaxLoopBlocks = 6
	workerAssistMaxRecoveries = 4
)

var htmlTitlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

type workerToolResult struct {
	command string
	args    []string
	output  []byte
	runErr  error
}
