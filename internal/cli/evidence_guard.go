package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

const unverifiedPrivilegeClaimMessage = "UNVERIFIED: I cannot prove admin/root access from session evidence. Provide command, raw output, exit code, and log path."

func (r *Runner) enforceEvidenceClaims(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return trimmed
	}
	if !containsPrivilegeClaim(trimmed) {
		return trimmed
	}
	if r.hasPrivilegeEvidenceInSession() {
		return trimmed
	}
	return unverifiedPrivilegeClaimMessage
}

func containsPrivilegeClaim(text string) bool {
	lower := strings.ToLower(text)
	phrases := []string{
		"gained admin access",
		"gained root access",
		"admin access",
		"root access",
		"privilege escalation",
		"elevated privileges",
		"compromised",
		"took over",
		"full control",
		"got shell",
		"meterpreter session",
		"command shell session",
	}
	for _, phrase := range phrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func (r *Runner) hasPrivilegeEvidenceInSession() bool {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return false
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return false
	}
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return false
	}
	for i := len(state.RecentObservations) - 1; i >= 0; i-- {
		obs := state.RecentObservations[i]
		if hasPrivilegeProofSnippet(obs.OutputExcerpt) {
			return true
		}
		if hasPrivilegeProofSnippet(obs.Error) {
			return true
		}
		if strings.TrimSpace(obs.LogPath) != "" {
			if hasPrivilegeProofInLog(obs.LogPath) {
				return true
			}
		}
	}
	return false
}

func hasPrivilegeProofSnippet(text string) bool {
	lower := strings.ToLower(text)
	markers := []string{
		"uid=0(",
		"whoami\nroot",
		"\nroot\n",
		"meterpreter session",
		"command shell session",
		"session opened",
		"permission denied",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasPrivilegeProofInLog(logPath string) bool {
	path := strings.TrimSpace(logPath)
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Clean(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	const maxBytes = 8192
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	return hasPrivilegeProofSnippet(string(data))
}
