package webapp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

type browserPreview struct {
	URL       string `json:"url"`
	Step      string `json:"step"`
	Phase     string `json:"phase"`
	UpdatedAt string `json:"updated_at"`
}

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
	var browser *browserPreview
	browserImage := ""
	refs := append([]string(nil), worker.ExpectedArtifacts...)
	for _, evidence := range worker.Evidence {
		refs = append(refs, evidence.ArtifactRefs...)
	}
	seen := make(map[string]bool)
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if filepath.Ext(ref) == ".png" || filepath.Ext(ref) == ".jpg" || filepath.Ext(ref) == ".webp" {
			if resolvedWithin(filepath.Join(root, "tasks", worker.ID, "work"), ref) {
				if info, err := os.Stat(ref); err == nil && info.Mode().IsRegular() && info.Size() <= maxArtifactServeBytes {
					imageURL := "/api/v1/assessments/" + url.PathEscape(id) + "/artifact?path=" + url.QueryEscape(ref)
					images = append(images, imageURL)
					if filepath.Base(ref) == "browser-live.png" {
						browserImage = imageURL
					}
				}
			}
		}
		if filepath.Base(ref) == "browser-live.png" && browser == nil {
			browser = readBrowserPreview(filepath.Join(root, "tasks", worker.ID, "work"), filepath.Join(filepath.Dir(ref), "browser-live.json"))
		}
	}
	var action, stdout, stderr string
	if request.URL.Query().Get("browser") != "1" {
		action = worker.Action
		stdout = outputTail(root, worker.ExecutionLog, ".stdout")
		stderr = outputTail(root, worker.ExecutionLog, ".stderr")
	}
	writeJSON(w, http.StatusOK, struct {
		Phase        string          `json:"phase"`
		Action       string          `json:"action"`
		Stdout       string          `json:"stdout"`
		Stderr       string          `json:"stderr"`
		Images       []string        `json:"images"`
		Browser      *browserPreview `json:"browser,omitempty"`
		BrowserImage string          `json:"browser_image,omitempty"`
	}{worker.Phase, action, stdout, stderr, images, browser, browserImage})
}

func readBrowserPreview(work, path string) *browserPreview {
	if !resolvedWithin(work, path) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var preview browserPreview
	if json.Unmarshal(data, &preview) != nil {
		return nil
	}
	parsed, err := url.Parse(preview.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		preview.URL = ""
	} else {
		preview.URL = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}).String()
	}
	return &preview
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
