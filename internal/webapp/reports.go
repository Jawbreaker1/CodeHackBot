package webapp

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

type generatedReport struct {
	Name   string                  `json:"name"`
	Format assessment.ReportFormat `json:"format"`
	Bytes  int64                   `json:"bytes"`
}

func (r generatedReport) Label() string {
	if r.Format == assessment.OWASPReport {
		return "OWASP WSTG-aligned report.md"
	}
	return "PTES-aligned report.md"
}

func saveFormattedReport(root string, state assessment.State, format assessment.ReportFormat) (*generatedReport, error) {
	content, err := assessment.RenderFormattedReport(state, format)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "reports")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create reports directory: %w", err)
	}
	file, err := os.CreateTemp(dir, string(format)+"-*.md")
	if err != nil {
		return nil, fmt.Errorf("create formatted report: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		os.Remove(file.Name())
		return nil, fmt.Errorf("write formatted report: %w", err)
	}
	if err := file.Sync(); err != nil {
		os.Remove(file.Name())
		return nil, fmt.Errorf("sync formatted report: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return nil, fmt.Errorf("close formatted report: %w", err)
	}
	return &generatedReport{Name: filepath.Base(file.Name()), Format: format, Bytes: int64(len(content))}, nil
}

func (r *run) formattedReport(w http.ResponseWriter, name string) {
	if !validSessionID(name) || !strings.HasSuffix(name, ".md") {
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
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
