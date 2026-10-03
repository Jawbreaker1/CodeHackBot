package assessment

import (
	"encoding/json"
	"fmt"
	"os"
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

func ReadSourceArtifact(location SourceLocation) ([]SourceLine, error) {
	info, err := os.Stat(location.ArtifactRef)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSourceArtifactBytes {
		return nil, fmt.Errorf("source artifact is unavailable or too large")
	}
	data, err := os.ReadFile(location.ArtifactRef)
	if err != nil || !utf8.Valid(data) {
		return nil, fmt.Errorf("source artifact is not valid UTF-8")
	}
	var artifact SourceArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, fmt.Errorf("source artifact is not structured JSON: %w", err)
	}
	if artifact.Version != 1 || artifact.Repository != location.Repository || artifact.Revision != location.Revision || artifact.Path != location.Path || len(artifact.Lines) == 0 || len(artifact.Lines) > 300 {
		return nil, fmt.Errorf("source artifact metadata or line count does not match the finding")
	}
	previous := 0
	covered := map[int]bool{}
	for _, line := range artifact.Lines {
		if line.Number <= previous || len(line.Text) > 4096 {
			return nil, fmt.Errorf("source artifact lines are not bounded and ordered")
		}
		previous = line.Number
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
