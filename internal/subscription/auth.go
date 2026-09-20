package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Credentials never enter the worker, its context, or its session state.
type Credentials struct{ AccessToken, AccountID string }

type CredentialSource interface {
	Read() (Credentials, error)
	Refresh(context.Context, Credentials) (Credentials, error)
}

// CodexAuth uses Codex's file credential store. Codex owns login and refresh;
// BirdHackBot does not maintain a competing copy of its refresh tokens.
type CodexAuth struct {
	Home    string
	mu      sync.Mutex
	refresh func(context.Context, string) error
}

func (a *CodexAuth) Read() (Credentials, error) {
	b, err := os.ReadFile(filepath.Join(a.Home, "auth.json"))
	if err != nil {
		return Credentials{}, fmt.Errorf("subscription sign-in unavailable; run codex -c 'cli_auth_credentials_store=\"file\"' login with this CODEX_HOME")
	}
	var stored struct {
		Mode   string  `json:"auth_mode"`
		APIKey *string `json:"OPENAI_API_KEY"`
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(b, &stored) != nil {
		return Credentials{}, fmt.Errorf("invalid Codex credential file; sign in again")
	}
	if stored.Mode != "chatgpt" || (stored.APIKey != nil && *stored.APIKey != "") {
		return Credentials{}, fmt.Errorf("ChatGPT subscription sign-in required; API-key credentials are not supported")
	}
	if stored.Tokens.AccessToken == "" || stored.Tokens.AccountID == "" {
		return Credentials{}, fmt.Errorf("incomplete subscription credentials; sign in again")
	}
	return Credentials{stored.Tokens.AccessToken, stored.Tokens.AccountID}, nil
}

func (a *CodexAuth) Refresh(ctx context.Context, rejected Credentials) (Credentials, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, err := a.Read()
	if err != nil {
		return Credentials{}, err
	}
	if current != rejected {
		return current, nil
	}
	refresh := a.refresh
	if refresh == nil {
		refresh = refreshWithCodex
	}
	if err := refresh(ctx, a.Home); err != nil {
		return Credentials{}, err
	}
	return a.Read()
}
