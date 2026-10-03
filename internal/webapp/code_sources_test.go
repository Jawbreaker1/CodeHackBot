package webapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestCodeAnalysisShowsOnlyRecordedSourceLines(t *testing.T) {
	server := NewServer(Config{RepoRoot: t.TempDir()})
	current, err := server.newRun("project", "Review access checks", "synthetic source")
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(current.root, "tasks", "review", "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(work, "auth.source.json")
	if err := os.WriteFile(source, []byte(`{"version":1,"repository":"fixture/app","revision":"abc123","path":"server/auth.py","lines":[{"number":1,"text":"def view(user, record):"},{"number":2,"text":"    owner = record.owner"},{"number":3,"text":"    return record.secret"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	current.state = assessment.State{ID: current.id, Status: "completed", Plans: []assessment.Decision{{Complete: true, Findings: []assessment.Finding{{
		Title: "Missing owner check", Status: "candidate", Impact: "Another user's record may be returned.",
		Steps: []string{"Inspect the view"}, Evidence: []string{source}, Remediation: []string{"Compare owner to current user"},
		SourceLocations: []assessment.SourceLocation{{Repository: "fixture/app", Revision: "abc123", Path: "server/auth.py", StartLine: 3, ArtifactRef: source}},
	}}}}, Results: []assessment.Result{{Task: assessment.Task{ID: "review"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{ArtifactRefs: []string{source}}}}}}
	view := current.analysis()
	if len(view.Findings) != 1 || len(view.Findings[0].SourceLocations) != 1 {
		t.Fatalf("source location missing: %+v", view.Findings)
	}
	location := view.Findings[0].SourceLocations[0]
	if location.ArtifactURL == "" || len(location.Lines) != 3 || location.Lines[2].Number != 3 || !location.Lines[2].Highlight || !strings.Contains(location.Lines[2].Text, "return record.secret") {
		t.Fatalf("recorded code not shown at the cited line: %+v", location)
	}
	combined := buildCustomerAnalysis("project", []analysisView{view})
	if len(combined.Findings[0].SourceLocations[0].Lines) != 3 {
		t.Fatal("customer view lost the source excerpt")
	}
	current.state.Plans[0].Findings[0].Evidence = []string{"unrelated.log"}
	if got := current.analysis().Findings[0].SourceLocations[0]; got.ArtifactURL != "" || len(got.Lines) != 0 {
		t.Fatalf("uncited artifact leaked into code view: %+v", got)
	}
	current.state.Plans[0].Findings[0].Evidence = []string{source}
	if err := os.WriteFile(source, []byte("1 def view(user, record):\n2 owner = record.owner\n3 return record.secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := current.analysis().Findings[0].SourceLocations[0]; got.ArtifactURL != "" || len(got.Lines) != 0 {
		t.Fatalf("plain-text evidence was misread as a source file: %+v", got)
	}
}
