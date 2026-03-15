package contextinspect

import (
	"fmt"
	"os"
	"path/filepath"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
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
	if err := os.WriteFile(path, []byte(packet.Render()+"\n"), 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}
