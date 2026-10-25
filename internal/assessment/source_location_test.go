package assessment

import (
	"os"
	"path/filepath"
	"slices"
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
	// The worker's registered artifact is the source of truth for provenance.
	// A coordinator may omit it or use an imprecise label in its decision.
	decision.Findings[0].SourceLocations[0].Repository = "local filesystem"
	decision.Findings[0].SourceLocations[0].Revision = ""
	decision.Findings[0].SourceLocations[0].Path = ""
	normalizeSourceLocations(&decision, state)
	if got := decision.Findings[0].SourceLocations[0]; got.Repository != "https://example.test/app.git" || got.Revision != "abc123" || got.Path != "server/auth.py" {
		t.Fatalf("registered artifact metadata was not used: %+v", got)
	}
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("normalized source location was rejected: %v", err)
	}
	// A cited, registered source location is evidence even if the coordinator
	// omits its duplicate reference from the finding's evidence array.
	decision.Findings[0].Evidence = nil
	decision.Findings[0].SourceLocations[0].Repository = ""
	decision.Findings[0].SourceLocations[0].Revision = ""
	decision.Findings[0].SourceLocations[0].Path = ""
	normalizeSourceLocations(&decision, state)
	if !slices.Contains(decision.Findings[0].Evidence, sourceRef) {
		t.Fatal("registered source location was not cited as finding evidence")
	}
	if got := decision.Findings[0].SourceLocations[0]; got.Path != "server/auth.py" || got.Revision != "abc123" {
		t.Fatalf("registered source location was not normalized: %+v", got)
	}
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("registered source location should be sufficient evidence: %v", err)
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
		if err != nil || !strings.Contains(string(report), "Source file: server/auth.py:18–20") || !strings.Contains(string(report), "fixture/app @ abc123") {
			t.Fatalf("%s omitted the source location: %v\n%s", format, err, report)
		}
		state.Plans[0].Findings[0].SourceLocations[0].Repository = "https://example.test"
		state.Plans[0].Findings[0].SourceLocations[0].Path = "/page"
		webReport, err := RenderFormattedReport(state, format)
		if err != nil || !strings.Contains(string(webReport), "Web page: /page:18–20") {
			t.Fatalf("%s omitted the web page label: %v\n%s", format, err, webReport)
		}
		state.Plans[0].Findings[0].SourceLocations[0].Repository = "fixture/app"
		state.Plans[0].Findings[0].SourceLocations[0].Path = "server/auth.py"
	}
}

func TestMissingSourceLineExplainsCapturedRanges(t *testing.T) {
	ref := filepath.Join(t.TempDir(), "source.json")
	data := `{"version":1,"repository":"fixture/app","revision":"abc123","path":"server/auth.py","lines":[{"number":18,"text":"first"},{"number":41,"text":"second"},{"number":42,"text":"third"}]}`
	if err := os.WriteFile(ref, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadSourceArtifact(SourceLocation{Repository: "fixture/app", Revision: "abc123", Path: "server/auth.py", StartLine: 18, EndLine: 19, ArtifactRef: ref})
	if err == nil || !strings.Contains(err.Error(), "cited line 19") || !strings.Contains(err.Error(), "captured lines: 18, 41-42") || !strings.Contains(err.Error(), "omit this source location") {
		t.Fatalf("missing source line should yield a usable correction: %v", err)
	}
}

func TestRootRelativeWebSourcePathIsAccepted(t *testing.T) {
	ref := filepath.Join(t.TempDir(), "source.json")
	data := `{"version":1,"repository":"https://example.test","revision":"abc123","path":"/page","lines":[{"number":18,"text":"const value = input.value"}]}`
	if err := os.WriteFile(ref, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	state := State{Limits: Limits{Workers: 1, Tasks: 12}, Results: []Result{{Task: Task{ID: "review"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{ArtifactRefs: []string{ref}}}}}}
	finding := Finding{Title: "Client observation", Status: "candidate", Impact: "Possible data exposure", Steps: []string{"Inspect the page"}, Evidence: []string{ref}, Remediation: []string{"Review the flow"}, SourceLocations: []SourceLocation{{StartLine: 18, ArtifactRef: ref}}}
	decision := Decision{Summary: "Review the page", Tasks: []Task{{ID: "verify", Goal: "Check the behavior", DoneWhen: "Behavior observed"}}, Findings: []Finding{finding}}
	normalizeSourceLocations(&decision, state)
	if got := decision.Findings[0].SourceLocations[0]; got.Repository != "https://example.test" || got.Path != "/page" {
		t.Fatalf("web source metadata was not retained: %+v", got)
	}
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("root-relative web source path should be accepted: %v", err)
	}
}

func TestSourceArtifactPathMatchesRepositoryKind(t *testing.T) {
	for _, test := range []struct {
		name       string
		repository string
		path       string
		kind       SourcePathKind
	}{
		{"local file", "fixture/app", "server/auth.py", SourceFilePath},
		{"remote repository file", "https://example.test/app.git", "server/auth.py", SourceFilePath},
		{"local absolute path", "fixture/app", "/server/auth.py", ""},
		{"web page", "https://example.test", "/bli-medlem", SourceWebPath},
		{"web page with trailing slash", "https://example.test", "/bli-medlem/", SourceWebPath},
		{"web root", "https://example.test", "/", SourceWebPath},
		{"web traversal", "https://example.test", "/../admin", ""},
		{"web network path", "https://example.test", "//other.test/path", ""},
		{"web URL with query is not a path", "https://example.test", "/page?token=value", ""},
		{"full URL is not a file path", "https://example.test", "https://example.test/page", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifySourcePath(test.repository, test.path); got != test.kind {
				t.Fatalf("ClassifySourcePath(%q, %q) = %q, want %q", test.repository, test.path, got, test.kind)
			}
		})
	}
}
