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

// Inspection offers fixed, read-only coordinator observations. Broader target
// work and arbitrary commands remain owned by assessment workers.
type Inspection struct {
	Workspace   string
	EvidenceDir string
	Policy      string
	Connected   bool
	Approver    approval.Approver
	Emit        func(assessment.Event)
}

type ToolCall struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
	URL  string `json:"url,omitempty"`
}

type Observation struct {
	Tool        ToolCall `json:"tool"`
	Status      string   `json:"status"`
	Data        any      `json:"data,omitempty"`
	Error       string   `json:"error,omitempty"`
	EvidenceRef string   `json:"evidence_ref,omitempty"`
}

const localInspectionScope = "Local observation: directory entry names/types within the configured workspace, this host's operating-system metadata, and kernel network metadata. Host metadata is limited to fixed read-only queries (uname, hostname, and /etc/os-release when present). No arbitrary commands, credential files, other file contents, device probes, or mutations."
const connectedInspectionScope = " Connected observation also offers DNS lookup for a named public domain and one bounded HTTP/HTTPS page fetch from a public address. No credentials, custom headers, redirects to other hosts, private-network fetches, active scans, or mutations."

func (i *Inspection) Scope() string {
	if i.Connected {
		return localInspectionScope + connectedInspectionScope
	}
	return localInspectionScope + " External DNS and web fetch are unavailable in air-gapped mode."
}

func (i *Inspection) Run(ctx context.Context, call ToolCall) (Observation, error) {
	observation := Observation{Tool: call, Status: "failed"}
	root, err := filepath.Abs(i.Workspace)
	if err != nil {
		return observation, err
	}
	var relative, target, summary, impact string
	target = root
	impact = "Reads local metadata without changing files or probing a target."
	switch call.Name {
	case "list_directory":
		if call.Host != "" || call.URL != "" {
			observation.Error = "list_directory accepts only a path"
			return observation, nil
		}
		target = call.Path
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
		if call.Path != "" || call.Host != "" || call.URL != "" {
			observation.Error = "local_network accepts no path or target"
			return observation, nil
		}
	case "host_system":
		if call.Path != "" || call.Host != "" || call.URL != "" {
			observation.Error = "host_system accepts no path or target"
			return observation, nil
		}
	case "dns_lookup":
		if !i.Connected {
			observation.Error = "public DNS lookup is unavailable in air-gapped mode"
			return observation, nil
		}
		if call.Path != "" || call.URL != "" {
			observation.Error = "dns_lookup accepts only a host"
			return observation, nil
		}
		call.Host, err = publicDomain(call.Host)
		if err != nil {
			observation.Error = err.Error()
			return observation, nil
		}
		target, summary, impact = call.Host, "Look up public DNS for "+call.Host, "Queries DNS for the named domain. It does not scan the site or change it."
	case "web_fetch":
		if !i.Connected {
			observation.Error = "public web fetch is unavailable in air-gapped mode"
			return observation, nil
		}
		if call.Path != "" || call.Host != "" {
			observation.Error = "web_fetch accepts only a URL"
			return observation, nil
		}
		parsed, parseErr := publicPageURL(call.URL)
		if parseErr != nil {
			observation.Error = parseErr.Error()
			return observation, nil
		}
		call.URL = parsed.String()
		target, summary, impact = call.URL, "Read public page "+call.URL, "Sends one ordinary HTTP GET to this public page. No login, form submission, or file change."
	default:
		observation.Error = "unknown coordinator observation tool"
		return observation, nil
	}
	observation.Tool = call
	if i.Approver == nil {
		return observation, fmt.Errorf("coordinator observation requires an approver")
	}
	if summary == "" {
		summary = "Inspect local " + call.Name
	}
	encoded, _ := json.Marshal(call)
	description := string(encoded)
	i.emit(assessment.Event{Kind: "action_proposed", Goal: i.Scope(), Action: description, Message: summary})
	decision, err := i.Approver.Approve(ctx, approval.Request{Command: description, Cwd: root, Summary: summary, Target: target, Risk: "low", Impact: impact})
	if err != nil {
		return observation, err
	}
	if decision != approval.DecisionApproveOnce && decision != approval.DecisionApproveSession {
		observation.Status = "denied"
		i.emit(assessment.Event{Kind: "blocked", Message: "Coordinator observation denied; nothing executed"})
		return observation, nil
	}
	if err := ctx.Err(); err != nil {
		return observation, err
	}
	if err := os.MkdirAll(i.EvidenceDir, 0700); err != nil {
		return observation, err
	}
	i.emit(assessment.Event{Kind: "execution_started", Action: description, Message: summary})
	switch call.Name {
	case "list_directory":
		observation.Data, err = listDirectory(root, relative)
	case "local_network":
		observation.Data, err = i.localNetwork(ctx, root)
	case "host_system":
		observation.Data, err = i.hostSystem(ctx, root)
	case "dns_lookup":
		observation.Data, err = lookupPublicDNS(ctx, call.Host)
	case "web_fetch":
		observation.Data, err = fetchPublicPage(ctx, call.URL)
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
	i.emit(assessment.Event{Kind: "execution_finished", Action: description, Message: "Coordinator observation recorded", EvidenceCount: 1, Evidence: &assessment.EvidenceView{Command: description, ExitStatus: observation.Status, Summary: string(data), LogRefs: []string{evidence.Name()}}})
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

type hostSystemObservation struct {
	Kernel       string `json:"kernel"`
	Hostname     string `json:"hostname"`
	Architecture string `json:"architecture"`
	OSRelease    string `json:"os_release,omitempty"`
}

func (i *Inspection) hostSystem(ctx context.Context, root string) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	kernel, err := fixedMetadataCommand(ctx, i.EvidenceDir, root, "uname", "-a")
	if err != nil {
		return nil, fmt.Errorf("uname query: %w", err)
	}
	hostname, err := fixedMetadataCommand(ctx, i.EvidenceDir, root, "hostname")
	if err != nil {
		return nil, fmt.Errorf("hostname query: %w", err)
	}
	architecture, err := fixedMetadataCommand(ctx, i.EvidenceDir, root, "uname", "-m")
	if err != nil {
		return nil, fmt.Errorf("architecture query: %w", err)
	}
	var release string
	if data, readErr := os.ReadFile("/etc/os-release"); readErr == nil {
		if len(data) > 16*1024 {
			return nil, fmt.Errorf("/etc/os-release exceeds the observation limit")
		}
		release = string(data)
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("read /etc/os-release: %w", readErr)
	}
	return hostSystemObservation{Kernel: kernel, Hostname: hostname, Architecture: architecture, OSRelease: release}, nil
}

func fixedMetadataCommand(ctx context.Context, evidenceDir, root, command string, args ...string) (string, error) {
	result, err := (execx.Executor{LogDir: evidenceDir}).Run(ctx, execx.Action{Command: command, Args: args, Cwd: root})
	if err != nil {
		return "", err
	}
	if len(result.ArtifactRefs) == 0 {
		return "", fmt.Errorf("query produced no recorded output")
	}
	data, err := os.ReadFile(result.ArtifactRefs[0])
	if err != nil {
		return "", err
	}
	if len(data) > 16*1024 {
		return "", fmt.Errorf("query output exceeds the observation limit")
	}
	return string(data), nil
}
