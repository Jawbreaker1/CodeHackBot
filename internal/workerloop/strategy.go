package workerloop

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

// loadStrategy keeps a model-selected local guide in the worker's durable
// packet. It is context retrieval, not a target action or assessment evidence.
func loadStrategy(packet *ctxpacket.WorkerPacket, name string) (bool, error) {
	catalog := packet.BehaviorFrame.StrategyCatalogPath
	if catalog == "" || packet.BehaviorFrame.StrategyCatalogText == "" {
		return false, fmt.Errorf("local strategy catalog is unavailable")
	}
	name = filepath.Clean(strings.TrimSpace(name))
	if filepath.IsAbs(name) || name == "." || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || filepath.Ext(name) != ".md" {
		return false, fmt.Errorf("strategy must be a relative Markdown path under the catalog directory")
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(catalog))
	if err != nil {
		return false, fmt.Errorf("resolve strategy catalog directory: %w", err)
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return false, fmt.Errorf("resolve strategy %q: %w", name, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("strategy path leaves the local catalog")
	}
	for _, doc := range packet.StrategyGuidance {
		if doc.Path == path {
			return false, nil
		}
	}
	if len(packet.StrategyGuidance) >= 3 {
		return false, fmt.Errorf("worker already has three loaded strategy guides")
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false, fmt.Errorf("strategy is not a regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil {
		return false, err
	}
	if len(content) > 8192 {
		return false, fmt.Errorf("strategy exceeds the 8 KiB guide limit")
	}
	total := len(content)
	for _, doc := range packet.StrategyGuidance {
		total += len(doc.Content)
	}
	if total > 16384 {
		return false, fmt.Errorf("loaded strategy guidance exceeds 16 KiB")
	}
	packet.StrategyGuidance = append(packet.StrategyGuidance, ctxpacket.StrategyDocument{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(content)), Content: string(content)})
	return true, nil
}
