package assist

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type fakeClient struct {
	content string
	err     error
}

func (f fakeClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{Content: f.content}, nil
}

type scriptedClient struct {
	contents []string
	errs     []error
	calls    int
	reqs     []llm.ChatRequest
}

func (s *scriptedClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	s.reqs = append(s.reqs, req)
	idx := s.calls
	s.calls++
	if idx < len(s.errs) && s.errs[idx] != nil {
		return llm.ChatResponse{}, s.errs[idx]
	}
	if idx < len(s.contents) {
		return llm.ChatResponse{Content: s.contents[idx]}, nil
	}
	return llm.ChatResponse{Content: ""}, nil
}

func TestFallbackAssistantQuestion(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question, got %s", suggestion.Type)
	}
}

func TestFallbackAssistantConversationalGoal(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{Goal: "Hello what can you help me with?"})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "complete" {
		t.Fatalf("expected complete, got %s", suggestion.Type)
	}
	if suggestion.Final == "" {
		t.Fatalf("expected final response")
	}
}

func TestFallbackAssistantLocalFileGoalWithoutTargets(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "There is a password protected zip file in this folder. Crack it and show contents.",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question, got %s", suggestion.Type)
	}
	if strings.Contains(strings.ToLower(suggestion.Question), "list_dir") {
		t.Fatalf("fallback should not synthesize a command: %+v", suggestion)
	}
}

func TestFallbackAssistantLocalFileGoalWithPathDoesNotUseReadFile(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "Read ./artifacts/secret.txt and summarize it.",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if strings.Contains(strings.ToLower(suggestion.Question), "read_file") {
		t.Fatalf("fallback should not synthesize read_file: %+v", suggestion)
	}
}

func TestFallbackAssistantWebGoalDoesNotUseBrowse(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "Investigate www.systemverification.com from a security perspective",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if strings.Contains(strings.ToLower(suggestion.Question), "browse") {
		t.Fatalf("fallback should not synthesize browse: %+v", suggestion)
	}
}

func TestFallbackAssistantReportGoalDoesNotUseReportCommand(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "Create a security_report.md in this folder using OWASP format.",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if strings.Contains(strings.ToLower(suggestion.Question), "report command") {
		t.Fatalf("fallback should not synthesize report command: %+v", suggestion)
	}
}

func TestFallbackAssistantReportGoalWithEvidenceStillAsksQuestion(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal:      "Create an OWASP report for this run.",
		RecentLog: "# OWASP-Style Security Assessment Report\n\n## Executive Summary\n- Outcome: ...",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question, got %s", suggestion.Type)
	}
}

func TestFallbackAssistantReportGoalInRecoverModeAsksQuestion(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "Generate an OWASP report for this run.",
		Mode: "recover",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question, got %s", suggestion.Type)
	}
}

func TestFallbackAssistantNoTargetsAsksGenericTargetQuestion(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "continue",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question, got %s", suggestion.Type)
	}
	if !strings.Contains(strings.ToLower(suggestion.Question), "ip/hostname/url") {
		t.Fatalf("unexpected question: %q", suggestion.Question)
	}
}

func TestFallbackAssistantAdaptiveLocalRecoveryDoesNotDefaultToNmap(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal:    "Recover from execution_failure on task T-06. Failed command: unzip -P badpass /home/johan/CodeHackBot/secret.zip. Failure log: /home/johan/CodeHackBot/sessions/run-x/artifact/T-06/worker.log.",
		Targets: []string{"127.0.0.1"},
		Mode:    "recover",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question fallback, got %s", suggestion.Type)
	}
}

func TestFallbackAssistantRecoverDoesNotReadLatestEvidenceRef(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal:                "Recover from execution_failure on task T-03.",
		Targets:             []string{"192.168.50.1"},
		Mode:                "recover",
		LatestResultSummary: "command failed: nmap ... | output: host timeout",
		LatestEvidenceRefs:  []string{"/tmp/nmap_vuln_scan.xml"},
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question fallback, got %+v", suggestion)
	}
}

func TestFallbackAssistantRecoverDirectoryEvidenceDoesNotUseListDir(t *testing.T) {
	dir := t.TempDir()
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal:                "Recover from execution_failure on task T-03.",
		Mode:                "recover",
		LatestResultSummary: "command failed: read_file /tmp/workspace | is a directory",
		LatestEvidenceRefs:  []string{dir},
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question fallback, got %+v", suggestion)
	}
}

func TestFallbackAssistantRecoverNonexistentArtifactPathDoesNotUseReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.xml")
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal:                "Recover from execution_failure on task T-03.",
		Mode:                "recover",
		LatestResultSummary: "command ok: nmap ... -oX scan.xml",
		LatestEvidenceRefs:  []string{path},
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question fallback, got %+v", suggestion)
	}
}

