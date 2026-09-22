package subscription

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
)

// Handler implements only the text completion contract used by BirdHackBot.
func Handler(provider *Provider, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !localauth.LoopbackHost(host) || r.Header.Get("Origin") != "" {
			writeError(w, 403, "local clients only")
			return
		}
		if token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			writeError(w, 401, "bridge token required")
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			writeError(w, 404, "unknown endpoint")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, 405, "POST required")
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<20))
		decoder.DisallowUnknownFields()
		var input Request
		if decoder.Decode(&input) != nil {
			writeError(w, 400, "invalid request; only model, text messages, temperature, and max_tokens are supported")
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			writeError(w, 400, "expected one JSON request")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		output, err := provider.Complete(ctx, input)
		if err != nil {
			var apiErr *APIError
			switch {
			case errors.As(err, &apiErr):
				if apiErr.RetryAfter != "" {
					w.Header().Set("Retry-After", apiErr.RetryAfter)
				}
				writeError(w, apiErr.Status, apiErr.Message)
			case ctx.Err() != nil:
				writeError(w, 504, "subscription request canceled or timed out")
			default:
				writeError(w, 502, "subscription response unavailable or incomplete")
			}
			return
		}
		_ = json.NewEncoder(w).Encode(output)
	})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message, "type": "subscription_bridge_error"}})
}
