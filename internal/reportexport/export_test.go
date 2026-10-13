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

func TestSaveDraftKeepsMarkdownBesideProjectPDF(t *testing.T) {
	original := reportPDFScript
	reportPDFScript = "import json, sys; metadata=json.loads(sys.argv[2]); open(sys.argv[1], 'wb').write(b'%PDF-1.4 test\\n' + metadata['subject_kind'].encode() + b'\\n' + sys.stdin.buffer.read())"
	defer func() { reportPDFScript = original }()
	root := t.TempDir()
	markdown := []byte("# Project report\n\n## Findings\n\nSaved finding.\n")
	artifacts, err := SaveDraft(root, markdown, "sample-project", "Project", assessment.PTESReport, PDF)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 || artifacts[0].Output != Markdown || artifacts[1].Output != PDF {
		t.Fatalf("draft artifacts = %+v", artifacts)
	}
	md, err := os.ReadFile(filepath.Join(root, "reports", artifacts[0].Name))
	if err != nil || string(md) != string(markdown) {
		t.Fatalf("saved Markdown = %q, err = %v", md, err)
	}
	pdf, err := os.ReadFile(filepath.Join(root, "reports", artifacts[1].Name))
	if err != nil || !strings.HasPrefix(string(pdf), "%PDF-") || !strings.Contains(string(pdf), "Project") {
		t.Fatalf("saved PDF = %q, err = %v", pdf, err)
	}
	for _, artifact := range artifacts {
		info, err := os.Stat(filepath.Join(root, "reports", artifact.Name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("draft permissions for %s: %v %v", artifact.Name, info, err)
		}
	}
}
