package webapp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

// artifactView lists saved evidence files without copying their contents into
// the session response. Execution logs remain in Activity and worker details.
type artifactView struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	TaskID      string `json:"task_id,omitempty"`
	Description string `json:"description,omitempty"`
	Bytes       int64  `json:"bytes"`
}

func assessmentArtifacts(root, sessionID string, state assessment.State, workers map[string]workerView) []artifactView {
	var artifacts []artifactView
	seen := make(map[string]bool)
	logs := make(map[string]bool)
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.LogRefs {
				logs[ref] = true
			}
		}
	}
	for _, worker := range workers {
		for _, evidence := range worker.Evidence {
			for _, ref := range evidence.LogRefs {
				logs[ref] = true
			}
		}
	}
	add := func(ref, taskID, description string) {
		if !filepath.IsAbs(ref) || seen[ref] || logs[ref] || executionLogPath(root, ref) || !resolvedWithin(root, ref) {
			return
		}
		info, err := os.Stat(ref)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxArtifactServeBytes {
			return
		}
		seen[ref] = true
		kind := "file"
		if imageMIME(ref) != "" {
			kind = "image"
		}
		artifacts = append(artifacts, artifactView{
			Name: filepath.Base(ref), URL: artifactURL(sessionID, ref), Kind: kind,
			TaskID: taskID, Description: description, Bytes: info.Size(),
		})
	}
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.ArtifactRefs {
				add(ref, result.Task.ID, "")
			}
		}
	}
	for taskID, worker := range workers {
		for _, evidence := range worker.Evidence {
			for _, ref := range evidence.ArtifactRefs {
				add(ref, taskID, "")
			}
		}
	}
	if anchors := browserAnchors(state); len(anchors) > 0 {
		pages, _ := browserAnalysis(root, sessionID, anchors)
		for _, page := range pages {
			if page.ScreenshotPath != "" {
				add(page.ScreenshotPath, page.TaskID, strings.TrimSpace(page.URL))
			}
		}
	}
	return artifacts
}

func executionLogPath(root, ref string) bool {
	rel, err := filepath.Rel(root, ref)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) >= 4 && parts[0] == "tasks" && parts[2] == "logs"
}
