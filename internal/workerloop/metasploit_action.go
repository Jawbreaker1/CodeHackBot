package workerloop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

// validateMetasploitLookup checks inputs before they become console commands.
// The lookup never parses console output or runs a module against a target.
func validateMetasploitLookup(r Response) error {
	if r.Command != "" || len(r.Args) != 0 || r.UseShell || r.Target != "" || r.Impact != "" || r.Risk != "" || len(r.Artifacts) != 0 {
		return fmt.Errorf("metasploit_lookup accepts only cve or module, without execution fields")
	}
	if r.CVE != "" {
		parts := strings.Split(strings.ToUpper(strings.TrimSpace(r.CVE)), "-")
		if len(parts) != 3 || parts[0] != "CVE" || len(parts[1]) != 4 || len(parts[2]) < 4 || !allDecimal(parts[1]) || !allDecimal(parts[2]) {
			return fmt.Errorf("metasploit_lookup cve must be a CVE identifier such as CVE-2021-44228")
		}
		return nil
	}
	module := strings.TrimSpace(r.Module)
	if module == "" || !strings.Contains(module, "/") {
		return fmt.Errorf("metasploit_lookup module must be a full module path")
	}
	for _, part := range strings.Split(module, "/") {
		if part == "" || strings.IndexFunc(part, func(ch rune) bool {
			return !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-')
		}) >= 0 {
			return fmt.Errorf("metasploit_lookup module contains unsupported characters")
		}
	}
	return nil
}

func allDecimal(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// prepareMetasploitLookup maps read-only catalog inspection onto the ordinary
// approval, cancellation, and evidence path. The task's runtime-owned log
// directory is its config root, away from untrusted worker workspace files.
func prepareMetasploitLookup(r Response, cwd, logDir string) (Response, *ctxpacket.ExecutionResult) {
	if err := validateMetasploitLookup(r); err != nil {
		return r, validationFailure("metasploit_lookup", err.Error(), "invalid_action", "invalid_action")
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return r, validationFailure("metasploit_lookup", "worker workspace could not be resolved", "invalid_action", "invalid_action")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return r, validationFailure("metasploit_lookup", "worker workspace is unavailable", "invalid_action", "invalid_action")
	}
	configRoot, err := filepath.Abs(logDir)
	if err != nil || strings.TrimSpace(logDir) == "" {
		return r, validationFailure("metasploit_lookup", "task evidence directory could not be resolved", "invalid_action", "invalid_action")
	}
	if _, err := exec.LookPath("msfconsole"); err != nil {
		return r, validationFailure("metasploit_lookup", "Metasploit Framework is unavailable on this host", "not_executable", "not_executable")
	}
	if _, err := exec.LookPath("env"); err != nil {
		return r, validationFailure("metasploit_lookup", "environment launcher is unavailable", "not_executable", "not_executable")
	}
	consoleCommand := ""
	if r.CVE != "" {
		r.CVE = strings.ToUpper(strings.TrimSpace(r.CVE))
		consoleCommand = "search cve:" + r.CVE + "; exit -y"
		r.Summary = "Search installed Metasploit modules for " + r.CVE
	} else {
		r.Module = strings.TrimSpace(r.Module)
		consoleCommand = "info " + r.Module + "; exit -y"
		r.Summary = "Inspect installed Metasploit module " + r.Module
	}
	r.Command = "env"
	r.Args = []string{"MSF_CFGROOT_CONFIG=" + configRoot, "msfconsole", "-q", "-x", consoleCommand}
	r.Target = "local Metasploit module catalog"
	r.Impact = "Reads installed module metadata and writes task-local Metasploit support files; no assessment target is contacted."
	r.Risk = "low"
	return r, nil
}
