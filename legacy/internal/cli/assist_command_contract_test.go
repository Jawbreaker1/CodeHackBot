package cli

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestNormalizeAssistCommandContractSplitsInlineCommand(t *testing.T) {
	command, args, err := normalizeAssistCommandContract("which zip2john", nil)
	if err != nil {
		t.Fatalf("normalize contract: %v", err)
	}
	if command != "which" {
		t.Fatalf("unexpected command: %q", command)
	}
	if len(args) != 1 || args[0] != "zip2john" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestNormalizeAssistCommandContractMergesInlineAndExplicitArgs(t *testing.T) {
	command, args, err := normalizeAssistCommandContract("nmap -sn", []string{"192.168.50.1"})
	if err != nil {
		t.Fatalf("normalize contract: %v", err)
	}
	if command != "nmap" {
		t.Fatalf("unexpected command: %q", command)
	}
	if len(args) != 2 || args[0] != "-sn" || args[1] != "192.168.50.1" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestNormalizeAssistCommandContractRejectsProseListLine(t *testing.T) {
	_, _, err := normalizeAssistCommandContract("1. First check if there are any other scanning tools available", nil)
	if err == nil {
		t.Fatalf("expected prose command rejection")
	}
	if !strings.Contains(err.Error(), "non-executable prose") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeAssistCommandContractRejectsTypePrefixProse(t *testing.T) {
	_, _, err := normalizeAssistCommandContract("type:", []string{"should", "be", "\"tool\""})
	if err == nil {
		t.Fatalf("expected prose command rejection")
	}
	if !strings.Contains(err.Error(), "non-executable prose") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeAssistCommandContractRejectsReservedContractToken(t *testing.T) {
	_, _, err := normalizeAssistCommandContract("evidence_refs", []string{"pointing", "to", "logs"})
	if err == nil {
		t.Fatalf("expected reserved contract token rejection")
	}
	if !strings.Contains(err.Error(), "non-executable prose") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeAssistCommandContractRejectsNarrativeCommandLine(t *testing.T) {
	_, _, err := normalizeAssistCommandContract("zip2john was run and created sessions/demo/zip.hash", nil)
	if err == nil {
		t.Fatalf("expected prose command rejection")
	}
	if !strings.Contains(err.Error(), "non-executable prose") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeAssistCommandContractRejectsUnwrappedShellOperators(t *testing.T) {
	_, _, err := normalizeAssistCommandContract("zip2john", []string{"./secret.zip", ">", "zip.hash"})
	if err == nil {
		t.Fatalf("expected shell operator rejection")
	}
	if !strings.Contains(err.Error(), "shell operators require explicit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeAssistCommandContractAllowsWrappedShellOperators(t *testing.T) {
	command, args, err := normalizeAssistCommandContract("bash", []string{"-lc", "zip2john ./secret.zip > zip.hash"})
	if err != nil {
		t.Fatalf("normalize contract: %v", err)
	}
	if command != "bash" {
		t.Fatalf("unexpected command: %q", command)
	}
	if len(args) != 2 || args[0] != "-lc" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestExecuteAssistSuggestionRejectsProseCommandContract(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-contract", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.executeAssistSuggestion(assist.Suggestion{
		Type:    "command",
		Command: "1. First check if nmap is installed",
	}, true)
	if err == nil {
		t.Fatalf("expected command contract rejection")
	}
	if !strings.Contains(err.Error(), "non-executable prose command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteAssistSuggestionRejectsInteractiveWriteFileInAssistMode(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-write-contract", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.executeAssistSuggestion(assist.Suggestion{
		Type:    "command",
		Command: "write_file",
		Args:    []string{"report.md"},
	}, false)
	if err == nil {
		t.Fatalf("expected write_file contract rejection")
	}
	if !strings.Contains(err.Error(), "write_file requires path and content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteAssistSuggestionRequiresPivotDecisionForTargetChange(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-target-contract", "", "")
	r.assistRuntime.Goal = "Scan internal network 192.168.50.0/24 and verify Johans-iphone (expected at 192.168.50.185)."
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.executeAssistSuggestion(assist.Suggestion{
		Type:     "command",
		Decision: "retry_modified",
		Command:  "nmap",
		Args:     []string{"-sV", "192.168.50.31"},
		Summary:  "scan candidate iphone host",
	}, true)
	if err == nil {
		t.Fatalf("expected target pivot contract rejection")
	}
	if !strings.Contains(err.Error(), "decision=pivot_strategy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteAssistSuggestionAllowsPivotDecisionForTargetChange(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-target-contract-pivot", "", "")
	r.assistRuntime.Goal = "Scan internal network 192.168.50.0/24 and verify Johans-iphone (expected at 192.168.50.185)."
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.executeAssistSuggestion(assist.Suggestion{
		Type:     "command",
		Decision: "pivot_strategy",
		Command:  "nmap",
		Args:     []string{"-sV", "192.168.50.31"},
		Summary:  "original target did not respond; pivot to discovered iphone host",
	}, true)
	if err != nil {
		t.Fatalf("expected pivot_strategy command to pass, got %v", err)
	}
}

func TestExecuteAssistSuggestionAllowsDiscoveryCIDRContainingPrimaryTarget(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-target-contract-cidr", "", "")
	r.assistRuntime.Goal = "Scan internal network 192.168.50.0/24 and verify Johans-iphone (expected at 192.168.50.185)."
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.executeAssistSuggestion(assist.Suggestion{
		Type:     "command",
		Decision: "retry_modified",
		Command:  "nmap",
		Args:     []string{"-sn", "192.168.50.0/24"},
		Summary:  "lightweight host discovery before host-focused validation",
	}, true)
	if err != nil {
		t.Fatalf("expected cidr discovery command to pass, got %v", err)
	}
}

func TestExecuteAssistSuggestionBlocksInteractiveUnzipPrompt(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-interactive-unzip", "", "")
	r.assistRuntime.Goal = "extract secret.zip password"
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	if hasPasswordArg([]string{"-p", "secret.zip"}) {
		t.Fatalf("expected hasPasswordArg false for -p without password")
	}
	if reason, blocked := assistInteractiveCommandReason("unzip", []string{"-p", "secret.zip"}); !blocked {
		t.Fatalf("expected direct guard block, got blocked=%v reason=%q args=%v", blocked, reason, []string{"-p", "secret.zip"})
	}

	err := r.executeAssistSuggestion(assist.Suggestion{
		Type:     "command",
		Decision: "retry_modified",
		Command:  "unzip",
		Args:     []string{"-p", "secret.zip"},
		Summary:  "try reading encrypted content",
	}, true)
	if err == nil {
		t.Fatalf("expected interactive unzip to be blocked")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "interactive command blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteAssistSuggestionAllowsNonInteractiveUnzipWithPassword(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-noninteractive-unzip", "", "")
	r.assistRuntime.Goal = "extract secret.zip password"
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.executeAssistSuggestion(assist.Suggestion{
		Type:     "command",
		Decision: "retry_modified",
		Command:  "unzip",
		Args:     []string{"-P", "password123", "-p", "secret.zip"},
		Summary:  "non-interactive extraction attempt",
	}, true)
	if err != nil {
		t.Fatalf("expected non-interactive unzip to pass contract, got %v", err)
	}
}

func TestExecuteAssistSuggestionBlocksInteractiveShellCommandInScript(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-interactive-shell-script", "", "")
	r.assistRuntime.Goal = "collect file output"
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	err := r.executeAssistSuggestion(assist.Suggestion{
		Type:     "command",
		Decision: "retry_modified",
		Command:  "bash",
		Args:     []string{"-lc", "echo test; unzip -p secret.zip"},
		Summary:  "scripted extraction",
	}, true)
	if err == nil {
		t.Fatalf("expected interactive shell script to be blocked")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "interactive command blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}
