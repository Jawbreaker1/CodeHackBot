package llmclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
)

func TestBridgeTokenRequiresLoopbackAndNeverFollowsRedirect(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	if err := localauth.Create(file); err != nil {
		t.Fatal(err)
	}
	client := Client{BaseURL: "https://api.openai.com/v1", Model: "test", AuthTokenFile: file}
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("remote token was not rejected: %v", err)
	}
	var called bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	client.BaseURL = redirect.URL
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}); err == nil {
		t.Fatal("redirect succeeded")
	}
	if called {
		t.Fatal("sent bridge token to redirected endpoint")
	}
}
