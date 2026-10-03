package webapp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

type analysisSourceLocation struct {
	Repository  string               `json:"repository"`
	Revision    string               `json:"revision"`
	Path        string               `json:"path"`
	StartLine   int                  `json:"start_line"`
	EndLine     int                  `json:"end_line,omitempty"`
	ArtifactRef string               `json:"artifact_ref"`
	ArtifactURL string               `json:"artifact_url,omitempty"`
	Lines       []analysisSourceLine `json:"lines,omitempty"`
}

type analysisSourceLine struct {
	Number    int    `json:"number"`
	Text      string `json:"text"`
	Highlight bool   `json:"highlight,omitempty"`
}

func analysisSources(locations []assessment.SourceLocation) []analysisSourceLocation {
	var out []analysisSourceLocation
	for _, location := range locations {
		if strings.TrimSpace(location.Repository) == "" || strings.TrimSpace(location.Revision) == "" || !filepath.IsLocal(location.Path) || location.StartLine < 1 || location.ArtifactRef == "" {
			continue
		}
		out = append(out, analysisSourceLocation{
			Repository: location.Repository, Revision: location.Revision, Path: location.Path,
			StartLine: location.StartLine, EndLine: location.EndLine, ArtifactRef: location.ArtifactRef,
		})
	}
	return out
}

func registeredSourceArtifacts(state assessment.State) map[string]bool {
	refs := map[string]bool{}
	for _, result := range state.Results {
		for _, evidence := range result.Evidence {
			for _, ref := range evidence.ArtifactRefs {
				refs[ref] = true
			}
		}
	}
	return refs
}

// The preview is read from the registered local artifact, never from
// model-authored source text or an arbitrary path named by the finding.
func hydrateSourceLocations(root string, artifacts map[string]bool, findings []analysisFinding) {
	for i := range findings {
		for j := range findings[i].SourceLocations {
			source := &findings[i].SourceLocations[j]
			if !artifacts[source.ArtifactRef] || !slices.Contains(findings[i].Evidence, source.ArtifactRef) || !resolvedWithin(root, source.ArtifactRef) {
				continue
			}
			info, err := os.Stat(source.ArtifactRef)
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxArtifactServeBytes {
				continue
			}
			source.ArtifactURL = artifactURL(findings[i].SessionID, source.ArtifactRef)
			if info.Size() > 1024*1024 {
				continue
			}
			data, err := os.ReadFile(source.ArtifactRef)
			if err != nil || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
				continue
			}
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			start := max(1, source.StartLine-3)
			end := source.EndLine
			if end < source.StartLine {
				end = source.StartLine
			}
			end = min(len(lines), end+3, start+39)
			for line := start; line <= end; line++ {
				source.Lines = append(source.Lines, analysisSourceLine{Number: line, Text: strings.TrimSuffix(lines[line-1], "\r"), Highlight: line >= source.StartLine && line <= max(source.StartLine, source.EndLine)})
			}
		}
	}
}
