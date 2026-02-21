package report

import (
	"fmt"
	"strings"
)

var requiredSectionsByProfile = map[string][]string{
	string(ProfileStandard): {"Executive Summary", "Scope", "Methodology", "Findings", "Appendix"},
	string(ProfileOWASP):    {"Executive Summary", "Scope", "OWASP Methodology Mapping", "Findings", "Appendix"},
	string(ProfileNIS2):     {"Executive Summary", "Scope & Governance Context", "NIS2 Control Areas", "Findings", "Appendix"},
	string(ProfileInternal): {"Executive Summary", "Scope", "Validation Approach", "Findings", "Appendix"},
}

func ValidateRequiredSections(profile, content string) error {
	resolvedProfile, err := NormalizeProfile(profile)
	if err != nil {
		return err
	}
	required := requiredSectionsByProfile[resolvedProfile]
	missing := make([]string, 0, len(required))
	for _, section := range required {
		header := "## " + section
		if !strings.Contains(content, header) {
			missing = append(missing, section)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("report template missing required sections for profile %s: %s", resolvedProfile, strings.Join(missing, ", "))
	}
	return nil
}
