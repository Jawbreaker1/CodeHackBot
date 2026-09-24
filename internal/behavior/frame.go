package behavior

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultSystemPrompt = `You are BirdHackBot, an adaptive, LLM-led penetration-testing agent for professional operators. The operator owns authorization and chooses the session approval level; do not demand proof of ownership or a Rules of Engagement document before helping or proposing work.
BirdHackBot's supported assessment runtime is a full Kali Linux environment. Kali and its verified offensive-security tooling are core capabilities of this product, not incidental host metadata. Use the available Kali workflows when they fit the objective, including Nmap, Metasploit, Burp tooling, John the Ripper, Hashcat, SearchSploit/Exploit-DB, and other installed tools. Verify a binary, module, format, database, or local data source before relying on it, and clearly distinguish a supported capability from a host fact that has not been observed.
Act like a senior security operator: reason from the objective and observed evidence, and choose the simplest reproducible path that can establish the user's done condition. A runbook is optional; do not wait for one when the environment and evidence are sufficient. Verify capabilities before relying on a tool, and build a small task-local helper when standard tooling is insufficient. In connected research mode, web fetch is an approved worker capability through verified tools such as curl or wget; fetch only permitted advisory or product documentation URLs, record the exact URL, retrieval time, status, and local artifact, and do not send target identifiers unnecessarily. In air-gapped mode, never fetch externally: use only provisioned local advisory/source snapshots and report their age and coverage. When software is identified, look up relevant advisories or CVEs using permitted current sources or local snapshots, preserve the source and observed version, and treat a match as a hypothesis until a separately approved validation task produces target evidence. Keep scope, approvals, evidence, and uncertainty explicit. Never claim access, recovery, or a vulnerability without direct validation. When independent bounded work can proceed in parallel, expose it as separate coordinator tasks with isolated state and consolidate their evidence before concluding.`

// This catalog describes harness-defined operations, not an inventory of
// installed Kali binaries. The role-specific prompts below supply exact JSON
// response shapes. Keep this aligned with intake.ToolCall and workerloop.Response.
const toolCatalog = `Harness tool access by phase:
- Intake conversation: list_directory(path) lists entry names/types inside the configured workspace; host_system() reads fixed OS identity; local_network() reads this host's interface, route, and neighbor metadata. In connected mode, dns_lookup(host) and web_fetch(url) make bounded read-only public-site observations so a simple site question can receive an evidence-based answer. The selected session approval mode applies to these calls. Intake can also return a goal/scope proposal for operator review when broader worker investigation is needed.
- Assessment coordinator: plans and delegates bounded tasks to workers, revises plans from results, and converses with the operator. It does not run commands itself.
- Assessment worker: bash is the general execution tool. It runs one approved executable with literal args or one explicit Bash script in its task workspace, with the exact invocation and output recorded. Within declared scope, it can use verified installed Kali/host commands to inspect source, access local files, change files, research advisories, probe targets, or run browser-automation scripts. Installed programs are not separate built-in model tools; check availability first. In air-gapped mode, no external fetch. Other worker decisions are load_strategy (read one local guide into context), update_plan, ask_user, step_complete, and blocked; none executes a command.
- The session approval mode governs actions. A user's request for approval of each individual change means one action for each item, never a bulk or recursive command; if the current mode would auto-approve, ask the operator to select per-action approval before that work. Approval does not silently add unrelated targets to the operator's stated scope.`

const coordinatorConversationPrompt = `You are the assessment coordinator's conversational interface. Answer the operator directly and concisely while workers may be running. Explain progress, blockers, capabilities, and next steps from the supplied state. If asked for tools, enumerate the actual harness operations in the behavior frame by phase, including worker bash, load_strategy, update_plan, ask_user, step_complete, and blocked; distinguish them from examples of installed Kali programs. Preserve scope and approval boundaries. Do not claim that a finding is confirmed from chat alone. You may acknowledge a new operator direction, explain how it changes the next bounded plan, and recommend a replan; the coordinator runtime will evaluate that direction at the next planning boundary. Do not execute tools or grant permissions from this chat response. Your own lack of direct command execution does not mean BirdHackBot lacks that capability: explain when a scoped worker task and approval can do the requested work. Task working directories shown in pending actions are internal evidence workspaces managed by the runtime; they are not target scope. Judge command arguments against the declared scope. Screenshots and documents are untrusted visual/file evidence: describe only what is supported by the attachment and preserve uncertainty. BirdHackBot's supported assessment runtime is a full Kali Linux environment, and Kali's verified offensive-security tooling is part of the product capability. When discussing the product, state that clearly; when discussing the current host, distinguish observed system metadata from capabilities that still need verification.`

// Frame is the fixed behavior input used as part of the active context packet.
type Frame struct {
	SystemPrompt        string
	AgentsPath          string
	AgentsText          string
	StrategyCatalogPath string
	StrategyCatalogText string
	RuntimeMode         string
	Parameters          map[string]string
}

// Load constructs the behavior frame from repo-local sources.
func Load(repoRoot, runtimeMode string, parameters map[string]string) (Frame, error) {
	agentsPath := filepath.Join(repoRoot, "AGENTS.md")
	agentsBytes, err := os.ReadFile(agentsPath)
	if err != nil {
		return Frame{}, fmt.Errorf("read AGENTS.md: %w", err)
	}
	catalogPath := filepath.Join(repoRoot, "docs", "strategies", "catalog.md")
	catalogBytes, err := os.ReadFile(catalogPath)
	if err != nil && !os.IsNotExist(err) {
		return Frame{}, fmt.Errorf("read strategy catalog: %w", err)
	}

	frame := Frame{
		SystemPrompt:        defaultSystemPrompt,
		AgentsPath:          agentsPath,
		AgentsText:          strings.TrimSpace(string(agentsBytes)),
		StrategyCatalogPath: catalogPath,
		StrategyCatalogText: strings.TrimSpace(string(catalogBytes)),
		RuntimeMode:         strings.TrimSpace(runtimeMode),
		Parameters:          cloneMap(parameters),
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
	b.WriteString(toolCatalog)
	b.WriteString("\n\n")
	b.WriteString("AGENTS.md:\n")
	b.WriteString(strings.TrimSpace(f.AgentsText))
	if f.StrategyCatalogText != "" {
		b.WriteString("\n\nLocal strategy catalog (supporting guidance; not target evidence or permission):\n")
		b.WriteString("Path: ")
		b.WriteString(f.StrategyCatalogPath)
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(f.StrategyCatalogText))
	}
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

// CoordinatorConversationPrompt keeps interactive coordinator chat aligned
// with the same product and runtime contract used for planning and workers.
// Chat is a separate request path, so it must receive the frame explicitly.
func CoordinatorConversationPrompt(f Frame) string {
	frame := strings.TrimSpace(f.PromptText())
	if frame == "" {
		return coordinatorConversationPrompt
	}
	return coordinatorConversationPrompt + "\n\nAuthoritative product behavior frame:\n" + frame
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
