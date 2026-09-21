package llmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
)

// ModelInfo is the small provider-neutral part of an OpenAI-compatible model
// descriptor needed by the web picker.
type ModelInfo struct {
	ID string `json:"id"`
}

// ListModels asks the already configured endpoint for its model catalog. It
// never sends credentials to a redirected host. Providers without a catalog
// (including some subscription bridges) return their error to the caller so a
// UI can keep the configured model and offer an explicit model ID entry.
func (c Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, fmt.Errorf("base url is required")
	}
	endpoint, err := url.Parse(strings.TrimRight(c.BaseURL, "/") + "/models")
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid model endpoint")
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	if c.AuthTokenFile != "" {
		if endpoint.Scheme != "http" || endpoint.User != nil || !localauth.LoopbackHost(endpoint.Hostname()) {
			return nil, fmt.Errorf("bridge authentication requires an http URL with a literal loopback IP")
		}
		token, err := localauth.Read(c.AuthTokenFile)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		localClient := *httpClient
		localClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		httpClient = &localClient
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("model catalog returned status %s", resp.Status)
		}
		var payload struct {
			Data []ModelInfo `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return nil, fmt.Errorf("decode model catalog: %w", err)
		}
		return payload.Data, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("model catalog returned status %s", resp.Status)
	}
	var payload struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode model catalog: %w", err)
	}
	return payload.Data, nil
}
