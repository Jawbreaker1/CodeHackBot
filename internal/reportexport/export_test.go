package reportexport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func TestSavePDFUsesTheSharedReportTemplate(t *testing.T) {
	original := reportPDFScript
	reportPDFScript = "import json, sys; metadata=json.loads(sys.argv[2]); open(sys.argv[1], 'wb').write(b'%PDF-1.4 test\\n' + metadata['format'].encode() + b'\\n' + sys.stdin.buffer.read())"
	defer func() { reportPDFScript = original }()
	root := t.TempDir()
	state := assessment.State{ID: "fixture", Goal: "Review fixture", Scope: "synthetic source only", Status: "completed", Plans: []assessment.Decision{{Summary: "One check completed", Complete: true}}}
	artifact, err := Save(root, state, assessment.OWASPReport, PDF)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "reports", artifact.Name))
	if err != nil || artifact.Output != PDF || !strings.HasPrefix(string(data), "%PDF-") || !strings.Contains(string(data), "owasp-wstg") || !strings.Contains(string(data), "Review fixture") {
		t.Fatalf("shared PDF export failed: artifact=%+v err=%v", artifact, err)
	}
}
