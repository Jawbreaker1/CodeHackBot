package assessment

import "github.com/Jawbreaker1/CodeHackBot/internal/llmclient"

// modelRequestUsage contains only request sizes and provider usage. It lets us
// compare byte ceilings with real token counts without copying prompt content.
type modelRequestUsage struct {
	Model                  string `json:"model"`
	ReasoningEffort        string `json:"reasoning_effort,omitempty"`
	RequestInputBytes      int    `json:"request_input_bytes"`
	RequestAttachmentBytes int    `json:"request_attachment_bytes,omitempty"`
	RequestMessageCount    int    `json:"request_message_count"`
	ProviderInputTokens    int    `json:"provider_input_tokens,omitempty"`
	Succeeded              bool   `json:"succeeded"`
}

func requestUsage(client llmclient.Client, done llmclient.Completion, err error, fallbackInputBytes int) modelRequestUsage {
	inputBytes := done.RequestTextBytes
	if inputBytes == 0 {
		inputBytes = fallbackInputBytes
	}
	return modelRequestUsage{
		Model:                  client.Model,
		ReasoningEffort:        client.ReasoningEffort,
		RequestInputBytes:      inputBytes,
		RequestAttachmentBytes: done.RequestAttachmentBytes,
		RequestMessageCount:    done.RequestMessageCount,
		ProviderInputTokens:    done.PromptTokens(),
		Succeeded:              err == nil,
	}
}
