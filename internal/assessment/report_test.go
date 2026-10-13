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
	index, err := os.ReadFile(filepath.Join(root, "evidence-index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report, secret) || strings.Contains(report, "Authorization: Bearer") {
		t.Fatal("report copied raw credentials from execution evidence or operator conversation")
	}
	if strings.Contains(string(index), secret) || !strings.Contains(report, "evidence-index.md") || !strings.Contains(string(index), "tasks/inspect/execution-01.log") || !strings.Contains(string(index), "The scoped service returned a successful response.") {
		t.Fatal("report and evidence index did not separate the conclusion from raw evidence references")
	}
}

func TestReportsShowOpenLeadContinuationSeparatelyFromFindings(t *testing.T) {
	root := t.TempDir()
	state := State{ID: "fixture", Status: "completed_with_gaps", Plans: []Decision{{
		Complete: true, Summary: "The observed version is still unknown.",
		OpenLeads: []Lead{{ID: "version", Hypothesis: "The installed build may precede a vendor fix.", NextCheck: "Read the installed build from the authorized status page and compare it with the vendor release history.", Evidence: []string{"tasks/inspect/version.log"}}},
	}}}
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		formatted, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		for _, report := range []string{string(canonical), string(formatted)} {
			if !strings.Contains(report, "Recommended continuation") || !strings.Contains(report, state.Plans[0].OpenLeads[0].NextCheck) || !strings.Contains(report, "Unverified lead: "+state.Plans[0].OpenLeads[0].Hypothesis) {
				t.Fatalf("%s report lost the next check or its unresolved status: %s", format, report)
			}
		}
	}
}

func TestExecutiveSummaryShowsFindingsBeforeAnUnresolvedGap(t *testing.T) {
	root := t.TempDir()
	state := State{ID: "fixture", Status: "completed_with_gaps", Plans: []Decision{{
		Complete: true,
		Summary:  "A remaining endpoint was not tested.",
		Findings: []Finding{{Title: "CSP permits inline scripts", Status: "candidate", Severity: "low", Impact: "The policy offers less protection.", Steps: []string{"Inspect the policy."}, Evidence: []string{"policy.log"}, Remediation: []string{"Tighten the policy."}}},
	}}}
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	outputs := [][]byte{canonical}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		formatted, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, formatted)
	}
	for _, output := range outputs {
		text := string(output)
		findings := strings.Index(text, "**Findings recorded for review**")
		title := strings.Index(text, "CSP permits inline scripts")
		gap := strings.Index(text, "A remaining endpoint was not tested.")
		if findings < 0 || title < findings || gap < title {
			t.Fatalf("executive summary omitted findings before the gap: %s", text)
		}
	}
}

func TestReportsPreferFinalOperatorSummaryAndKeepFullTasksInEvidenceIndex(t *testing.T) {
	root := t.TempDir()
	state := State{ID: "fixture", Status: "completed", Plans: []Decision{{
		PlainSummary:    "Inspect the observed application boundary.",
		Tasks:           []Task{{ID: "inspect", Goal: "Long technical worker instructions and every exploratory variant", DoneWhen: "Detailed completion criteria"}},
		ApprovedTaskIDs: []string{"inspect"},
	}, {
		Complete:     true,
		PlainSummary: "The application boundary was reviewed; no finding is established.",
		Summary:      "A narrow method gap might need several more tests.",
	}}, Results: []Result{{Task: Task{ID: "inspect", Goal: "Long technical worker instructions and every exploratory variant", DoneWhen: "Detailed completion criteria"}, Status: "done"}}}
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(root, "evidence-index.md"))
	if err != nil {
		t.Fatal(err)
	}
	outputs := [][]byte{canonical}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		formatted, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, formatted)
	}
	for _, output := range outputs {
		text := string(output)
		if !strings.Contains(text, "The application boundary was reviewed") || strings.Contains(text, "narrow method gap might need") || !strings.Contains(text, "Inspect the observed application boundary") || !strings.Contains(text, "`inspect` —") || strings.Contains(text, "Long technical worker instructions") {
			t.Fatalf("report did not prioritize the conclusion and compact method: %s", text)
		}
	}
	if !strings.Contains(string(index), "Long technical worker instructions") || !strings.Contains(string(index), "Detailed completion criteria") {
		t.Fatal("evidence index lost full worker assignment")
	}
}

func TestReportKeepsRepeatedExecutionReferencesOutOfOverview(t *testing.T) {
	root := t.TempDir()
	result := Result{Task: Task{ID: "inspect", Goal: "Inspect the scoped system"}, Status: "done", Summary: "Inspection completed with recorded evidence."}
	for i := 0; i < 40; i++ {
		result.Evidence = append(result.Evidence, ctxpacket.ExecutionResult{ExitStatus: "0", LogRefs: []string{"tasks/inspect/logs/command.log", "tasks/inspect/logs/command.log.stdout", "tasks/inspect/logs/command.log.stderr"}})
	}
	if err := writeReport(root, State{ID: "fixture", Results: []Result{result}}); err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(root, "evidence-index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(report), "command.log.stdout") || len(report) >= len(index) || strings.Count(string(index), "command.log.stdout") != 40 {
		t.Fatal("overview still contains repetitive execution references or index lost them")
	}
}

func TestReportsIdentifyTheExactDeniedAction(t *testing.T) {
	root := t.TempDir()
	denied := &ctxpacket.DeniedExecution{
		Summary: "Read approval source excerpts", Target: "local source files",
		Command: "cat policy.go", AuditRef: "tasks/check/logs/denied.approval.json",
	}
	state := State{
		ID: "fixture", Status: "completed_with_gaps",
		Plans:   []Decision{{Complete: true, Summary: "The operator declined the harmless marker test.", Gaps: []string{"The marker test was denied."}}},
		Results: []Result{{Task: Task{ID: "check", Goal: "Validate a harmless marker"}, Status: "blocked", DeniedExecution: denied}},
	}
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, denied.Summary) || !strings.Contains(text, denied.AuditRef) || !strings.Contains(text, "did not execute") || strings.Contains(text, "declined the harmless marker") || strings.Contains(text, "marker test was denied") {
		t.Fatalf("report lost the precise denied action: %s", text)
	}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		formatted, err := RenderFormattedReport(state, format)
		if err != nil || !strings.Contains(string(formatted), denied.Summary) || !strings.Contains(string(formatted), denied.AuditRef) || strings.Contains(string(formatted), "marker test was denied") {
			t.Fatalf("%s report lost denied action: %v %s", format, err, formatted)
		}
	}
	compact := compactPriorResults(state.Results)
	if len(compact) != 1 || compact[0].DeniedExecution == nil || compact[0].DeniedExecution.Command != denied.Command {
		t.Fatalf("coordinator handoff lost denied action: %+v", compact)
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
