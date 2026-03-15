package approval

import (
	"context"
	"strings"
	"testing"
)

func TestPromptApproverApproveOnce(t *testing.T) {
	var out strings.Builder
	approver := &PromptApprover{Reader: strings.NewReader("t\n"), Writer: &out}
	decision, err := approver.Approve(context.Background(), Request{Command: "ls -la"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if decision != DecisionApproveOnce {
		t.Fatalf("decision = %q", decision)
	}
	if !strings.Contains(out.String(), "Approve execution?") {
		t.Fatalf("prompt output missing header: %q", out.String())
	}
}

func TestPromptApproverApproveSessionPersists(t *testing.T) {
	var out strings.Builder
	approver := &PromptApprover{Reader: strings.NewReader("a\n"), Writer: &out}
	decision, err := approver.Approve(context.Background(), Request{Command: "ls -la"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if decision != DecisionApproveSession {
		t.Fatalf("decision = %q", decision)
	}

	decision, err = approver.Approve(context.Background(), Request{Command: "pwd"})
	if err != nil {
		t.Fatalf("second Approve() error = %v", err)
	}
	if decision != DecisionApproveSession {
		t.Fatalf("second decision = %q", decision)
	}
}

func TestPromptApproverDeny(t *testing.T) {
	approver := &PromptApprover{Reader: strings.NewReader("n\n"), Writer: &strings.Builder{}}
	decision, err := approver.Approve(context.Background(), Request{Command: "ls -la"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if decision != DecisionDeny {
		t.Fatalf("decision = %q", decision)
	}
}

func TestPromptApproverInvalidChoice(t *testing.T) {
	approver := &PromptApprover{Reader: strings.NewReader("maybe\n"), Writer: &strings.Builder{}}
	_, err := approver.Approve(context.Background(), Request{Command: "ls -la"})
	if err == nil {
		t.Fatal("Approve() error = nil, want error")
	}
}

func TestPromptApproverEmptyInput(t *testing.T) {
	approver := &PromptApprover{Reader: strings.NewReader(""), Writer: &strings.Builder{}}
	_, err := approver.Approve(context.Background(), Request{Command: "ls -la"})
	if err == nil || !strings.Contains(err.Error(), "approval input required") {
		t.Fatalf("Approve() error = %v, want empty-input error", err)
	}
}
