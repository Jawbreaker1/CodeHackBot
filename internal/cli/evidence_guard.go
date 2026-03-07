package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

const unverifiedPrivilegeClaimMessage = "UNVERIFIED: I cannot prove admin/root access from session evidence. Provide command, raw output, exit code, and log path."
const unverifiedVulnerabilityClaimMessage = "UNVERIFIED: Vulnerability/CVE claim is not source-validated from session evidence. Validate with tool/advisory output (e.g., msfconsole/searchsploit/nmap vuln scripts or captured advisory pages) and provide command, raw output, and log/artifact path."

var cveClaimPattern = regexp.MustCompile(`(?i)\bCVE[-_ ]?(\d{4})[-_](\d{4,})\b`)

func (r *Runner) enforceEvidenceClaims(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return trimmed
	}
	if containsPrivilegeClaim(trimmed) && !r.hasPrivilegeEvidenceInSession() {
		return unverifiedPrivilegeClaimMessage
	}
	if containsVulnerabilityClaim(trimmed) && !r.hasVulnerabilityEvidenceInSession(trimmed) {
		return unverifiedVulnerabilityClaimMessage
	}
	return trimmed
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

func containsVulnerabilityClaim(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	claimPhrases := []string{
		"is vulnerable",
		"appears vulnerable",
		"host appears vulnerable",
		"identified vulnerability",
		"identified vulnerabilities",
		"found vulnerability",
		"found vulnerabilities",
		"detected vulnerability",
		"detected vulnerabilities",
		"affected by cve",
		"confirmed cve",
		"mapped to cve",
		"exposed cve",
	}
	for _, phrase := range claimPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	if !cveClaimPattern.MatchString(text) {
		return false
	}
	return strings.Contains(lower, "found") ||
		strings.Contains(lower, "identified") ||
		strings.Contains(lower, "detected") ||
		strings.Contains(lower, "vulnerable") ||
		strings.Contains(lower, "affected") ||
		strings.Contains(lower, "exposed") ||
		strings.Contains(lower, "confirmed") ||
		strings.Contains(lower, "mapped")
}

func (r *Runner) hasVulnerabilityEvidenceInSession(claimText string) bool {
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
	claimedCVEs := extractClaimedCVEs(claimText)
	for i := len(state.RecentObservations) - 1; i >= 0; i-- {
		obs := state.RecentObservations[i]
		if !observationLooksLikeVulnSource(obs) {
			continue
		}
		logTail := readLogTail(resolveObservationLogPath(obs.LogPath, sessionDir), 8192)
		evidenceText := strings.TrimSpace(strings.Join([]string{obs.OutputExcerpt, obs.Error, logTail}, "\n"))
		if evidenceText == "" || !containsVulnerabilityProofSnippet(evidenceText) {
			continue
		}
		if len(claimedCVEs) == 0 {
			return true
		}
		upper := strings.ToUpper(evidenceText)
		for _, cve := range claimedCVEs {
			if strings.Contains(upper, cve) {
				return true
			}
		}
	}
	return false
}

func extractClaimedCVEs(text string) []string {
	matches := cveClaimPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		normalized := "CVE-" + match[1] + "-" + match[2]
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func observationLooksLikeVulnSource(obs memory.Observation) bool {
	cmdText := strings.ToLower(strings.TrimSpace(obs.Command + " " + strings.Join(obs.Args, " ")))
	if containsAnySubstring(cmdText,
		"nmap", "msfconsole", "metasploit", "searchsploit", "nikto", "nuclei",
		"cve", "advisory", "nvd.nist.gov", "exploit-db",
		"browse", "crawl", "read_file", "parse_links", "curl", "wget") {
		return true
	}
	logPath := strings.ToLower(strings.TrimSpace(obs.LogPath))
	return strings.Contains(logPath, "vuln") || strings.Contains(logPath, "cve")
}

func containsVulnerabilityProofSnippet(text string) bool {
	lower := strings.ToLower(text)
	if cveClaimPattern.MatchString(text) {
		return true
	}
	markers := []string{
		"vulnerable",
		"host appears vulnerable",
		"state: vulnerable",
		"metasploit module",
		"searchsploit",
		"nvd.nist.gov",
		"vendor advisory",
		"security advisory",
		"exploit-db",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func resolveObservationLogPath(logPath string, sessionDir string) string {
	path := strings.TrimSpace(logPath)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	cleanRel := filepath.Clean(path)
	sessionPath := filepath.Join(sessionDir, cleanRel)
	if _, err := os.Stat(sessionPath); err == nil {
		return sessionPath
	}
	return cleanRel
}

func readLogTail(path string, maxBytes int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	return strings.TrimSpace(string(data))
}

func containsAnySubstring(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
