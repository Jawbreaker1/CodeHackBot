package assessment

import (
	"encoding/json"
	"fmt"
	"os"
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
	if artifact.Version != 1 || strings.TrimSpace(artifact.Repository) == "" || strings.TrimSpace(artifact.Revision) == "" || !filepath.IsLocal(artifact.Path) || filepath.Clean(artifact.Path) != artifact.Path || len(artifact.Lines) == 0 || len(artifact.Lines) > 300 {
		return SourceArtifact{}, fmt.Errorf("source artifact metadata or line count is invalid")
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
			if !artifacts[location.ArtifactRef] || !slices.Contains(finding.Evidence, location.ArtifactRef) {
				continue
			}
			artifact, err := readSourceArtifact(location.ArtifactRef)
			if err != nil {
				continue
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
			return nil, fmt.Errorf("source artifact does not include cited line %d", line)
		}
	}
	return artifact.Lines, nil
}
