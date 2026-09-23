package webapp

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

var workerDebugSections = map[string]bool{
	"plan_history": true, "recent_conversation": true, "older_conversation_summary": true,
	"running_summary": true, "relevant_recent_results": true, "memory_bank_retrievals": true,
	"strategy_guidance": true, "capability_inputs": true, "context_notes": true,
}

type contextTurn struct {
	Kind    string `json:"kind"`
	Task    string `json:"task,omitempty"`
	Turn    int    `json:"turn"`
	HasFull bool   `json:"has_full_request"`
}

func validDebugTask(task string) bool {
	if task == "" || len(task) > 48 || task == "." || task == ".." || filepath.Base(task) != task {
		return false
	}
	for _, ch := range task {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func readDebugFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("context record is not a bounded regular file")
	}
	return os.ReadFile(path)
}

func contextDir(root, task string) string {
	return filepath.Join(root, "tasks", task, "context")
}

func safeContextDir(root, task string) (string, error) {
	if !validDebugTask(task) {
		return "", fmt.Errorf("invalid worker task")
	}
	for _, path := range []string{filepath.Join(root, "tasks", task), contextDir(root, task)} {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("worker context is not a directory")
		}
	}
	return contextDir(root, task), nil
}

func localDebugRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Legacy snapshots predate the structured section sidecar. Their section
// markers come from our own stable packet renderer, not from model output.
func splitLegacyPacket(data string) []ctxpacket.RenderedSection {
	names := map[string]bool{}
	for _, section := range (ctxpacket.WorkerPacket{}).RenderSections() {
		names[section.Name] = true
	}
	var sections []ctxpacket.RenderedSection
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if names[name] && (len(sections) == 0 || sections[len(sections)-1].Name != name) {
				sections = append(sections, ctxpacket.RenderedSection{Name: name})
				continue
			}
		}
		if len(sections) != 0 {
			sections[len(sections)-1].Content += line + "\n"
		}
	}
	if len(sections) == 0 {
		return []ctxpacket.RenderedSection{{Name: "legacy_packet_snapshot", Content: data}}
	}
	for i := range sections {
		sections[i].Content = strings.TrimSpace(sections[i].Content)
	}
	return sections
}

