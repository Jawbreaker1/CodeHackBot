package report

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed template.md
var defaultTemplate string

//go:embed template_owasp.md
var owaspTemplate string

//go:embed template_nis2.md
var nis2Template string

//go:embed template_internal.md
var internalTemplate string

type Profile string

const (
	ProfileStandard Profile = "standard"
	ProfileOWASP    Profile = "owasp"
	ProfileNIS2     Profile = "nis2"
	ProfileInternal Profile = "internal"
)

type Info struct {
	Date         string
	Scope        []string
	Findings     []string
	SessionID    string
	Ledger       string
	Summary      string
	KnownFacts   string
	Focus        string
	Plan         string
	Inventory    string
	Observations string
}

var (
	ipv4Pattern        = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	openPortPattern    = regexp.MustCompile(`\b(\d{1,5})/(tcp|udp)\s+open\b`)
	nmapHostLinePrefix = "Nmap scan report for "
)

func DefaultTemplatePath() string {
	return filepath.Join("internal", "report", "template.md")
}

func Generate(templatePath, outPath string, info Info) error {
	return GenerateWithProfile(string(ProfileStandard), templatePath, outPath, info)
}

func GenerateWithProfile(profile, templatePath, outPath string, info Info) error {
	resolvedProfile, err := NormalizeProfile(profile)
	if err != nil {
		return err
	}
	content, err := loadTemplateContent(resolvedProfile, templatePath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(outPath) == "" {
		return fmt.Errorf("output path is required")
	}
	return writeReportContent(content, outPath, info)
}

func AvailableProfiles() []string {
	return []string{string(ProfileStandard), string(ProfileOWASP), string(ProfileNIS2), string(ProfileInternal)}
}

func NormalizeProfile(profile string) (string, error) {
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile == "" {
		return string(ProfileStandard), nil
	}
	switch profile {
	case "default", "std", "base", string(ProfileStandard):
		return string(ProfileStandard), nil
	case "owasp", "wstg", "asvs":
		return string(ProfileOWASP), nil
	case "nis2", "n2":
		return string(ProfileNIS2), nil
	case "internal", "lab", "internal_lab":
		return string(ProfileInternal), nil
	default:
		return "", fmt.Errorf("unsupported report profile %q (supported: %s)", profile, strings.Join(AvailableProfiles(), ", "))
	}
}

func loadTemplateContent(profile, templatePath string) (string, error) {
	content := ""
	if strings.TrimSpace(templatePath) == "" {
		switch profile {
		case string(ProfileStandard):
			content = defaultTemplate
		case string(ProfileOWASP):
			content = owaspTemplate
		case string(ProfileNIS2):
			content = nis2Template
		case string(ProfileInternal):
			content = internalTemplate
		default:
			content = defaultTemplate
		}
	} else {
		data, err := os.ReadFile(templatePath)
		if err != nil {
			return "", fmt.Errorf("read template: %w", err)
		}
		content = string(data)
	}
	return content, nil
}

func writeReportContent(content, outPath string, info Info) error {
	date := info.Date
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	scopeText := strings.Join(compactValues(info.Scope), ", ")
	if scopeText == "" {
		scopeText = "(not provided)"
	}
	highLevel := deriveHighLevelFindings(info)
	content = strings.ReplaceAll(content, "Date:", fmt.Sprintf("Date: %s", date))
	content = strings.ReplaceAll(content, "Scope:", fmt.Sprintf("Scope: %s", scopeText))
	content = strings.ReplaceAll(content, "High-level findings:", fmt.Sprintf("High-level findings: %s", strings.Join(highLevel, "; ")))
	content = strings.ReplaceAll(content, "- Targets:", fmt.Sprintf("- Targets: %s", scopeText))
	content = strings.ReplaceAll(content, "- Out of scope:", "- Out of scope: (not specified)")
	content = strings.ReplaceAll(content, "- Testing window:", fmt.Sprintf("- Testing window: %s UTC session", date))
	if info.SessionID != "" {
		content = strings.ReplaceAll(content, "Session IDs", fmt.Sprintf("Session IDs\n- %s", info.SessionID))
	}
	content = rewriteFindingsSection(content, highLevel, scopeText, info.Observations)
	if info.Ledger != "" {
		content = strings.TrimSpace(content) + "\n\n## Evidence Ledger\n\n" + strings.TrimSpace(info.Ledger) + "\n"
	}
	content = appendSection(content, "Session Summary", info.Summary)
	content = appendSection(content, "Known Facts", sanitizeKnownFactsForReport(info.KnownFacts, info.Observations))
	content = appendSection(content, "Recent Observations", info.Observations)
	content = appendSection(content, "Task Foundation", info.Focus)
	content = appendSection(content, "Plan", info.Plan)
	content = appendSection(content, "Inventory", info.Inventory)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func sanitizeKnownFactsForReport(knownFacts, observations string) string {
	lines := strings.Split(knownFacts, "\n")
	if len(lines) == 0 {
		return knownFacts
	}
	privilegedEvidence := hasPrivilegeEvidence(observations)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, line)
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
		if isPrivilegedClaim(lower) && !privilegedEvidence {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func compactValues(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func deriveHighLevelFindings(info Info) []string {
	findings := filterFindingsWithEvidence(compactValues(info.Findings), info.Observations)
	if len(findings) == 0 {
		findings = append(findings, deriveObservationFindings(info.Observations)...)
	}
	if len(findings) == 0 {
		findings = append(findings, deriveFactFindings(info.KnownFacts, info.Observations)...)
	}
	if len(findings) == 0 {
		findings = append(findings, "Command observations captured; review Recent Observations for evidence.")
	}
	if len(findings) > 6 {
		findings = findings[:6]
	}
	return findings
}

func filterFindingsWithEvidence(findings []string, observations string) []string {
	if len(findings) == 0 {
		return findings
	}
	privilegedEvidence := hasPrivilegeEvidence(observations)
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		lower := strings.ToLower(strings.TrimSpace(finding))
		if isPrivilegedClaim(lower) && !privilegedEvidence {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func deriveFactFindings(knownFacts string, observations string) []string {
	lines := strings.Split(knownFacts, "\n")
	out := make([]string, 0, len(lines))
	privilegedEvidence := hasPrivilegeEvidence(observations)
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if lower == "none recorded." || lower == "summary pending." {
			continue
		}
		if isPrivilegedClaim(lower) && !privilegedEvidence {
			continue
		}
		out = append(out, line)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func deriveObservationFindings(observations string) []string {
	lines := strings.Split(observations, "\n")
	hosts := map[string]struct{}{}
	openPorts := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, nmapHostLinePrefix); idx >= 0 {
			hostPart := strings.TrimSpace(line[idx+len(nmapHostLinePrefix):])
			for _, ip := range ipv4Pattern.FindAllString(hostPart, -1) {
				hosts[ip] = struct{}{}
			}
		}
		for _, m := range openPortPattern.FindAllString(line, -1) {
			openPorts[m] = struct{}{}
		}
	}
	out := make([]string, 0, 4)
	if len(hosts) > 0 {
		out = append(out, fmt.Sprintf("Discovered %d host(s) during network scan.", len(hosts)))
	}
	if len(openPorts) > 0 {
		out = append(out, fmt.Sprintf("Observed %d open service port(s) in scan output.", len(openPorts)))
	}
	if len(out) == 0 && strings.TrimSpace(observations) != "" {
		out = append(out, "Scan commands completed and produced observational output.")
	}
	return out
}

func hasPrivilegeEvidence(observations string) bool {
	lower := strings.ToLower(observations)
	markers := []string{
		"uid=0(",
		"whoami",
		"meterpreter session",
		"command shell session",
		"session opened",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isPrivilegedClaim(lowerFact string) bool {
	phrases := []string{
		"admin access",
		"root access",
		"privilege escalation",
		"elevated privileges",
		"compromised",
		"full control",
	}
	for _, phrase := range phrases {
		if strings.Contains(lowerFact, phrase) {
			return true
		}
	}
	return false
}

func rewriteFindingsSection(content string, findings []string, scopeText, observations string) string {
	placeholderStart := "### [Finding Title]"
	appendixHeader := "\n## Appendix"
	start := strings.Index(content, placeholderStart)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start:], appendixHeader)
	if end < 0 {
		return content
	}
	end += start

	evidence := firstNonEmptyObservation(observations)
	if evidence == "" {
		evidence = "See Recent Observations section."
	}
	if scopeText == "" {
		scopeText = "(not provided)"
	}
	var b strings.Builder
	for i, finding := range findings {
		fmt.Fprintf(&b, "### Finding %d\n", i+1)
		fmt.Fprintf(&b, "- Severity: Info\n")
		fmt.Fprintf(&b, "- Affected assets: %s\n", scopeText)
		fmt.Fprintf(&b, "- Description: %s\n", finding)
		fmt.Fprintf(&b, "- Evidence: %s\n", evidence)
		fmt.Fprintf(&b, "- Steps to reproduce: Review command sequence in Recent Observations.\n")
		fmt.Fprintf(&b, "- Impact: Requires analyst validation.\n")
		fmt.Fprintf(&b, "- Remediation: Validate exposure and apply least-privilege hardening where needed.\n")
		fmt.Fprintf(&b, "- References: N/A\n\n")
	}
	return content[:start] + strings.TrimSpace(b.String()) + content[end:]
}

func firstNonEmptyObservation(observations string) string {
	for _, line := range strings.Split(observations, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		return line
	}
	return ""
}

func appendSection(content string, title string, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return content
	}
	content = strings.TrimSpace(content)
	return content + "\n\n## " + title + "\n\n" + body + "\n"
}