func TestFallbackAssistantRecoverDoesNotTreatRouterFailureAsLocalWorkflow(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal:                "Recover from execution_failure on task T-01-port-scan. Failure reason: no_progress. Error: no progress reading worker log.",
		Targets:             []string{"192.168.50.1"},
		Mode:                "recover",
		LatestResultSummary: "command failed: nmap -sV 192.168.50.1 | output: host timeout",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("expected question fallback, got %+v", suggestion)
	}
}

func TestLLMAssistantParsesJSON(t *testing.T) {
	client := fakeClient{content: `{"type":"command","command":"echo","args":["hi"]}`}
	assistant := LLMAssistant{Client: client}
	suggestion, err := assistant.Suggest(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if suggestion.Type != "command" || suggestion.Command != "echo" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
}

func TestLLMAssistantParsesFencedJSON(t *testing.T) {
	client := fakeClient{content: "```json\n{\"type\":\"question\",\"question\":\"targets?\"}\n```"}
	assistant := LLMAssistant{Client: client}
	suggestion, err := assistant.Suggest(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if suggestion.Type != "question" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
}

func TestLLMAssistantParsesComplete(t *testing.T) {
	client := fakeClient{content: `{"type":"complete","final":"All done."}`}
	assistant := LLMAssistant{Client: client}
	suggestion, err := assistant.Suggest(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if suggestion.Type != "complete" || suggestion.Final != "All done." {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
}

func TestLLMAssistantParsesLooseStepsObject(t *testing.T) {
	client := fakeClient{content: `{"type":"plan","plan":"scan then summarize","steps":{"1":"scan hosts","2":"summarize findings"}}`}
	assistant := LLMAssistant{Client: client}
	suggestion, err := assistant.Suggest(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if suggestion.Type != "plan" {
		t.Fatalf("expected plan, got %s", suggestion.Type)
	}
	if len(suggestion.Steps) != 2 || suggestion.Steps[0] != "scan hosts" || suggestion.Steps[1] != "summarize findings" {
		t.Fatalf("unexpected steps: %#v", suggestion.Steps)
	}
}

func TestLLMAssistantParsesLooseArgsString(t *testing.T) {
	client := fakeClient{content: `{"type":"command","command":"nmap","args":"-sn 192.168.50.0/24"}`}
	assistant := LLMAssistant{Client: client}
	suggestion, err := assistant.Suggest(context.Background(), Input{SessionID: "s"})
	if err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if suggestion.Type != "command" || suggestion.Command != "nmap" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if len(suggestion.Args) != 2 || suggestion.Args[0] != "-sn" || suggestion.Args[1] != "192.168.50.0/24" {
		t.Fatalf("unexpected args: %#v", suggestion.Args)
	}
}

func TestLLMAssistantReturnsParseErrorWithRawContent(t *testing.T) {
	client := fakeClient{content: "I cannot help with that request."}
	assistant := LLMAssistant{Client: client}
	_, err := assistant.Suggest(context.Background(), Input{SessionID: "s"})
	if err == nil {
		t.Fatalf("expected parse error")
	}
	var parseErr SuggestionParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected SuggestionParseError, got %T (%v)", err, err)
	}
	if !strings.Contains(parseErr.Raw, "cannot help") {
		t.Fatalf("expected raw refusal to be preserved, got %q", parseErr.Raw)
	}
}

func TestAssistSystemPromptRequiresSourceValidationForVulnerabilities(t *testing.T) {
	prompt := strings.ToLower(assistSystemPrompt)
	if !strings.Contains(prompt, "vulnerability/cve claim") {
		t.Fatalf("expected vulnerability/CVE source validation guidance in system prompt")
	}
	if !strings.Contains(prompt, "never rely on model memory alone") {
		t.Fatalf("expected anti-hallucination guidance in system prompt")
	}
	if !strings.Contains(prompt, "msfconsole") {
		t.Fatalf("expected metasploit source guidance in system prompt")
	}
}

func TestLLMAssistantRepairRetryParsesSecondResponse(t *testing.T) {
	client := &scriptedClient{
		contents: []string{
			"I cannot help with that request.",
			`{"type":"command","command":"ls","args":["-la"]}`,
		},
	}
	assistant := LLMAssistant{Client: client}
	suggestion, err := assistant.Suggest(context.Background(), Input{SessionID: "s", Goal: "list files"})
	if err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if suggestion.Type != "command" || suggestion.Command != "ls" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if client.calls != 2 {
		t.Fatalf("expected 2 model calls, got %d", client.calls)
	}
}

func TestLLMAssistantSuggestMetadataIncludesRawResponses(t *testing.T) {
	client := &scriptedClient{
		contents: []string{
			"I cannot help with that request.",
			`{"type":"command","command":"ls","args":["-la"]}`,
		},
	}
	var gotMeta LLMSuggestMetadata
	assistant := LLMAssistant{
		Client: client,
		OnSuggestMeta: func(meta LLMSuggestMetadata) {
			gotMeta = meta
		},
	}
	if _, err := assistant.Suggest(context.Background(), Input{SessionID: "s"}); err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if !gotMeta.ParseRepairUsed {
		t.Fatalf("expected parse repair metadata to be true")
	}
	if !strings.Contains(gotMeta.PrimaryResponse, "cannot help") {
		t.Fatalf("expected primary response in metadata, got %q", gotMeta.PrimaryResponse)
	}
	if !strings.Contains(gotMeta.RepairResponse, `"type":"command"`) {
		t.Fatalf("expected repair response in metadata, got %q", gotMeta.RepairResponse)
	}
}

func TestLLMAssistantUsesConfiguredTemperaturesAndTokens(t *testing.T) {
	primaryTemp := float32(0.33)
	primaryTokens := 777
	repairTemp := float32(0.07)
	repairTokens := 222
	client := &scriptedClient{
		contents: []string{
			"I cannot help with that request.",
			`{"type":"command","command":"ls","args":["-la"]}`,
		},
	}
	assistant := LLMAssistant{
		Client:            client,
		Temperature:       &primaryTemp,
		MaxTokens:         &primaryTokens,
		RepairTemperature: &repairTemp,
		RepairMaxTokens:   &repairTokens,
	}
	if _, err := assistant.Suggest(context.Background(), Input{SessionID: "s"}); err != nil {
		t.Fatalf("suggest error: %v", err)
	}
	if len(client.reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(client.reqs))
	}
	if client.reqs[0].Temperature != primaryTemp || client.reqs[0].MaxTokens != primaryTokens {
		t.Fatalf("unexpected primary llm options: %+v", client.reqs[0])
	}
	if client.reqs[1].Temperature != repairTemp || client.reqs[1].MaxTokens != repairTokens {
		t.Fatalf("unexpected repair llm options: %+v", client.reqs[1])
	}
}

func TestChainedAssistantFallback(t *testing.T) {
	assistant := ChainedAssistant{
		Primary:  LLMAssistant{Client: fakeClient{err: errors.New("down")}},
		Fallback: FallbackAssistant{},
	}
	suggestion, err := assistant.Suggest(context.Background(), Input{})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type == "" {
		t.Fatalf("expected suggestion")
	}
}

func TestNormalizeSuggestionSplitsCommand(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "nmap -sV 10.0.0.1",
	})
	if suggestion.Command != "nmap" {
		t.Fatalf("expected command nmap, got %s", suggestion.Command)
	}
	if len(suggestion.Args) != 2 || suggestion.Args[0] != "-sV" {
		t.Fatalf("unexpected args: %v", suggestion.Args)
	}
}

func TestNormalizeSuggestionSplitsCommandWhenArgsAlreadyPresent(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "ls -la",
		Args:    []string{"."},
	})
	if suggestion.Command != "ls" {
		t.Fatalf("expected command ls, got %s", suggestion.Command)
	}
	if len(suggestion.Args) != 2 || suggestion.Args[0] != "-la" || suggestion.Args[1] != "." {
		t.Fatalf("unexpected args: %v", suggestion.Args)
	}
}

