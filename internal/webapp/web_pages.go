package webapp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

// Browser pages are observations from a registered Playwright run. They are
// not a crawl of the target, a completeness claim, or assessment findings.
type analysisWebPage struct {
	SessionID      string `json:"session_id"`
	TaskID         string `json:"task_id"`
	URL            string `json:"url"`
	ScreenshotURL  string `json:"screenshot_url,omitempty"`
	ScreenshotPath string `json:"-"`
	Comment        string `json:"comment,omitempty"`
}

type analysisWebTransition struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	From      string `json:"from"`
	To        string `json:"to"`
}

type browserAnchor struct {
	TaskID string
	Path   string
}

type browserManifest struct {
	Version int `json:"version"`
	Pages   []struct {
		URL        string `json:"url"`
		Screenshot string `json:"screenshot"`
	} `json:"pages"`
	Transitions []struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"transitions"`
}

type webNote struct {
	TaskID  string `json:"task_id"`
	URL     string `json:"url"`
	Comment string `json:"comment"`
}

func browserAnchors(state assessment.State) []browserAnchor {
	var anchors []browserAnchor
	seen := map[string]bool{}
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.ArtifactRefs {
				if filepath.Base(ref) != "browser-live.png" || seen[ref] {
					continue
				}
				seen[ref] = true
				anchors = append(anchors, browserAnchor{TaskID: result.Task.ID, Path: ref})
			}
		}
	}
	return anchors
}

func observedURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}).String()
}

func readBrowserManifest(work, anchor string) *browserManifest {
	if !resolvedWithin(work, anchor) {
		return nil
	}
	path := filepath.Join(filepath.Dir(anchor), "browser-pages.json")
	if !resolvedWithin(work, path) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var manifest browserManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Version != 1 || len(manifest.Pages) > 40 || len(manifest.Transitions) > 120 {
		return nil
	}
	return &manifest
}

func browserAnalysis(root, sessionID string, anchors []browserAnchor) ([]analysisWebPage, []analysisWebTransition) {
	var pages []analysisWebPage
	var transitions []analysisWebTransition
	notes := readWebNotes(root)
	for _, anchor := range anchors {
		work := filepath.Join(root, "tasks", anchor.TaskID, "work")
		if !pathWithin(root, work) || !resolvedWithin(work, anchor.Path) {
			continue
		}
		manifest := readBrowserManifest(work, anchor.Path)
		if manifest == nil {
			// Earlier sessions have a single live preview, without a journey.
			preview := readBrowserPreview(work, filepath.Join(filepath.Dir(anchor.Path), "browser-live.json"))
			if info, err := os.Stat(anchor.Path); preview != nil && preview.URL != "" && err == nil && info.Mode().IsRegular() && info.Size() <= maxArtifactServeBytes {
				pages = append(pages, analysisWebPage{SessionID: sessionID, TaskID: anchor.TaskID, URL: preview.URL, ScreenshotURL: artifactURL(sessionID, anchor.Path), ScreenshotPath: anchor.Path, Comment: notes[noteKey(anchor.TaskID, preview.URL)]})
			}
			continue
		}
		observed := map[string]bool{}
		for _, page := range manifest.Pages {
			cleanURL := observedURL(page.URL)
			if cleanURL == "" || observed[cleanURL] {
				continue
			}
			observed[cleanURL] = true
			entry := analysisWebPage{SessionID: sessionID, TaskID: anchor.TaskID, URL: cleanURL, Comment: notes[noteKey(anchor.TaskID, cleanURL)]}
			filename := page.Screenshot
			if filepath.Base(filename) == filename && strings.HasPrefix(filename, "browser-page-") && strings.HasSuffix(filename, ".png") && len(filename) <= 24 {
				imagePath := filepath.Join(filepath.Dir(anchor.Path), filename)
				if resolvedWithin(work, imagePath) {
					if info, err := os.Stat(imagePath); err == nil && info.Mode().IsRegular() && info.Size() <= maxArtifactServeBytes {
						entry.ScreenshotPath = imagePath
						entry.ScreenshotURL = artifactURL(sessionID, imagePath)
					}
				}
			}
			pages = append(pages, entry)
		}
		for _, edge := range manifest.Transitions {
			from, to := observedURL(edge.From), observedURL(edge.To)
			if observed[from] && observed[to] && from != to {
				transitions = append(transitions, analysisWebTransition{SessionID: sessionID, TaskID: anchor.TaskID, From: from, To: to})
			}
		}
	}
	return pages, transitions
}

func artifactURL(sessionID, path string) string {
	return "/api/v1/assessments/" + url.PathEscape(sessionID) + "/artifact?path=" + url.QueryEscape(path)
}

func noteKey(taskID, pageURL string) string { return taskID + "\n" + pageURL }

func readWebNotes(root string) map[string]string {
	notes := map[string]string{}
	path := filepath.Join(root, "analysis-web-notes.json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 512*1024 || !resolvedWithin(root, path) {
		return notes
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return notes
	}
	var entries []webNote
	if json.Unmarshal(data, &entries) != nil {
		return notes
	}
	for _, entry := range entries {
		notes[noteKey(entry.TaskID, entry.URL)] = entry.Comment
	}
	return notes
}

func (r *run) saveWebNote(input webNote) error {
	input.Comment = strings.TrimSpace(input.Comment)
	if utf8.RuneCountInString(input.Comment) > 2000 {
		return fmt.Errorf("comment is too long")
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	view := r.analysis()
	found := false
	for _, page := range view.WebPages {
		if page.TaskID == input.TaskID && page.URL == input.URL {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("page was not observed in this assessment")
	}
	r.mu.RLock()
	root := r.root
	r.mu.RUnlock()
	var entries []webNote
	for key, comment := range readWebNotes(root) {
		parts := strings.SplitN(key, "\n", 2)
		if len(parts) == 2 && !(parts[0] == input.TaskID && parts[1] == input.URL) && comment != "" {
			entries = append(entries, webNote{TaskID: parts[0], URL: parts[1], Comment: comment})
		}
	}
	if input.Comment != "" {
		entries = append(entries, input)
	}
	if len(entries) > 100 {
		return fmt.Errorf("too many page comments")
	}
	sort.Slice(entries, func(i, j int) bool {
		return noteKey(entries[i].TaskID, entries[i].URL) < noteKey(entries[j].TaskID, entries[j].URL)
	})
	return atomicWriteJSON(filepath.Join(root, "analysis-web-notes.json"), entries)
}