func (s *Server) contextDebug(w http.ResponseWriter, r *http.Request, current *run, action string) {
	if !localDebugRequest(r) {
		writeError(w, http.StatusForbidden, "context debugger is available only through local loopback")
		return
	}
	current.mu.RLock()
	root := current.root
	current.mu.RUnlock()
	switch action {
	case "index":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		turns := []contextTurn{}
		matches, _ := filepath.Glob(filepath.Join(root, "coordinator-*-request.json"))
		for _, path := range matches {
			name := filepath.Base(path)
			if strings.Contains(name, "correction") {
				continue
			}
			value := strings.TrimSuffix(strings.TrimPrefix(name, "coordinator-"), "-request.json")
			if n, err := strconv.Atoi(value); err == nil {
				turns = append(turns, contextTurn{Kind: "coordinator", Turn: n, HasFull: true})
			}
		}
		taskDirs, _ := filepath.Glob(filepath.Join(root, "tasks", "*", "context"))
		for _, dir := range taskDirs {
			task := filepath.Base(filepath.Dir(dir))
			if _, err := safeContextDir(root, task); err != nil {
				continue
			}
			files, _ := filepath.Glob(filepath.Join(dir, "step-*-pre-llm.txt"))
			for _, path := range files {
				name := filepath.Base(path)
				value := strings.TrimSuffix(strings.TrimPrefix(name, "step-"), "-pre-llm.txt")
				if n, err := strconv.Atoi(value); err == nil {
					_, fullErr := os.Stat(filepath.Join(dir, fmt.Sprintf("step-%03d-request.json", n)))
					turns = append(turns, contextTurn{Kind: "worker", Task: task, Turn: n, HasFull: fullErr == nil})
				}
			}
		}
		sort.Slice(turns, func(i, j int) bool {
			if turns[i].Kind != turns[j].Kind {
				return turns[i].Kind < turns[j].Kind
			}
			if turns[i].Task != turns[j].Task {
				return turns[i].Task < turns[j].Task
			}
			return turns[i].Turn < turns[j].Turn
		})
		writeJSON(w, http.StatusOK, map[string]any{"turns": turns, "editable_sections": workerDebugSections})
	case "item":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		kind, task := r.URL.Query().Get("kind"), r.URL.Query().Get("task")
		turn, err := strconv.Atoi(r.URL.Query().Get("turn"))
		if err != nil || turn < 1 || turn > 999 {
			writeError(w, http.StatusBadRequest, "invalid context turn")
			return
		}
		var requestPath, sectionsPath, snapshotPath string
		if kind == "coordinator" && task == "" {
			requestPath = filepath.Join(root, fmt.Sprintf("coordinator-%02d-request.json", turn))
		} else if kind == "worker" && validDebugTask(task) {
			dir, err := safeContextDir(root, task)
			if err != nil {
				writeError(w, http.StatusNotFound, "worker context not found")
				return
			}
			requestPath = filepath.Join(dir, fmt.Sprintf("step-%03d-request.json", turn))
			sectionsPath = filepath.Join(dir, fmt.Sprintf("step-%03d-pre-llm-sections.json", turn))
			snapshotPath = filepath.Join(dir, fmt.Sprintf("step-%03d-pre-llm.txt", turn))
		} else {
			writeError(w, http.StatusBadRequest, "invalid context source")
			return
		}
		value := map[string]any{"kind": kind, "task": task, "turn": turn}
		if data, err := readDebugFile(requestPath); err == nil {
			var messages any
			if json.Unmarshal(data, &messages) == nil {
				value["messages"] = messages
				value["request_bytes"] = len(data)
			}
		}
		if kind == "worker" {
			if data, err := readDebugFile(sectionsPath); err == nil {
				var sections []ctxpacket.RenderedSection
				if json.Unmarshal(data, &sections) == nil {
					value["sections"] = sections
				}
			} else if data, err := readDebugFile(snapshotPath); err == nil {
				value["sections"] = splitLegacyPacket(string(data))
			}
		}
		if value["messages"] == nil && value["sections"] == nil {
			writeError(w, http.StatusNotFound, "context turn not found")
			return
		}
		writeJSON(w, http.StatusOK, value)
	case "omissions":
		task := r.URL.Query().Get("task")
		if !validDebugTask(task) {
			writeError(w, http.StatusBadRequest, "invalid worker task")
			return
		}
		dir, err := safeContextDir(root, task)
		if err != nil {
			writeError(w, http.StatusNotFound, "worker context not found")
			return
		}
		path := filepath.Join(dir, "debug-omissions.json")
		if r.Method == http.MethodGet {
			data, err := readDebugFile(path)
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusOK, map[string]any{"sections": []string{}})
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodGet+", "+http.MethodPut)
			return
		}
		var input struct {
			Sections []string `json:"sections"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if len(input.Sections) > len(workerDebugSections) {
			writeError(w, http.StatusBadRequest, "too many context sections")
			return
		}
		seen := map[string]bool{}
		for _, section := range input.Sections {
			if !workerDebugSections[section] || seen[section] {
				writeError(w, http.StatusBadRequest, "section cannot be omitted from model input")
				return
			}
			seen[section] = true
		}
		data, _ := json.Marshal(input)
		tmp, err := os.CreateTemp(filepath.Dir(path), ".debug-omissions-*")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer os.Remove(tmp.Name())
		_, err = tmp.Write(data)
		if err == nil {
			err = tmp.Chmod(0600)
		}
		if err == nil {
			err = tmp.Close()
		} else {
			_ = tmp.Close()
		}
		if err == nil {
			err = os.Rename(tmp.Name(), path)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, input)
	default:
		http.NotFound(w, r)
	}
}
