package webapp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubscriptionSignInSettingsEndpoint(t *testing.T) {
	server := NewServer(Config{RepoRoot: t.TempDir(), Profiles: []ModelProfile{{ID: "daybreak", Label: "Daybreak", Provider: "subscription", BaseURL: "http://127.0.0.1:8787/v1", Model: "gpt-daybreak-blue-latest", TokenFile: "/nonexistent"}}})
	web := httptest.NewServer(server)
	defer web.Close()
	response, err := http.Get(web.URL + "/api/v1/subscription/login/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(data), `"status":"idle"`) {
		t.Fatalf("local status: %d %s", response.StatusCode, data)
	}
	request, _ := http.NewRequest(http.MethodPost, web.URL+"/api/v1/subscription/login/start", nil)
	request.Header.Set("Origin", "https://foreign.example")
	rejected, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer rejected.Body.Close()
	if rejected.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin sign-in status = %d", rejected.StatusCode)
	}
	request, _ = http.NewRequest(http.MethodPost, web.URL+"/api/v1/subscription/login/start", nil)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	rejected, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer rejected.Body.Close()
	if rejected.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site sign-in status = %d", rejected.StatusCode)
	}
}
