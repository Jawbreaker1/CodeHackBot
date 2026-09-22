package webapp

import (
	"context"
	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionPermissionsPersistAndDefaultIndependently(t *testing.T) {
	config := Config{RepoRoot: t.TempDir()}
	s := NewServer(config)
	current, err := s.newRun("fixture", "test approvals", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(s)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id
	view := postJSON[assessmentView](t, path+"/permissions", permissionRequest{Mode: approval.DangerousOnly, Acknowledge: true})
	if view.PermissionMode != approval.DangerousOnly {
		t.Fatal(view.PermissionMode)
	}
	a := &runApprover{run: current, taskID: "check"}
	d, err := a.Approve(context.Background(), approval.Request{Summary: "read title", Target: "local fixture", Impact: "read only", Risk: "low"})
	if err != nil || d != approval.DecisionApproveSession || len(current.approvals) != 0 {
		t.Fatalf("low-risk execution was not auto-approved: %s %v", d, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, _ = a.Approve(ctx, approval.Request{Risk: "dangerous"})
	if d != approval.DecisionDeny {
		t.Fatalf("dangerous execution bypassed operator: %s", d)
	}
	restored := NewServer(config)
	if restored.loadErr != nil {
		t.Fatal(restored.loadErr)
	}
	if restored.getRun(current.id).permissionMode != approval.DangerousOnly {
		t.Fatal("permission mode lost on restart")
	}
	other, err := restored.newRun("fixture", "another test", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	if other.view("").PermissionMode != approval.EveryExecution {
		t.Fatal("session permission leaked to new session")
	}
}

func TestAutomaticPermissionModesRequireAcknowledgement(t *testing.T) {
	for _, mode := range []approval.Mode{approval.DangerousOnly, approval.FullAccess} {
		r := httptest.NewRequest("POST", "/permissions", strings.NewReader(`{"mode":"`+string(mode)+`"}`))
		w := httptest.NewRecorder()
		if _, ok := readPermission(w, r); ok || w.Code != 400 {
			t.Fatalf("mode %s changed without confirmation", mode)
		}
	}
}
