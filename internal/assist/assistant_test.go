package assist

import (
	"context"
	"errors"
	"os/exec"
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
}

func (s *scriptedClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
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
	if suggestion.Type != "command" {
		t.Fatalf("expected command, got %s", suggestion.Type)
	}
	if suggestion.Command != "list_dir" {
		t.Fatalf("expected list_dir command, got %q", suggestion.Command)
	}
}

func TestFallbackAssistantLocalFileGoalWithPathUsesReadFile(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "Read ./artifacts/secret.txt and summarize it.",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "command" || suggestion.Command != "read_file" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if len(suggestion.Args) == 0 || suggestion.Args[0] != "artifacts/secret.txt" {
		t.Fatalf("unexpected args: %v", suggestion.Args)
	}
}

func TestFallbackAssistantWebGoalUsesBrowse(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "Investigate www.systemverification.com from a security perspective",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "command" || suggestion.Command != "browse" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if len(suggestion.Args) == 0 || !strings.Contains(suggestion.Args[0], "systemverification.com") {
		t.Fatalf("unexpected browse args: %v", suggestion.Args)
	}
}

func TestFallbackAssistantReportGoalUsesReportCommand(t *testing.T) {
	suggestion, err := (FallbackAssistant{}).Suggest(context.Background(), Input{
		Goal: "Create a security_report.md in this folder using OWASP format.",
	})
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if suggestion.Type != "command" || suggestion.Command != "report" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
	if len(suggestion.Args) == 0 || suggestion.Args[0] != "security_report.md" {
		t.Fatalf("unexpected report args: %+v", suggestion.Args)
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

func TestAssistSystemPromptAllowsCompleteInExecuteStepMode(t *testing.T) {
	if !strings.Contains(assistSystemPrompt, "return type=complete immediately when the goal has been satisfied") {
		t.Fatalf("execute-step completion guidance missing from system prompt")
	}
}
