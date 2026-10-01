package webapp

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func codexHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func (s *Server) subscriptionLogin(w http.ResponseWriter, r *http.Request) {
	if !localDebugRequest(r) || !sameOrigin(r) {
		http.Error(w, "ChatGPT sign-in requires the local BirdHackBot UI", http.StatusForbidden)
		return
	}
	configured := false
	for _, profile := range s.config.Profiles {
		configured = configured || profile.Provider == "subscription"
	}
	if !configured {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Configure a ChatGPT model profile before signing in."})
		return
	}
	switch r.URL.Path {
	case "/api/v1/subscription/login/status":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, s.login.Status())
	case "/api/v1/subscription/login/start":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		writeJSON(w, http.StatusOK, s.login.Start())
	case "/api/v1/subscription/login/cancel":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		writeJSON(w, http.StatusOK, s.login.Cancel())
	default:
		http.NotFound(w, r)
	}
}

func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host && u.User == nil
}
