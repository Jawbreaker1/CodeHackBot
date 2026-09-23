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

func TestCanonicalReportDoesNotPresentUnreviewedPlanAsConclusion(t *testing.T) {
	root := t.TempDir()
	state := State{ID: "fixture", Status: "incomplete", Plans: []Decision{{Summary: "The worker will inspect next", Gaps: []string{"Inspection pending"}, Tasks: []Task{{ID: "inspect"}}}}, Results: []Result{{Task: Task{ID: "inspect"}, Status: "done", Summary: "Inspection finished with a blocked page."}}}
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "The worker will inspect next") || !strings.Contains(text, "Inspection finished with a blocked page") || !strings.Contains(text, "were not reconciled") {
		t.Fatalf("canonical report presented stale planning as a conclusion: %s", text)
	}
}

func TestLatestUnreviewedResultExcludesEarlierWork(t *testing.T) {
	state := State{Status: "incomplete", Plans: []Decision{{Tasks: []Task{{ID: "first"}}}, {Tasks: []Task{{ID: "second"}}}}, Results: []Result{{Task: Task{ID: "first"}, Status: "done"}}}
	if _, pending := LatestUnreviewedResult(state); pending {
		t.Fatal("earlier worker result was labeled unreviewed after a later plan")
	}
}
