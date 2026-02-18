package orchestrator

import (
	"testing"
	"time"
)

func TestApprovalBrokerPolicyMatrix(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	type tc struct {
		name  string
		mode  PermissionMode
		optIn bool
		risk  string
		want  ApprovalDecision
	}
	cases := []tc{
		{"readonly recon", PermissionReadonly, false, string(RiskReconReadonly), ApprovalAllow},
		{"readonly active", PermissionReadonly, false, string(RiskActiveProbe), ApprovalDeny},
		{"default recon", PermissionDefault, false, string(RiskReconReadonly), ApprovalAllow},
		{"default active", PermissionDefault, false, string(RiskActiveProbe), ApprovalNeedsRequest},
		{"default disruptive denied", PermissionDefault, false, string(RiskDisruptive), ApprovalDeny},
		{"all exploit allowed", PermissionAll, false, string(RiskExploitControlled), ApprovalAllow},
		{"all privesc requires approval", PermissionAll, false, string(RiskPrivEsc), ApprovalNeedsRequest},
		{"all disruptive opt-in request", PermissionAll, true, string(RiskDisruptive), ApprovalNeedsRequest},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := NewApprovalBroker(tt.mode, tt.optIn, 10*time.Minute)
			if err != nil {
				t.Fatalf("NewApprovalBroker: %v", err)
			}
			task := task("t1", nil, 1)
			task.RiskLevel = tt.risk
			got, _, _, err := b.EvaluateTask(task, now)
			if err != nil {
				t.Fatalf("EvaluateTask: %v", err)
			}
			if got != tt.want {
				t.Fatalf("decision mismatch: got %s want %s", got, tt.want)
			}
		})
	}
}

func TestApprovalBrokerGrantAndExpiration(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	b, err := NewApprovalBroker(PermissionDefault, false, time.Minute)
	if err != nil {
		t.Fatalf("NewApprovalBroker: %v", err)
	}
	tsk := task("t1", nil, 1)
	tsk.RiskLevel = string(RiskActiveProbe)
	decision, reason, tier, err := b.EvaluateTask(tsk, now)
	if err != nil {
		t.Fatalf("EvaluateTask: %v", err)
	}
	if decision != ApprovalNeedsRequest {
		t.Fatalf("expected request decision, got %s", decision)
	}
	req := b.EnsureRequest("run-1", tsk, tier, reason, now)
	if err := b.Approve(req.ID, ApprovalScopeTask, "tester", "ok", now, time.Minute); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	granted := b.DrainGranted()
	if len(granted) != 1 || granted[0].TaskID != "t1" {
		t.Fatalf("expected granted task t1")
	}

	decision, _, _, err = b.EvaluateTask(tsk, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("EvaluateTask after grant: %v", err)
	}
	if decision != ApprovalAllow {
		t.Fatalf("expected allow after grant, got %s", decision)
	}

	// New request that expires.
	task2 := task("t2", nil, 1)
	task2.RiskLevel = string(RiskActiveProbe)
	decision, reason, tier, err = b.EvaluateTask(task2, now)
	if err != nil {
		t.Fatalf("EvaluateTask task2: %v", err)
	}
	if decision != ApprovalNeedsRequest {
		t.Fatalf("expected request for task2")
	}
	req2 := b.EnsureRequest("run-1", task2, tier, reason, now)
	expired := b.Expire(req2.ExpiresAt.Add(time.Second))
	if len(expired) != 1 || expired[0].TaskID != "t2" {
		t.Fatalf("expected task2 to expire")
	}
	drained := b.DrainExpired()
	if len(drained) != 1 || drained[0].TaskID != "t2" {
		t.Fatalf("expected one drained expired request")
	}
}
