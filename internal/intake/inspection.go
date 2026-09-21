package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
)

// Inspection is a small local observation capability, not another worker or
// arbitrary command executor. Target interaction remains owned by assessment.
type Inspection struct {
	Workspace   string
	EvidenceDir string
	Policy      string
	Approver    approval.Approver
	Emit        func(assessment.Event)
}

type ToolCall struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

type Observation struct {
	Tool        ToolCall `json:"tool"`
	Status      string   `json:"status"`
	Data        any      `json:"data,omitempty"`
	Error       string   `json:"error,omitempty"`
	EvidenceRef string   `json:"evidence_ref,omitempty"`
}

const inspectionScope = "Local observation only: directory entry names/types within the configured workspace and this host's kernel network metadata. No file contents, device probes, network packets, mutations, or arbitrary commands."

func (i *Inspection) Scope() string { return inspectionScope }

func (i *Inspection) Run(ctx context.Context, call ToolCall) (Observation, error) {
	observation := Observation{Tool: call, Status: "failed"}
	root, err := filepath.Abs(i.Workspace)
	if err != nil {
		return observation, err
	}
	var relative string
	switch call.Name {
	case "list_directory":
		target := call.Path
		if target == "" {
			target = "."
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		}
		relative, err = filepath.Rel(root, target)
		if err != nil || !filepath.IsLocal(relative) {
			observation.Error = "path must stay inside the configured workspace"
			return observation, nil
		}
	case "local_network":
		if call.Path != "" {
			observation.Error = "local_network accepts no path or target"
			return observation, nil
		}
	default:
		observation.Error = "unknown local observation tool"
		return observation, nil
	}
	if i.Approver == nil {
		return observation, fmt.Errorf("local observation requires an approver")
	}
	encoded, _ := json.Marshal(call)
	description := string(encoded)
	i.emit(assessment.Event{Kind: "action_proposed", Goal: inspectionScope, Action: description, Message: "Inspect local environment"})
	decision, err := i.Approver.Approve(ctx, approval.Request{Command: description, Cwd: root})
	if err != nil {
		return observation, err
	}
	if decision != approval.DecisionApproveOnce && decision != approval.DecisionApproveSession {
		observation.Status = "denied"
		i.emit(assessment.Event{Kind: "blocked", Message: "Local observation denied; nothing executed"})
		return observation, nil
	}
	if err := ctx.Err(); err != nil {
		return observation, err
	}
	if err := os.MkdirAll(i.EvidenceDir, 0700); err != nil {
		return observation, err
	}
	i.emit(assessment.Event{Kind: "execution_started", Action: description, Message: "Reading approved local metadata"})
	switch call.Name {
	case "list_directory":
		observation.Data, err = listDirectory(root, relative)
	case "local_network":
		observation.Data, err = i.localNetwork(ctx, root)
	}
	if err != nil {
		observation.Error = err.Error()
	} else {
		observation.Status = "ok"
	}
	evidence, err := os.CreateTemp(i.EvidenceDir, "observation-*.json")
	if err != nil {
		return observation, err
	}
	observation.EvidenceRef = evidence.Name()
	writeErr := json.NewEncoder(evidence).Encode(observation)
	closeErr := evidence.Close()
	if writeErr != nil {
		return observation, writeErr
	}
	if closeErr != nil {
		return observation, closeErr
	}
	data, _ := json.Marshal(observation)
	i.emit(assessment.Event{Kind: "execution_finished", Action: description, Message: "Local observation recorded", EvidenceCount: 1, Evidence: &assessment.EvidenceView{Command: description, ExitStatus: observation.Status, Summary: string(data), LogRefs: []string{evidence.Name()}}})
	return observation, nil
}

func (i *Inspection) emit(e assessment.Event) {
	e.TaskID = "coordinator"
	if i.Emit != nil {
		i.Emit(e)
	}
}

func listDirectory(workspace, relative string) (any, error) {
	// os.Root rejects symlinks that escape the workspace, including nested ones.
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(257)
	if err != nil && err != io.EOF {
		return nil, err
	}
	truncated := len(entries) > 256
	if truncated {
		entries = entries[:256]
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	type entry struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	listing := make([]entry, 0, len(entries))
	for _, e := range entries {
		kind := "file"
		if e.IsDir() {
			kind = "directory"
		} else if e.Type()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		listing = append(listing, entry{Name: e.Name(), Type: kind})
	}
	return struct {
		Path      string  `json:"path"`
		Entries   []entry `json:"entries"`
		Truncated bool    `json:"truncated"`
	}{filepath.Join(workspace, relative), listing, truncated}, nil
}

func (i *Inspection) localNetwork(ctx context.Context, root string) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// These fixed iproute2 queries read kernel metadata. No probe or destination
	// argument is accepted from the model. JSON goes back to the model unchanged.
	out := make(map[string]json.RawMessage)
	for _, query := range []string{"address", "route", "neigh"} {
		result, err := (execx.Executor{LogDir: i.EvidenceDir}).Run(ctx, execx.Action{Command: "ip", Args: []string{"-json", query, "show"}, Cwd: root})
		if err != nil {
			return out, fmt.Errorf("local %s query: %w", query, err)
		}
		if len(result.ArtifactRefs) == 0 {
			return out, fmt.Errorf("local %s query produced no recorded output", query)
		}
		data, err := os.ReadFile(result.ArtifactRefs[0])
		if err != nil {
			return out, err
		}
		if len(data) > 64*1024 || !json.Valid(data) {
			return out, fmt.Errorf("local %s metadata exceeds the observation limit or is not JSON", query)
		}
		out[query] = data
	}
	return out, nil
}
