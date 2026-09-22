package webapp

import (
	"embed"
	"net/http"
)

//go:embed static/index.html
var indexHTML string

//go:embed static/analysis.html
var analysisHTML string

//go:embed static/*.css static/*.js static/*.svg
var uiAssets embed.FS

func serveUI(w http.ResponseWriter, r *http.Request) bool {
	var kind string
	switch r.URL.Path {
	case "/app.css":
		kind = "text/css; charset=utf-8"
	case "/app.js", "/inspector.js", "/analysis.js":
		kind = "text/javascript; charset=utf-8"
	case "/analysis.css":
		kind = "text/css; charset=utf-8"
	case "/logo.svg", "/logo-small.svg", "/wordmark.svg":
		kind = "image/svg+xml"
	default:
		return false
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return true
	}
	data, err := uiAssets.ReadFile("static" + r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
	return true
}
