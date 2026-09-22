package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
)

const completedEvent = `data: {"type":"response.completed","response":{"id":"test","status":"completed","output":[{"type":"reasoning"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"type\":\"complete\"}"}]}],"usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11}}}` + "\n\n"

type testAuth struct {
	refreshes  atomic.Int32
	err        error
	refreshErr error
}

func (a *testAuth) Read() (Credentials, error) { return Credentials{"old", "account"}, a.err }
func (a *testAuth) Refresh(ctx context.Context, rejected Credentials) (Credentials, error) {
	a.refreshes.Add(1)
	return Credentials{"new", "account"}, a.refreshErr
}

func TestWorkerClientThroughBridge(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer old" || r.Header.Get("ChatGPT-Account-Id") != "account" {
			t.Error("missing subscription auth")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "chosen-model" || payload["store"] != false || payload["stream"] != true || payload["tool_choice"] != "none" || len(payload["tools"].([]any)) != 0 {
			t.Errorf("unexpected inference payload: %v", payload)
		}
		if payload["instructions"] != "Return JSON" {
			t.Error("lost system instructions")
		}
		if _, exists := payload["temperature"]; exists {
			t.Error("unsupported temperature sent upstream")
		}
		if payload["max_output_tokens"] != float64(1234) {
			t.Errorf("max output was not forwarded: %v", payload["max_output_tokens"])
		}
		fmt.Fprint(w, completedEvent)
	}))
	defer upstream.Close()
	path := filepath.Join(t.TempDir(), "bridge-token")
	if err := localauth.Create(path); err != nil {
		t.Fatal(err)
	}
	token, err := localauth.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	bridge := httptest.NewServer(Handler(&Provider{Auth: &testAuth{}, endpoint: upstream.URL}, token))
	defer bridge.Close()
	client := llmclient.Client{BaseURL: bridge.URL + "/v1", Model: "chosen-model", AuthTokenFile: path, MaxOutputTokens: 1234}
	got, err := client.Complete(context.Background(), []llmclient.Message{{Role: "system", Content: "Return JSON"}, {Role: "user", Content: "Hello"}}, llmclient.ChatOptions{Profile: llmclient.ProfileStructuredControl})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != `{"type":"complete"}` || got.FinishReason != "stop" || !strings.Contains(string(got.Usage), `"total_tokens":11`) {
		t.Fatalf("bad completion: %+v", got)
	}
}

