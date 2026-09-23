package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/contextinspect"
)

func TestContextDebuggerListsExactTurnsAndAppliesOptionalOmissions(t *testing.T) {
	root := t.TempDir()
	dir := contextDir(root, "research-1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(root, "coordinator-01-request.json"):   `[{"role":"system","content":"coordinator"}]`,
		filepath.Join(dir, "step-001-pre-llm.txt"):           "[behavior_frame]\nfixture\n",
		filepath.Join(dir, "step-001-pre-llm-sections.json"): `[{"Name":"behavior_frame","Content":"fixture"},{"Name":"strategy_guidance","Content":"guide"}]`,
		filepath.Join(dir, "step-001-request.json"):          `[{"role":"system","content":"worker"}]`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{}
	current := &run{root: root}
	call := func(action, method, url, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		response := httptest.NewRecorder()
		server.contextDebug(response, req, current, action)
		return response
	}
	listed := call("index", http.MethodGet, "/context/index", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"task":"research-1"`) || !strings.Contains(listed.Body.String(), `"kind":"coordinator"`) {
		t.Fatalf("index: %d %s", listed.Code, listed.Body.String())
	}
	item := call("item", http.MethodGet, "/context/item?kind=worker&task=research-1&turn=1", "")
	if item.Code != http.StatusOK || !strings.Contains(item.Body.String(), `"strategy_guidance"`) || !strings.Contains(item.Body.String(), `"messages"`) {
		t.Fatalf("item: %d %s", item.Code, item.Body.String())
	}
	denied := call("omissions", http.MethodPut, "/context/omissions?task=research-1", `{"sections":["behavior_frame"]}`)
	if denied.Code != http.StatusBadRequest {
		t.Fatalf("protected section could be omitted: %d", denied.Code)
	}
	updated := call("omissions", http.MethodPut, "/context/omissions?task=research-1", `{"sections":["strategy_guidance"]}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("set omission: %d %s", updated.Code, updated.Body.String())
	}
	sections, err := (contextinspect.Recorder{Dir: dir}).ReadOmissions()
	if err != nil || len(sections) != 1 || sections[0] != "strategy_guidance" {
		t.Fatalf("omission not available to worker: %v %v", sections, err)
	}
	var out map[string]any
	if err := json.Unmarshal(updated.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if got := call("item", http.MethodGet, "/context/item?kind=worker&task=..&turn=1", ""); got.Code != http.StatusBadRequest {
		t.Fatalf("traversal accepted: %d", got.Code)
	}
	remote := httptest.NewRequest(http.MethodGet, "/context/index", nil)
	remote.RemoteAddr = "192.0.2.15:12345"
	blocked := httptest.NewRecorder()
	server.contextDebug(blocked, remote, current, "index")
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("remote context request accepted: %d", blocked.Code)
	}
}

func TestContextDebuggerSplitsOwnLegacyPacketMarkers(t *testing.T) {
	sections := splitLegacyPacket("[behavior_frame]\nsystem\n\n[strategy_guidance]\nguide\n\n[operator_state]\nready\n")
	if len(sections) != 3 || sections[1].Name != "strategy_guidance" || sections[1].Content != "guide" {
		t.Fatalf("legacy packet was not split into ordered sections: %+v", sections)
	}
}
