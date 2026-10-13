package webapp

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request, root string, messages []intakeMessage, id string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	var ref attachmentRef
	found := false
	for _, message := range messages {
		for _, candidate := range message.Attachments {
			if candidate.ID == id {
				ref, found = candidate, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	path, err := attachmentPath(root, ref)
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", ref.MIMEType)
	w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(safeAttachmentFilename(ref.Filename), `"`, "")+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, ref.Filename, time.Time{}, file)
}

func (s *Server) serveAssessmentArtifact(w http.ResponseWriter, r *http.Request, current *run) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	wanted := filepath.Clean(strings.TrimSpace(r.URL.Query().Get("path")))
	if wanted == "." || !filepath.IsAbs(wanted) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	current.mu.RLock()
	root := current.root
	anchors := browserAnchors(current.state)
	found := false
	for _, result := range current.state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range append(append([]string(nil), evidence.ArtifactRefs...), evidence.LogRefs...) {
				if filepath.Clean(ref) == wanted {
					found = true
					break
				}
			}
		}
	}
	for _, worker := range current.workers {
		for _, evidence := range worker.Evidence {
			for _, ref := range append(append([]string(nil), evidence.ArtifactRefs...), evidence.LogRefs...) {
				if filepath.Clean(ref) == wanted {
					found = true
				}
			}
		}
		for _, ref := range worker.ExpectedArtifacts {
			if filepath.Clean(ref) == wanted && resolvedWithin(filepath.Join(root, "tasks", worker.ID, "work"), wanted) {
				found = true
			}
		}
	}
	current.mu.RUnlock()
	if !found {
		pages, _, _ := browserAnalysis(root, current.id, anchors)
		for _, page := range pages {
			if page.ScreenshotPath == wanted {
				found = true
				break
			}
		}
	}
	if !found || !resolvedWithin(root, wanted) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	file, err := os.Open(wanted)
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxArtifactServeBytes {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(wanted))
	if filepath.Ext(wanted) == ".log" {
		contentType = "text/plain; charset=utf-8"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	disposition := "attachment"
	if imageMIME(wanted) != "" {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+`; filename="`+strings.ReplaceAll(filepath.Base(wanted), `"`, "")+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(wanted), info.ModTime(), file)
}

func pathWithin(root, path string) bool {
	root, rootErr := filepath.Abs(root)
	path, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
