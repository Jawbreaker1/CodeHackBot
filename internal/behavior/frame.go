package behavior

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultSystemPrompt = `You are BirdHackBot, an LLM-led security testing agent for authorized lab environments.
Follow repository rules, stay within scope, preserve evidence, and prefer clear reproducible results.`

// Frame is the fixed behavior input used as part of the active context packet.
type Frame struct {
	SystemPrompt string
	AgentsPath   string
	AgentsText   string
	RuntimeMode  string
	Parameters   map[string]string
}

// Load constructs the behavior frame from repo-local sources.
func Load(repoRoot, runtimeMode string, parameters map[string]string) (Frame, error) {
	agentsPath := filepath.Join(repoRoot, "AGENTS.md")
	agentsBytes, err := os.ReadFile(agentsPath)
	if err != nil {
		return Frame{}, fmt.Errorf("read AGENTS.md: %w", err)
	}

	frame := Frame{
		SystemPrompt: defaultSystemPrompt,
		AgentsPath:   agentsPath,
		AgentsText:   strings.TrimSpace(string(agentsBytes)),
		RuntimeMode:  strings.TrimSpace(runtimeMode),
		Parameters:   cloneMap(parameters),
	}
	if frame.RuntimeMode == "" {
		frame.RuntimeMode = "worker"
	}
	return frame, nil
}

// PromptText renders the stable behavior-frame text for context construction.
func (f Frame) PromptText() string {
	var b strings.Builder
	b.WriteString("System prompt:\n")
	b.WriteString(strings.TrimSpace(f.SystemPrompt))
	b.WriteString("\n\n")
	b.WriteString("AGENTS.md:\n")
	b.WriteString(strings.TrimSpace(f.AgentsText))
	b.WriteString("\n\n")
	b.WriteString("Runtime mode:\n")
	b.WriteString(strings.TrimSpace(f.RuntimeMode))

	keys := make([]string, 0, len(f.Parameters))
	for k := range f.Parameters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		b.WriteString("\n\nBehavior parameters:\n")
		for _, k := range keys {
			b.WriteString("- ")
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(f.Parameters[k])
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
