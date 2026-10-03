package assessment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestSourceFindingRequiresCitedRegisteredArtifact(t *testing.T) {
	sourceRef := filepath.Join(t.TempDir(), "source.json")
	if err := os.WriteFile(sourceRef, []byte(`{"version":1,"repository":"https://example.test/app.git","revision":"abc123","path":"server/auth.py","lines":[{"number":18,"text":"def view():"},{"number":19,"text":"    owner = record.owner"},{"number":20,"text":"    return record.secret"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	state := State{Limits: Limits{Workers: 1, Tasks: 12}, Results: []Result{{Task: Task{ID: "review"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{ArtifactRefs: []string{sourceRef}}}}}}
	finding := Finding{
		Title: "Missing authorization check", Status: "candidate", Impact: "Another account's record may be readable.",
		Steps: []string{"Review the handler"}, Evidence: []string{sourceRef}, Remediation: []string{"Check the caller's account"},
		SourceLocations: []SourceLocation{{Repository: "https://example.test/app.git", Revision: "abc123", Path: "server/auth.py", StartLine: 18, EndLine: 20, ArtifactRef: sourceRef}},
	}
	decision := Decision{Summary: "Review the source", Tasks: []Task{{ID: "verify", Goal: "Check the behavior", DoneWhen: "Behavior observed"}}, Findings: []Finding{finding}}
	if err := validateDecision(decision, state); err != nil {
		t.Fatal(err)
	}
	decision.Findings[0].SourceLocations[0].ArtifactRef = "/session/tasks/review/work/other.py"
	if err := validateDecision(decision, state); err == nil {
		t.Fatal("uncited source file was accepted")
	}
	decision.Findings[0].SourceLocations[0].ArtifactRef = sourceRef
	decision.Findings[0].SourceLocations[0].Path = "../auth.py"
	if err := validateDecision(decision, state); err == nil {
		t.Fatal("invalid logical source path was accepted")
	}
	decision.Findings[0].SourceLocations[0].Path = "server/auth.py"
	if err := os.WriteFile(sourceRef, []byte("18 def view():\n19 owner = record.owner\n20 return record.secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateDecision(decision, state); err == nil {
		t.Fatal("unstructured numbered excerpt was accepted as source code")
	}
}

func TestFormattedReportsKeepSourceLocation(t *testing.T) {
	finding := Finding{Title: "Source lead", Status: "candidate", Impact: "Potential disclosure", Steps: []string{"Inspect handler"}, Evidence: []string{"source.py"}, Remediation: []string{"Check ownership"}, SourceLocations: []SourceLocation{{Repository: "fixture/app", Revision: "abc123", Path: "server/auth.py", StartLine: 18, EndLine: 20, ArtifactRef: "source.py"}}}
	state := State{ID: "fixture", Plans: []Decision{{Complete: true, Findings: []Finding{finding}}}}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		report, err := RenderFormattedReport(state, format)
		if err != nil || !strings.Contains(string(report), "server/auth.py:18–20") || !strings.Contains(string(report), "fixture/app @ abc123") {
			t.Fatalf("%s omitted the source location: %v\n%s", format, err, report)
		}
	}
}
