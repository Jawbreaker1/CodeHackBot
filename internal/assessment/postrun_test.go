package assessment

import (
	"strings"
	"testing"
)

func TestPostRunContextKeepsOpenNextChecks(t *testing.T) {
	state := State{Status: "completed_with_gaps", Plans: []Decision{{
		Complete:  true,
		OpenLeads: []Lead{{ID: "version", Hypothesis: "Installed build is unknown.", NextCheck: "Read the installed build from the status page.", Evidence: []string{"tasks/inspect/version.log"}}},
	}}}
	context := PostRunFindingsContext(state)
	if !strings.Contains(context, `"open_leads"`) || !strings.Contains(context, `"next_check":"Read the installed build from the status page."`) || !strings.Contains(context, `"unresolved_gaps"`) {
		t.Fatalf("post-run coordinator lost its continuation: %s", context)
	}
}
