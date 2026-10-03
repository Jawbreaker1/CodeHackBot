package main

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
	"github.com/Jawbreaker1/CodeHackBot/internal/webapp"
)

func TestManagedBridgeStartsForWebProfileAndCleansUp(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	profiles := []webapp.ModelProfile{{Provider: "subscription", ManagedBridge: true}}
	closeBridge, err := attachManagedBridge(context.Background(), profiles)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridge()
	profile := profiles[0]
	token, err := localauth.Read(profile.TokenFile)
	if err != nil || profile.BaseURL == "" {
		t.Fatalf("managed profile missing its local bridge: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, profile.BaseURL+"/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("managed bridge status = %d", response.StatusCode)
	}
	closeBridge()
	if _, err := os.Stat(profile.TokenFile); !os.IsNotExist(err) {
		t.Fatalf("temporary bridge token remained after shutdown: %v", err)
	}
}