func TestNormalizeSuggestionAvoidsDuplicateInlineAndExplicitArgs(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "unzip -P telefo01 -o secret.zip",
		Args:    []string{"-P", "telefo01", "-o", "secret.zip"},
	})
	if suggestion.Command != "unzip" {
		t.Fatalf("expected command unzip, got %s", suggestion.Command)
	}
	if len(suggestion.Args) != 4 {
		t.Fatalf("expected 4 args, got %v", suggestion.Args)
	}
	want := []string{"-P", "telefo01", "-o", "secret.zip"}
	for i := range want {
		if suggestion.Args[i] != want[i] {
			t.Fatalf("unexpected args at %d: %v", i, suggestion.Args)
		}
	}
}

func TestNormalizeSuggestionCollapsesRepeatedOptionBlockInCommand(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "unzip -P telefo01 -o secret.zip -P telefo01 -o secret.zip",
	})
	if suggestion.Command != "unzip" {
		t.Fatalf("expected command unzip, got %s", suggestion.Command)
	}
	want := []string{"-P", "telefo01", "-o", "secret.zip"}
	if len(suggestion.Args) != len(want) {
		t.Fatalf("expected %d args, got %v", len(want), suggestion.Args)
	}
	for i := range want {
		if suggestion.Args[i] != want[i] {
			t.Fatalf("unexpected args at %d: %v", i, suggestion.Args)
		}
	}
}

