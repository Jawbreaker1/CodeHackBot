package assessment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestReportReferencesRawEvidenceWithoutCopyingSensitiveInput(t *testing.T) {
	root := t.TempDir()
	const secret = "synthetic-bearer-token-for-regression"
	state := State{
		ID:               "fixture",
		OperatorMessages: []string{"Use " + secret + " for this test"},
		Results: []Result{{
			Task:    Task{ID: "inspect", Goal: "Inspect scoped service"},
			Status:  "done",
			Summary: "The scoped service returned a successful response.",
			Evidence: []ctxpacket.ExecutionResult{{
				ActualExec:    "curl -H 'Authorization: Bearer " + secret + "' http://127.0.0.1/",
				OutputSummary: "Response included " + secret,
				ExitStatus:    "0",
				LogRefs:       []string{"tasks/inspect/execution-01.log"},
			}},
		}},
	}
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	if strings.Contains(report, secret) || strings.Contains(report, "Authorization: Bearer") {
		t.Fatal("report copied raw credentials from execution evidence or operator conversation")
	}
	if !strings.Contains(report, "tasks/inspect/execution-01.log") || !strings.Contains(report, "The scoped service returned a successful response.") {
		t.Fatal("report lost the worker conclusion or reference to its local evidence")
	}
}

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
	if strings.Contains(string(data), "withdrawn candidate") || !strings.Contains(string(data), "validated issue") || !strings.Contains(string(data), "Earlier reproduction claims without a supported challenge") || !strings.Contains(string(data), "Status: candidate (model assessment") {
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
