package workerloop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxDeclaredArtifactBytes = 64 << 20

// registerArtifacts turns model-declared output paths into evidence only after
// the approved action has completed. Paths stay inside the worker workspace;
// the runtime never scans a directory or treats arbitrary files as evidence.
func registerArtifacts(workspace string, declared []string) ([]string, error) {
	if len(declared) == 0 {
		return nil, nil
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact workspace: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact workspace: %w", err)
	}
	seen := map[string]bool{}
	refs := make([]string, 0, len(declared))
	for _, raw := range declared {
		value := strings.TrimSpace(raw)
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return refs, fmt.Errorf("resolve artifact %q: %w", value, err)
		}
		if !withinWorkspace(root, path) {
			return refs, fmt.Errorf("artifact %q is outside the worker workspace", value)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return refs, fmt.Errorf("artifact %q is unavailable: %w", value, err)
		}
		if !withinWorkspace(root, resolved) {
			return refs, fmt.Errorf("artifact %q resolves outside the worker workspace", value)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return refs, fmt.Errorf("stat artifact %q: %w", value, err)
		}
		if !info.Mode().IsRegular() {
			return refs, fmt.Errorf("artifact %q is not a regular file", value)
		}
		if info.Size() > maxDeclaredArtifactBytes {
			return refs, fmt.Errorf("artifact %q exceeds the %d MiB limit", value, maxDeclaredArtifactBytes>>20)
		}
		canonical, err := filepath.Abs(resolved)
		if err != nil {
			return refs, fmt.Errorf("resolve artifact %q: %w", value, err)
		}
		if !seen[canonical] {
			seen[canonical] = true
			refs = append(refs, canonical)
		}
	}
	return refs, nil
}

func withinWorkspace(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
}
