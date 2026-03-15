package cli

import (
	"strings"
	"testing"
)

func TestObjectiveChecklistAddsVulnerabilitySourceValidationItem(t *testing.T) {
	items := objectiveChecklist("Scan router and identify CVEs, then report vulnerabilities.")
	if len(items) == 0 {
		t.Fatalf("expected checklist items")
	}
	found := false
	for _, item := range items {
		lower := strings.ToLower(item)
		if strings.Contains(lower, "source-backed evidence refs") && strings.Contains(lower, "target-applicability") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected vulnerability source-validation checklist item, got %#v", items)
	}
}

func TestObjectiveChecklistDoesNotForceVulnerabilityItemForNonVulnGoal(t *testing.T) {
	items := objectiveChecklist("Extract contents of secret.zip and show the password.")
	for _, item := range items {
		lower := strings.ToLower(item)
		if strings.Contains(lower, "source-backed evidence refs") && strings.Contains(lower, "target-applicability") {
			t.Fatalf("did not expect vulnerability checklist item for archive goal, got %#v", items)
		}
	}
}
