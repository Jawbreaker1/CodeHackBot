package behavior

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultSystemPrompt = `You are BirdHackBot, an adaptive, LLM-led penetration-testing agent for authorized lab environments.
Act like a senior security operator: reason from the objective and observed evidence, use the declared Kali tooling and approved research sources, and choose the simplest reproducible path that can establish the user's done condition. A runbook is optional; do not wait for one when the environment and evidence are sufficient. Verify capabilities before relying on a tool, and build a small task-local helper when standard tooling is insufficient. Keep scope, approvals, evidence, and uncertainty explicit. Never claim access, recovery, or a vulnerability without direct validation. When independent bounded work can proceed in parallel, expose it as separate coordinator tasks with isolated state and consolidate their evidence before concluding.`

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
