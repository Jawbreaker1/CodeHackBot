# Legacy Snapshot

This directory contains the pre-rebuild implementation snapshot.

Rules:
- `legacy/` is reference code, not the target architecture.
- New implementation work should happen at the repo root, not inside `legacy/`.
- Reuse from `legacy/` should be deliberate and selective, with clear justification.

Contents moved here:
- old Go module files
- old `cmd/` tree
- old `internal/` tree
- old config, prompts, scripts, and assets

The rebuild branch uses `docs/architecture.md` as the source of truth for what replaces this snapshot.
