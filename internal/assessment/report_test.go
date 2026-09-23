package assessment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportUsesCurrentFindingRevision(t *testing.T) {
	root := t.TempDir()
	prior := Finding{Title: "withdrawn candidate", Status: "candidate", Impact: "hypothesis", Steps: []string{"inspect"}, Evidence: []string{"old.log"}, Remediation: []string{"review"}}
	current := Finding{Title: "validated issue", Status: "reproduced", Impact: "observed", Steps: []string{"inspect"}, Evidence: []string{"new.log"}, Remediation: []string{"repair"}}
	state := State{ID: "fixture", Goal: "review fixture", Scope: "synthetic only", Plans: []Decision{{Findings: []Finding{prior}}, {Complete: true, Findings: []Finding{current}}}}
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "withdrawn candidate") || !strings.Contains(string(data), "validated issue") {
		t.Fatalf("report did not use the current finding revision: %s", data)
	}
}