func TestProviderMapsVisualAttachmentsToResponsesInputParts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		items, ok := payload["input"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("input=%#v", payload["input"])
		}
		item := items[0].(map[string]any)
		parts := item["content"].([]any)
		if len(parts) != 3 || parts[0].(map[string]any)["type"] != "input_text" || parts[1].(map[string]any)["type"] != "input_image" || parts[2].(map[string]any)["type"] != "input_file" {
			t.Fatalf("content parts=%#v", parts)
		}
		if !strings.HasPrefix(parts[1].(map[string]any)["image_url"].(string), "data:image/png;base64,") || !strings.HasPrefix(parts[2].(map[string]any)["file_data"].(string), "data:application/pdf;base64,") {
			t.Fatalf("attachment data URLs=%#v", parts)
		}
		fmt.Fprint(w, completedEvent)
	}))
	defer upstream.Close()
	provider := Provider{Auth: &testAuth{}, endpoint: upstream.URL}
	_, err := provider.Complete(context.Background(), Request{Model: "vision-model", Messages: []llmclient.Message{{Role: "user", Content: "inspect", Attachments: []llmclient.Attachment{{Filename: "screen.png", MIMEType: "image/png", Detail: "high", Data: []byte("png")}, {Filename: "report.pdf", MIMEType: "application/pdf", Data: []byte("pdf")}}}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOutputLimitFallbackForBackendWithoutOptionalField(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if calls.Add(1) == 1 {
			if _, ok := payload["max_output_tokens"]; !ok {
				t.Fatal("fixture did not receive the optional output limit")
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, ok := payload["max_output_tokens"]; ok {
			t.Fatal("fallback retained the rejected optional output limit")
		}
		fmt.Fprint(w, completedEvent)
	}))
	defer upstream.Close()
	provider := Provider{Auth: &testAuth{}, endpoint: upstream.URL}
	got, err := provider.Complete(context.Background(), Request{Model: "test", MaxTokens: 128, Messages: []llmclient.Message{{Role: "user", Content: "hello"}}})
	if err != nil || got == nil || calls.Load() != 2 {
		t.Fatalf("fallback completion failed: calls=%d response=%v err=%v", calls.Load(), got, err)
	}
}

func TestBridgeRejectsUnauthorizedAndToolRequests(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	handler := Handler(&Provider{Auth: &testAuth{}, endpoint: upstream.URL}, "local-token")
	for _, tc := range []struct {
		name, auth, origin, remote, body string
		status                           int
	}{
		{"unauthorized", "", "", "127.0.0.1:5", `{}`, 401},
		{"browser", "Bearer local-token", "https://untrusted.example", "127.0.0.1:5", `{}`, 403},
		{"remote", "Bearer local-token", "", "192.0.2.1:5", `{}`, 403},
		{"tools", "Bearer local-token", "", "127.0.0.1:5", `{"model":"x","messages":[],"tools":[{"type":"shell"}]}`, 400},
		{"stream", "Bearer local-token", "", "127.0.0.1:5", `{"model":"x","messages":[],"stream":true}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tc.body))
			req.RemoteAddr = tc.remote
			req.Header.Set("Authorization", tc.auth)
			req.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d; want %d", w.Code, tc.status)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("rejected request reached upstream")
	}
}

func TestRefreshOnceAndLimits(t *testing.T) {
	for _, status := range []int{200, 400, 401, 403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			auth := &testAuth{}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if n == 1 {
					w.WriteHeader(401)
					return
				}
				if r.Header.Get("Authorization") != "Bearer new" {
					t.Error("did not use refreshed credential")
				}
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(status)
				if status == 200 {
					fmt.Fprint(w, completedEvent)
				} else {
					fmt.Fprint(w, `{"error":"sensitive upstream diagnostics"}`)
				}
			}))
			defer server.Close()
			provider := Provider{Auth: auth, endpoint: server.URL}
			_, err := provider.Complete(context.Background(), Request{Model: "test", Messages: []llmclient.Message{{Role: "user", Content: "hi"}}})
			if status == 200 && err != nil {
				t.Fatal(err)
			}
			if status != 200 {
				e, ok := err.(*APIError)
				if !ok || e.Status != status || e.RetryAfter != "120" || strings.Contains(e.Message, "sensitive") {
					t.Fatalf("unexpected error %v", err)
				}
			}
			if calls.Load() != 2 || auth.refreshes.Load() != 1 {
				t.Fatal("unbounded or missing refresh")
			}
		})
	}
}

func TestFailedRefreshStopsWithoutResubmission(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(401) }))
	defer server.Close()
	auth := &testAuth{refreshErr: errors.New("expired refresh token")}
	p := Provider{Auth: auth, endpoint: server.URL}
	_, err := p.Complete(context.Background(), Request{Model: "test", Messages: []llmclient.Message{{Role: "user", Content: "hello"}}})
	var failure *APIError
	if !errors.As(err, &failure) || failure.Status != 401 || calls.Load() != 1 || auth.refreshes.Load() != 1 {
		t.Fatalf("failed refresh retried or succeeded: %v", err)
	}
}

func TestCancellationReachesUpstream(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	bridge := httptest.NewServer(Handler(&Provider{Auth: &testAuth{}, endpoint: server.URL}, "token"))
	defer bridge.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", bridge.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer token")
	done := make(chan error, 1)
	go func() {
		resp, err := bridge.Client().Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not started")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request survived cancellation")
	}
	if err := <-done; err == nil {
		t.Fatal("canceled request succeeded")
	}
}

func TestNoPartialOrToolCompletion(t *testing.T) {
	for _, input := range []string{
		`data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n",
		`data: {"type":"response.incomplete"}` + "\n\n",
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call"}]}}` + "\n\n",
	} {
		if _, err := readCompletion(strings.NewReader(input), "test"); err == nil {
			t.Fatal("non-completion accepted")
		}
	}
}

func TestCompletedItemStreamWithoutRepeatedOutput(t *testing.T) {
	input := `data: {"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}` + "\n\n" +
		`data: {"type":"response.completed","response":{"status":"completed","model":"resolved-model","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}` + "\n\n"
	got, err := readCompletion(strings.NewReader(input), "requested-alias")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got)
	if !strings.Contains(string(encoded), `"content":"hello"`) || got["model"] != "resolved-model" {
		t.Fatalf("lost stream result: %s", encoded)
	}
}

func TestCodexAuthRejectsAPIKeyAndReusesRefreshedFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "auth.json")
	write := func(mode, access string) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"auth_mode": mode, "tokens": map[string]string{"access_token": access, "account_id": "account"}})
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := &CodexAuth{Home: home}
	write("apikey", "api-secret")
	if _, err := a.Read(); err == nil {
		t.Fatal("accepted API-key mode")
	}
	write("chatgpt", "old")
	var calls int
	a.refresh = func(context.Context, string) error { calls++; write("chatgpt", "new"); return nil }
	for i := 0; i < 2; i++ {
		c, err := a.Refresh(context.Background(), Credentials{"old", "account"})
		if err != nil || c.AccessToken != "new" {
			t.Fatalf("refresh: %v", err)
		}
	}
	if calls != 1 {
		t.Fatal("rotated the same rejected token twice")
	}
}

func TestUpstreamRedirectDoesNotReceiveCredentials(t *testing.T) {
	var calls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	p := Provider{Auth: &testAuth{}, endpoint: redirect.URL}
	_, err := p.Complete(context.Background(), Request{Model: "test", Messages: []llmclient.Message{{Role: "user", Content: "hello"}}})
	if err == nil || calls.Load() != 0 {
		t.Fatal("followed upstream redirect")
	}
}