func TestNormalizeSuggestionSplitsShellPrefixWhenArgsAlreadyPresent(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "bash -c",
		Args:    []string{"echo ok"},
	})
	if suggestion.Command != "bash" {
		t.Fatalf("expected command bash, got %s", suggestion.Command)
	}
	if len(suggestion.Args) != 2 || suggestion.Args[0] != "-c" || suggestion.Args[1] != "echo ok" {
		t.Fatalf("unexpected args: %v", suggestion.Args)
	}
}

func TestNormalizeSuggestionSplitsDashCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: ls may be unavailable")
	}
	if _, err := exec.LookPath("ls-la"); err == nil {
		t.Skip("ls-la exists on PATH; cannot validate split")
	}
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "ls-la",
	})
	if suggestion.Command != "ls" {
		t.Fatalf("expected command ls, got %s", suggestion.Command)
	}
	if len(suggestion.Args) != 1 || suggestion.Args[0] != "-la" {
		t.Fatalf("unexpected args: %v", suggestion.Args)
	}
}

func TestNormalizeSuggestionSplitsBashScriptPreservingQuotes(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "bash -lc 'echo hi | grep hi'",
	})
	if suggestion.Command != "bash" {
		t.Fatalf("expected command bash, got %s", suggestion.Command)
	}
	if len(suggestion.Args) != 2 {
		t.Fatalf("expected 2 args, got %v", suggestion.Args)
	}
	if suggestion.Args[0] != "-lc" {
		t.Fatalf("expected -lc, got %q", suggestion.Args[0])
	}
	if suggestion.Args[1] != "echo hi | grep hi" {
		t.Fatalf("unexpected script arg: %q", suggestion.Args[1])
	}
}

func TestNormalizeSuggestionMapsRecoverToCommand(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:    "recover",
		Command: "whois",
		Args:    []string{"example.com"},
	})
	if suggestion.Type != "command" {
		t.Fatalf("expected command, got %s", suggestion.Type)
	}
}

func TestNormalizeSuggestionMapsUnknownTypeByPayload(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type:     "analysis",
		Question: "Need target?",
	})
	if suggestion.Type != "question" {
		t.Fatalf("expected question, got %s", suggestion.Type)
	}
}

func TestNormalizeSuggestionConvertsToolWithoutFilesToCommand(t *testing.T) {
	suggestion := normalizeSuggestion(Suggestion{
		Type: "tool",
		Tool: &ToolSpec{
			Name: "report-runner",
			Run: ToolRun{
				Command: "report",
				Args:    []string{"owasp_report.md"},
			},
		},
		Summary: "Generate final report from collected evidence.",
	})
	if suggestion.Type != "command" {
		t.Fatalf("expected command, got %s", suggestion.Type)
	}
	if suggestion.Command != "report" {
		t.Fatalf("expected command report, got %q", suggestion.Command)
	}
	if suggestion.Tool != nil {
		t.Fatalf("expected tool to be cleared after normalization")
	}
	if len(suggestion.Args) != 1 || suggestion.Args[0] != "owasp_report.md" {
		t.Fatalf("unexpected args: %v", suggestion.Args)
	}
}

func TestExtractJSONFromChannelWrappedContent(t *testing.T) {
	raw := `<channel>final <constrain>json<message>{"type":"complete","final":"ok"}`
	got := extractJSON(raw)
	if got != `{"type":"complete","final":"ok"}` {
		t.Fatalf("unexpected json extraction: %q", got)
	}
}

func TestParseSimpleCommandIgnoresChannelEnvelope(t *testing.T) {
	raw := `<channel>final <constrain>json<message>{"type":"tool","tool":{"name":"x"}}`
	if got := parseSimpleCommand(raw); got != "" {
		t.Fatalf("expected no command, got %q", got)
	}
}

func TestParseSimpleCommandRejectsProseTypePrefix(t *testing.T) {
	raw := `type: should be "tool" based on previous responses`
	if got := parseSimpleCommand(raw); got != "" {
		t.Fatalf("expected no command, got %q", got)
	}
}

func TestParseSimpleCommandRejectsNumberedNarrativeLine(t *testing.T) {
	raw := `1. There's a secret.zip file in the working directory`
	if got := parseSimpleCommand(raw); got != "" {
		t.Fatalf("expected no command from numbered narrative line, got %q", got)
	}
}

func TestAssistSystemPromptAllowsCompleteInExecuteStepMode(t *testing.T) {
	if !strings.Contains(assistSystemPrompt, "return type=complete immediately when the goal has been satisfied") {
		t.Fatalf("execute-step completion guidance missing from system prompt")
	}
}
