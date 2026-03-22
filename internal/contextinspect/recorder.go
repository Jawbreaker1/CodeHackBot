package contextinspect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextstats"
)

// Recorder writes human-readable context packet snapshots for live diagnosis.
type Recorder struct {
	Dir string
}

// Capture writes one context snapshot for a given step and stage.
func (r Recorder) Capture(step int, stage string, packet ctxpacket.WorkerPacket) error {
	if step <= 0 {
		return fmt.Errorf("step must be positive")
	}
	if stage == "" {
		return fmt.Errorf("stage is required")
	}
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("step-%03d-%s.txt", step, stage))
	rendered := packet.Render() + "\n"
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	metaPath := filepath.Join(r.Dir, fmt.Sprintf("step-%03d-%s-meta.txt", step, stage))
	if err := os.WriteFile(metaPath, []byte(renderMeta(path, rendered, packet.RenderSections())), 0o644); err != nil {
		return fmt.Errorf("write snapshot metadata: %w", err)
	}
	return nil
}

func renderMeta(snapshotPath, rendered string, sections []ctxpacket.RenderedSection) string {
	stats := contextstats.Build(rendered, sections)
	lines := []string{
		"[snapshot]",
		"path: " + snapshotPath,
		fmt.Sprintf("total_chars: %d", stats.TotalChars),
		fmt.Sprintf("total_lines: %d", stats.TotalLines),
		fmt.Sprintf("approx_total_tokens: %d", stats.ApproxTotalTokens),
		fmt.Sprintf("section_count: %d", stats.SectionCount),
		"",
		"[sections]",
	}
	for _, section := range stats.Sections {
		lines = append(lines,
			fmt.Sprintf("%s: chars=%d lines=%d approx_tokens=%d", section.Name, section.Chars, section.Lines, section.ApproxTokens),
		)
	}
	return strings.Join(lines, "\n") + "\n"
}
