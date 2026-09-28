package webapp

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/reportexport"
)

type reportOutput = reportexport.Output

const (
	markdownOutput = reportexport.Markdown
	pdfOutput      = reportexport.PDF
)

type generatedReport = reportexport.Artifact

func (r *run) report(w http.ResponseWriter) {
	r.sessionMarkdown(w, "report.md", "report is not ready")
}

func (r *run) evidenceIndex(w http.ResponseWriter) {
	r.sessionMarkdown(w, "evidence-index.md", "evidence index is not ready")
}

func (r *run) sessionMarkdown(w http.ResponseWriter, name, missing string) {
	data, err := os.ReadFile(filepath.Join(r.root, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, missing)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write(data)
}

var saveFormattedReport = reportexport.Save

func (r *run) formattedReport(w http.ResponseWriter, name string) {
	if !validSessionID(name) || (!strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".pdf")) {
		writeError(w, http.StatusNotFound, "report not found")
		return
	}
	path := filepath.Join(r.root, "reports", name)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "report not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	contentType := "text/markdown; charset=utf-8"
	if strings.HasSuffix(name, ".pdf") {
		contentType = "application/pdf"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
