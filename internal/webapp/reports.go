package webapp

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

//go:embed report_pdf.py
var reportPDFScript string

type reportOutput string

const (
	markdownOutput reportOutput = "markdown"
	pdfOutput      reportOutput = "pdf"
)

type generatedReport struct {
	Name   string                  `json:"name"`
	Format assessment.ReportFormat `json:"format"`
	Output reportOutput            `json:"output,omitempty"`
	Bytes  int64                   `json:"bytes"`
}

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

func (r generatedReport) Label() string {
	ext := ".md"
	if r.Output == pdfOutput {
		ext = ".pdf"
	}
	if r.Format == assessment.OWASPReport {
		return "OWASP WSTG-aligned report" + ext
	}
	return "PTES-aligned report" + ext
}

func (r generatedReport) Confirmation() string {
	label := "PTES"
	if r.Format == assessment.OWASPReport {
		label = "OWASP WSTG"
	}
	output := "Markdown"
	if r.Output == pdfOutput {
		output = "PDF"
	}
	return "Created the " + label + "-aligned " + output + " draft from the saved assessment. It contains the recorded scope and work, any findings, limitations, and report completeness checks. This export does not add requested details that are absent from the saved assessment; review the linked file before sharing."
}

func saveFormattedReport(root string, state assessment.State, format assessment.ReportFormat, output reportOutput) (*generatedReport, error) {
	if output == "" {
		output = markdownOutput
	}
	if output != markdownOutput && output != pdfOutput {
		return nil, fmt.Errorf("unsupported report output %q", output)
	}
	content, err := assessment.RenderFormattedReport(state, format)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "reports")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create reports directory: %w", err)
	}
	ext := ".md"
	if output == pdfOutput {
		ext = ".pdf"
	}
	file, err := os.CreateTemp(dir, string(format)+"-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("create formatted report: %w", err)
	}
	defer file.Close()
	if output == pdfOutput {
		if err := file.Close(); err != nil {
			os.Remove(file.Name())
			return nil, fmt.Errorf("close PDF report: %w", err)
		}
		if err := renderReportPDF(content, file.Name()); err != nil {
			os.Remove(file.Name())
			return nil, err
		}
		info, err := os.Stat(file.Name())
		if err != nil {
			os.Remove(file.Name())
			return nil, fmt.Errorf("stat PDF report: %w", err)
		}
		return &generatedReport{Name: filepath.Base(file.Name()), Format: format, Output: output, Bytes: info.Size()}, nil
	}
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
	return &generatedReport{Name: filepath.Base(file.Name()), Format: format, Output: output, Bytes: int64(len(content))}, nil
}

func renderReportPDF(markdown []byte, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-c", reportPDFScript, path)
	cmd.Stdin = bytes.NewReader(markdown)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("render PDF report: %w", ctx.Err())
		}
		return fmt.Errorf("render PDF report (install python3-markdown-it and chromium on Kali): %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read PDF report: %w", err)
	}
	defer file.Close()
	header := make([]byte, 5)
	if _, err := file.Read(header); err != nil || !bytes.Equal(header, []byte("%PDF-")) {
		return fmt.Errorf("report renderer did not produce a PDF")
	}
	return nil
}

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
