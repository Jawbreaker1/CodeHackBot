package guided

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

type preferences struct {
	Provider        string `json:"provider"`
	BaseURL         string `json:"base_url,omitempty"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	MaxInputBytes   int    `json:"max_input_bytes,omitempty"`
}

// SubscriptionInputByteLimit gives Daybreak a larger evidence budget than the
// conservative local-model default while still failing visibly before a
// provider request becomes unbounded.
const SubscriptionInputByteLimit = 128 * 1024

func configureProvider(ctx context.Context, c *Console, path string) (preferences, error) {
	var p preferences
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &p) == nil && p.Model != "" && (p.Provider == "local" || p.Provider == "subscription") {
		answer, err := c.Ask(ctx, fmt.Sprintf("Use saved provider %s / %s (reasoning: %s)? [Y/n]", p.Provider, p.Model, reasoningLabel(p)))
		if err != nil {
			return p, err
		}
		if answer == "" || strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
			return savePreferences(ctx, c, path, p)
		}
	}
	for {
		choice, err := c.Ask(ctx, "Choose model access:\n  1. Local model server\n  2. ChatGPT subscription (selected context is sent to OpenAI)\nSelection [1]")
		if err != nil {
			return p, err
		}
		switch choice {
		case "", "1":
			p.Provider = "local"
		case "2":
			p.Provider = "subscription"
		default:
			c.Print("Choose 1 or 2.\n")
			continue
		}
		break
	}
	p.ReasoningEffort = ""
	p.MaxOutputTokens = 0
	if p.Provider == "local" {
		for {
			endpoint, err := c.Ask(ctx, "Local model server address [http://127.0.0.1:1234/v1]")
			if err != nil {
				return p, err
			}
			if endpoint == "" {
				endpoint = "http://127.0.0.1:1234/v1"
			}
			models, err := listModels(ctx, endpoint)
			if err != nil {
				c.Print("Cannot connect: %v\nCheck that the local server is running and its address is correct. Ctrl-C cancels setup.\n", err)
				continue
			}
			p.BaseURL = strings.TrimRight(endpoint, "/")
			for i, model := range models {
				c.Print("  %d. %s\n", i+1, model)
			}
			for {
				model, err := c.Ask(ctx, "Choose a model number or enter its exact model ID")
				if err != nil {
					return p, err
				}
				if n, err := strconv.Atoi(model); err == nil && n >= 1 && n <= len(models) {
					model = models[n-1]
				}
				if model == "" {
					continue
				}
				p.Model = model
				break
			}
			break
		}
		c.Print("All application model roles will use this server. Full air-gapped operation is not yet validated; third-party tools can still access the network.\n")
	} else {
		model, err := c.Ask(ctx, "Subscription model ID [gpt-daybreak-blue-latest]")
		if err != nil {
			return p, err
		}
		if model == "" {
			model = "gpt-daybreak-blue-latest"
		}
		p.Model, p.BaseURL = model, ""
		p.MaxInputBytes = SubscriptionInputByteLimit
	}
	return savePreferences(ctx, c, path, p)
}

func reasoningLabel(p preferences) string {
	if p.ReasoningEffort == "" || p.ReasoningEffort == "default" {
		return "provider default"
	}
	return p.ReasoningEffort
}

func savePreferences(ctx context.Context, c *Console, path string, p preferences) (preferences, error) {
	if p.Provider == "local" && p.MaxOutputTokens == 0 {
		p.MaxOutputTokens = 32768
	}
	if p.MaxInputBytes == 0 {
		if p.Provider == "subscription" {
			p.MaxInputBytes = SubscriptionInputByteLimit
		} else {
			p.MaxInputBytes = llmclient.DefaultInputByteLimit
		}
	}
	if p.Provider == "local" && p.ReasoningEffort == "" {
		for {
			effort, err := c.Ask(ctx, "Reasoning effort: low, medium, high, xhigh, or default (server setting) [low]")
			if err != nil {
				return p, err
			}
			if effort == "" {
				effort = "low"
			}
			switch effort {
			case "low", "medium", "high", "xhigh", "default":
				p.ReasoningEffort = effort
			default:
				c.Print("Choose a listed setting supported by your model server.\n")
				continue
			}
			break
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return p, err
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return p, err
	}
	return p, nil
}

func listModels(ctx context.Context, endpoint string) ([]string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("enter an http(s) model-server URL without credentials or query parameters")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model server returned %s", resp.Status)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("model list is not a supported response")
	}
	var out []string
	for _, model := range body.Data {
		if model.ID != "" {
			out = append(out, model.ID)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("server has no available models; load a model first")
	}
	return out, nil
}

func startProvider(ctx context.Context, p preferences) (llmclient.Client, func(), error) {
	client := llmclient.Client{BaseURL: p.BaseURL, Model: p.Model}
	client.MaxInputBytes = p.MaxInputBytes
	if p.Provider == "local" {
		client.MaxOutputTokens = p.MaxOutputTokens
		client.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
		if p.ReasoningEffort != "default" {
			client.ReasoningEffort = p.ReasoningEffort
		}
		return client, func() {}, nil
	}
	// Reuse the existing subscription adapter; the application owns its local
	// listener and ephemeral credential, never provider credential copies.
	codexDir := os.Getenv("CODEX_HOME")
	if codexDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return client, nil, err
		}
		codexDir = filepath.Join(home, ".codex")
	}
	auth := &subscription.CodexAuth{Home: codexDir}
	if _, err := auth.Read(); err != nil {
		return client, nil, err
	}
	dir, err := os.MkdirTemp("", "birdhackbot-provider-")
	if err != nil {
		return client, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	tokenPath := filepath.Join(dir, "client-token")
	if err := localauth.Create(tokenPath); err != nil {
		cleanup()
		return client, nil, err
	}
	token, err := localauth.Read(tokenPath)
	if err != nil {
		cleanup()
		return client, nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cleanup()
		return client, nil, err
	}
	server := &http.Server{Handler: subscription.Handler(&subscription.Provider{Auth: auth}, token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 190 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() { _ = server.Serve(listener) }()
	client.BaseURL, client.AuthTokenFile = "http://"+listener.Addr().String()+"/v1", tokenPath
	return client, func() { _ = server.Close(); cleanup() }, nil
}
