package webapp

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// Watch reads only runtime-recorded execution streams and declared image
// artifacts. It does not execute commands or infer progress from output text.
func (r *run) watch(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	r.mu.RLock()
	worker, ok := r.workers[request.URL.Query().Get("worker")]
	root, id := r.root, r.id
	r.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "worker not found")
		return
	}
	images := []string{}
	for _, ref := range worker.ExpectedArtifacts {
		if filepath.Ext(ref) == ".png" || filepath.Ext(ref) == ".jpg" || filepath.Ext(ref) == ".webp" {
			if resolvedWithin(filepath.Join(root, "tasks", worker.ID, "work"), ref) {
				if info, err := os.Stat(ref); err == nil && info.Mode().IsRegular() && info.Size() <= maxArtifactServeBytes {
					images = append(images, "/api/v1/assessments/"+url.PathEscape(id)+"/artifact?path="+url.QueryEscape(ref))
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Phase  string   `json:"phase"`
		Action string   `json:"action"`
		Stdout string   `json:"stdout"`
		Stderr string   `json:"stderr"`
		Images []string `json:"images"`
	}{worker.Phase, worker.Action, outputTail(root, worker.ExecutionLog, ".stdout"), outputTail(root, worker.ExecutionLog, ".stderr"), images})
}

func outputTail(root, log, suffix string) string {
	if log == "" || !resolvedWithin(root, log+suffix) {
		return ""
	}
	f, err := os.Open(log + suffix)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	const limit = 16 * 1024
	if info.Size() > limit {
		if _, err := f.Seek(-limit, io.SeekEnd); err != nil {
			return ""
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return ""
	}
	return string(data)
}

func resolvedWithin(root, path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	return pathWithin(root, path) && pathWithin(resolvedRoot, resolved)
}
