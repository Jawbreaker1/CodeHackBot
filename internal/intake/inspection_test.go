package intake

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
)

func TestListDirectoryIsBoundedToWorkspaceAndRecordsEvidence(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("not returned"), 0600); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(t.TempDir(), "evidence")
	inspection := Inspection{Workspace: workspace, EvidenceDir: evidence, Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce}}
	result, err := inspection.Run(context.Background(), ToolCall{Name: "list_directory", Path: "."})
	if err != nil || result.Status != "ok" || result.EvidenceRef == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Path    string `json:"path"`
		Entries []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"entries"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(data, &listing); err != nil || len(listing.Entries) != 1 || listing.Entries[0].Name != "notes.txt" {
		t.Fatalf("listing=%#v", result.Data)
	}
	if _, err := inspection.Run(context.Background(), ToolCall{Name: "list_directory", Path: "../"}); err != nil {
		t.Fatalf("escape should be a typed denial, not a runtime error: %v", err)
	}
}

func TestHostSystemObservationUsesFixedMetadataQueries(t *testing.T) {
	workspace := t.TempDir()
	evidence := filepath.Join(t.TempDir(), "evidence")
	inspection := Inspection{Workspace: workspace, EvidenceDir: evidence, Approver: approval.StaticApprover{Decision: approval.DecisionApproveOnce}}
	result, err := inspection.Run(context.Background(), ToolCall{Name: "host_system"})
	if err != nil || result.Status != "ok" || result.EvidenceRef == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	var host struct {
		Kernel       string `json:"kernel"`
		Hostname     string `json:"hostname"`
		Architecture string `json:"architecture"`
	}
	if err := json.Unmarshal(data, &host); err != nil {
		t.Fatal(err)
	}
	if host.Kernel == "" || host.Hostname == "" || host.Architecture == "" {
		t.Fatalf("host metadata=%s", data)
	}
}
