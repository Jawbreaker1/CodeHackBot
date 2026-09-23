package webapp

import (
	"context"
	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
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

func TestAutomaticPermissionModesStartProposedTasksWithoutPlanPrompt(t *testing.T) {
	plan := assessment.Decision{Tasks: []assessment.Task{{ID: "discovery"}, {ID: "research"}}}
	for _, mode := range []approval.Mode{approval.DangerousOnly, approval.FullAccess} {
		r := &run{permissionMode: mode}
		review, err := r.reviewPlanWait(context.Background(), plan)
		if err != nil || len(review.TaskIDs) != 2 || review.TaskIDs[0] != "discovery" || review.TaskIDs[1] != "research" || r.plan != nil {
			t.Fatalf("mode %s: review=%+v pending=%v err=%v", mode, review, r.plan, err)
		}
	}
}

func TestChangingApprovalModeReleasesCoveredPendingWork(t *testing.T) {
	s := NewServer(Config{RepoRoot: t.TempDir()})
	current, err := s.newRun("fixture", "test approvals", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	low := &pendingApproval{ID: "low", request: approval.Request{Summary: "read", Target: "fixture", Impact: "read only", Risk: "low"}, result: make(chan approval.Decision, 1)}
	high := &pendingApproval{ID: "high", request: approval.Request{Summary: "modify", Target: "fixture", Impact: "changes file", Risk: "dangerous"}, result: make(chan approval.Decision, 1)}
	plan := &pendingPlan{ID: "plan", plan: assessment.Decision{Tasks: []assessment.Task{{ID: "inspect"}}}, result: make(chan assessment.PlanReview, 1)}
	current.approvals[low.ID] = low
	current.approvals[high.ID] = high
	current.plan = plan
	httpServer := httptest.NewServer(s)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id + "/permissions"
	view := postJSON[assessmentView](t, path, permissionRequest{Mode: approval.DangerousOnly, Acknowledge: true})
	if view.PermissionMode != approval.DangerousOnly || len(view.PendingApprovals) != 1 || view.PendingApprovals[0].ID != high.ID || view.PendingPlan != nil {
		t.Fatalf("dangerous-only did not release covered pending work: %+v", view)
	}
	if decision := <-low.result; decision != approval.DecisionApproveSession {
		t.Fatal(decision)
	}
	if reviewed := <-plan.result; len(reviewed.TaskIDs) != 1 || reviewed.TaskIDs[0] != "inspect" {
		t.Fatalf("pending plan not selected: %+v", reviewed)
	}
	view = postJSON[assessmentView](t, path, permissionRequest{Mode: approval.FullAccess, Acknowledge: true})
	if view.PermissionMode != approval.FullAccess || len(view.PendingApprovals) != 0 {
		t.Fatalf("full access left a pending action: %+v", view)
	}
	if decision := <-high.result; decision != approval.DecisionApproveSession {
		t.Fatal(decision)
	}
}
