package webapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
)

func TestConfiguredProfilesRouteAndRestoreEntireClient(t *testing.T) {
	type request struct {
		Model           string `json:"model"`
		ReasoningEffort string `json:"reasoning_effort"`
		MaxTokens       int    `json:"max_tokens"`
	}
	daybreakCalls := make(chan request, 2)
	qwenCalls := make(chan request, 2)
	fixture := func(calls chan request) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body request
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			calls <- body
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"reply\":\"Ready to plan\",\"proposal\":null,\"tool\":null}"}}],"usage":{"total_tokens":1}}`)
		}))
	}
	daybreak := fixture(daybreakCalls)
	defer daybreak.Close()
	qwen := fixture(qwenCalls)
	defer qwen.Close()
	token := filepath.Join(t.TempDir(), "bridge-token")
	if err := localauth.Create(token); err != nil {
		t.Fatal(err)
	}
	config := Config{RepoRoot: t.TempDir(), SessionsRoot: filepath.Join(t.TempDir(), "sessions"), DefaultProfile: "daybreak", Profiles: []ModelProfile{
		{ID: "daybreak", Label: "Daybreak Blue", Provider: "subscription", BaseURL: daybreak.URL + "/v1", Model: "gpt-daybreak-blue-latest", TokenFile: token},
		{ID: "qwen38", Label: "Qwen 3.8", Provider: "local", BaseURL: qwen.URL + "/v1", Model: "qwen/qwen3.8-27b", ReasoningEffort: "low", MaxInputBytes: llmclient.Qwen38LabInputByteLimit, MaxOutputTokens: 32768, RequestTimeoutSeconds: 600},
	}}
	server := NewServer(config)
	app := httptest.NewServer(server)
	defer app.Close()
	var catalog struct {
		ProfilesEnabled bool          `json:"profiles_enabled"`
		Models          []modelOption `json:"models"`
	}
	catalog = getJSON[struct {
		ProfilesEnabled bool          `json:"profiles_enabled"`
		Models          []modelOption `json:"models"`
	}](t, app.URL+"/api/v1/models")
	if !catalog.ProfilesEnabled || len(catalog.Models) != 2 || catalog.Models[1].Model != "qwen/qwen3.8-27b" {
		t.Fatalf("model menu = %+v", catalog)
	}
	first := getJSON[intakeView](t, app.URL+"/api/v1/intake")
	if first.ModelProfile != "daybreak" {
		t.Fatalf("default intake = %+v", first)
	}
	_ = postJSON[intakeView](t, app.URL+"/api/v1/intake/"+first.ID+"/messages", intakeMessageRequest{Text: "Plan a scoped test"})
	if call := <-daybreakCalls; call.Model != "gpt-daybreak-blue-latest" || call.ReasoningEffort != "" {
		t.Fatalf("default provider request = %+v", call)
	}
	second := getJSON[intakeView](t, app.URL+"/api/v1/intake")
	second = postJSON[intakeView](t, app.URL+"/api/v1/intake/"+second.ID+"/model", modelRequest{Profile: "qwen38"})
	if second.ModelProfile != "qwen38" || second.Model != "qwen/qwen3.8-27b" {
		t.Fatalf("selected profile = %+v", second)
	}
	if got := server.getIntake(second.ID).client.InputByteLimit(); got != llmclient.Qwen38LabInputByteLimit {
		t.Fatalf("selected Qwen input ceiling = %d", got)
	}
	if status := postStatus(t, app.URL+"/api/v1/intake/"+second.ID+"/model", modelRequest{Profile: "unknown"}); status != http.StatusConflict {
		t.Fatalf("unknown profile status = %d", status)
	}
	_ = postJSON[intakeView](t, app.URL+"/api/v1/intake/"+second.ID+"/messages", intakeMessageRequest{Text: "Plan the same scoped test"})
	if call := <-qwenCalls; call.Model != "qwen/qwen3.8-27b" || call.ReasoningEffort != "low" || call.MaxTokens != 32768 {
		t.Fatalf("local provider request = %+v", call)
	}
	if status := postStatus(t, app.URL+"/api/v1/intake/"+second.ID+"/model", modelRequest{Profile: "daybreak"}); status != http.StatusConflict {
		t.Fatalf("mid-conversation provider change status = %d", status)
	}
	restored := httptest.NewServer(NewServer(config))
	defer restored.Close()
	view := getJSON[intakeView](t, restored.URL+"/api/v1/intake/"+second.ID)
	if view.ModelProfile != "qwen38" || view.Model != "qwen/qwen3.8-27b" {
		t.Fatalf("restored profile = %+v", view)
	}
	_ = postJSON[intakeView](t, restored.URL+"/api/v1/intake/"+second.ID+"/messages", intakeMessageRequest{Text: "Continue the scoped test"})
	if call := <-qwenCalls; call.Model != "qwen/qwen3.8-27b" || call.ReasoningEffort != "low" {
		t.Fatalf("restored provider request = %+v", call)
	}
	withoutQwen := config
	withoutQwen.Profiles = config.Profiles[:1]
	missing := NewServer(withoutQwen).getIntake(second.ID)
	if missing == nil || missing.client.BaseURL != "" || missing.client.Model != "qwen/qwen3.8-27b" {
		t.Fatal("removed profile was silently routed through the default provider")
	}
	draft := postJSON[assessmentView](t, app.URL+"/api/v1/assessments", createRequest{Customer: "profile-test", Goal: "inspect synthetic fixture", Scope: "local fixture only"})
	draft = postJSON[assessmentView](t, app.URL+"/api/v1/assessments/"+draft.ID+"/model", modelRequest{Profile: "qwen38"})
	if draft.ModelProfile != "qwen38" || draft.Model != "qwen/qwen3.8-27b" {
		t.Fatalf("draft assessment model = %+v", draft)
	}
	active := NewServer(config)
	run := active.getRun(draft.ID)
	if run == nil || run.client.BaseURL != qwen.URL+"/v1" {
		t.Fatal("draft assessment did not retain its provider on restart")
	}
	run.status = "running"
	if err := active.changeRunProfile(run, "daybreak"); err == nil || run.client.BaseURL != qwen.URL+"/v1" {
		t.Fatal("active assessment changed provider in place")
	}
}

func TestModelProfileFileRejectsInvalidDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	data := `{"default":"missing","profiles":[{"id":"qwen38","label":"Qwen","provider":"local","base_url":"http://127.0.0.1:1234/v1","model":"qwen/qwen3.8-27b"}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModelProfiles(path); err == nil {
		t.Fatal("accepted a missing default profile")
	}
	data = `{"default":"qwen38","profiles":[{"id":"qwen38","label":"Qwen","provider":"local","base_url":"http://127.0.0.1:1234/v1","model":"qwen/qwen3.8-27b","reasoning_effort":"low"}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := LoadModelProfiles(path)
	if err != nil || file.Default != "qwen38" || file.Profiles[0].client().ReasoningEffort != "low" {
		t.Fatalf("load valid profile = %+v, %v", file, err)
	}
}
