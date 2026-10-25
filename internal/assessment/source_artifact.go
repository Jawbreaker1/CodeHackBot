package assessment

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// SourceArtifact is a bounded, line-numbered capture of source code. Workers
// create it from inspected code and register the JSON file as task evidence.
type SourceArtifact struct {
	Version    int          `json:"version"`
	Repository string       `json:"repository"`
	Revision   string       `json:"revision"`
	Path       string       `json:"path"`
	Lines      []SourceLine `json:"lines"`
}

type SourceLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

type SourcePathKind string

const (
	SourceFilePath SourcePathKind = "file"
	SourceWebPath  SourcePathKind = "web"
)

const maxSourceArtifactBytes = 1 << 20

func readSourceArtifact(ref string) (SourceArtifact, error) {
	info, err := os.Stat(ref)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSourceArtifactBytes {
		return SourceArtifact{}, fmt.Errorf("source artifact is unavailable or too large")
	}
	data, err := os.ReadFile(ref)
	if err != nil || !utf8.Valid(data) {
		return SourceArtifact{}, fmt.Errorf("source artifact is not valid UTF-8")
	}
	var artifact SourceArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return SourceArtifact{}, fmt.Errorf("source artifact is not structured JSON: %w", err)
	}
	if artifact.Version != 1 || strings.TrimSpace(artifact.Repository) == "" || strings.TrimSpace(artifact.Revision) == "" {
		return SourceArtifact{}, fmt.Errorf("source artifact version, repository, or revision is invalid")
	}
	if ClassifySourcePath(artifact.Repository, artifact.Path) == "" {
		return SourceArtifact{}, fmt.Errorf("source artifact path must be a clean relative file path, or a root-relative URL path for an HTTP(S) repository")
	}
	if len(artifact.Lines) == 0 || len(artifact.Lines) > 300 {
		return SourceArtifact{}, fmt.Errorf("source artifact line count is invalid")
	}
	previous := 0
	for _, line := range artifact.Lines {
		if line.Number <= previous || len(line.Text) > 4096 {
			return SourceArtifact{}, fmt.Errorf("source artifact lines are not bounded and ordered")
		}
		previous = line.Number
	}
	return artifact, nil
}

// ClassifySourcePath keeps logical source locations separate from the local
// artifact filename. The path is never used to open a file or fetch a URL.
func ClassifySourcePath(repository, sourcePath string) SourcePathKind {
	parsedPath, err := url.Parse(sourcePath)
	if err != nil || parsedPath.Scheme != "" || parsedPath.Host != "" {
		return ""
	}
	if filepath.IsLocal(sourcePath) && filepath.Clean(sourcePath) == sourcePath {
		return SourceFilePath
	}
	repoURL, err := url.Parse(repository)
	if err != nil || (repoURL.Scheme != "http" && repoURL.Scheme != "https") || repoURL.Host == "" {
		return ""
	}
	cleanPath := strings.TrimSuffix(sourcePath, "/")
	if strings.HasPrefix(sourcePath, "/") && !strings.HasPrefix(sourcePath, "//") &&
		!strings.ContainsAny(sourcePath, "\\?#") && (sourcePath == "/" || path.Clean(cleanPath) == cleanPath) {
		return SourceWebPath
	}
	return ""
}

func (location SourceLocation) SourceLabel() string {
	if ClassifySourcePath(location.Repository, location.Path) == SourceWebPath {
		return "Web page"
	}
	return "Source file"
}

// The registered artifact owns provenance metadata. The coordinator selects
// the relevant line range; it need not copy repository labels or hashes from a
// worker summary into its JSON decision.
func normalizeSourceLocations(d *Decision, state State) {
	artifacts := map[string]bool{}
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.ArtifactRefs {
				artifacts[ref] = true
			}
		}
	}
	for i := range d.Findings {
		finding := &d.Findings[i]
		for j := range finding.SourceLocations {
			location := &finding.SourceLocations[j]
			if !artifacts[location.ArtifactRef] {
				continue
			}
			artifact, err := readSourceArtifact(location.ArtifactRef)
			if err != nil {
				continue
			}
			// A source location already cites this registered artifact. Keep the
			// finding's evidence list consistent when the model omits the same
			// reference there, without inventing or accepting unregistered proof.
			if !slices.Contains(finding.Evidence, location.ArtifactRef) {
				finding.Evidence = append(finding.Evidence, location.ArtifactRef)
			}
			location.Repository, location.Revision, location.Path = artifact.Repository, artifact.Revision, artifact.Path
		}
	}
}

func ReadSourceArtifact(location SourceLocation) ([]SourceLine, error) {
	artifact, err := readSourceArtifact(location.ArtifactRef)
	if err != nil {
		return nil, err
	}
	if artifact.Repository != location.Repository || artifact.Revision != location.Revision || artifact.Path != location.Path {
		return nil, fmt.Errorf("source artifact metadata does not match the finding")
	}
	covered := map[int]bool{}
	for _, line := range artifact.Lines {
		covered[line.Number] = true
	}
	end := location.EndLine
	if end == 0 {
		end = location.StartLine
	}
	if location.StartLine < 1 || end < location.StartLine || end-location.StartLine >= 300 {
		return nil, fmt.Errorf("finding source range is invalid")
	}
	for line := location.StartLine; line <= end; line++ {
		if !covered[line] {
			return nil, fmt.Errorf("source artifact %s does not include cited line %d (captured lines: %s); cite a captured range or omit this source location", artifact.Path, line, capturedSourceRanges(artifact.Lines))
		}
	}
	return artifact.Lines, nil
}

func capturedSourceRanges(lines []SourceLine) string {
	if len(lines) == 0 {
		return "none"
	}
	var ranges []string
	start, end := lines[0].Number, lines[0].Number
	appendRange := func() {
		if start == end {
			ranges = append(ranges, fmt.Sprint(start))
		} else {
			ranges = append(ranges, fmt.Sprintf("%d-%d", start, end))
		}
	}
	for _, line := range lines[1:] {
		if line.Number == end+1 {
			end = line.Number
			continue
		}
		appendRange()
		start, end = line.Number, line.Number
	}
	appendRange()
	return strings.Join(ranges, ", ")
}
