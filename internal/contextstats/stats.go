package contextstats

import (
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/tokenutil"
)

type SectionStat struct {
	Name         string
	Chars        int
	Lines        int
	ApproxTokens int
}

type PacketStats struct {
	TotalChars        int
	TotalLines        int
	ApproxTotalTokens int
	SectionCount      int
	Sections          []SectionStat
}

func Build(rendered string, sections []ctxpacket.RenderedSection) PacketStats {
	stats := PacketStats{
		TotalChars:        len(rendered),
		TotalLines:        countLines(rendered),
		ApproxTotalTokens: ApproxTokens(rendered),
		SectionCount:      len(sections),
		Sections:          make([]SectionStat, 0, len(sections)),
	}
	for _, section := range sections {
		content := strings.TrimSpace(section.Content)
		stats.Sections = append(stats.Sections, SectionStat{
			Name:         section.Name,
			Chars:        len(content),
			Lines:        countLines(content),
			ApproxTokens: ApproxTokens(content),
		})
	}
	return stats
}

func ApproxTokens(text string) int {
	return tokenutil.ApproxTokens(text)
}

func countLines(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
