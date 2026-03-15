# Rebuild Plan

This task list derives from `docs/architecture.md`.
The architecture document is the source of truth for the rebuild branch.

## Rebuild 0 — Reset
- [x] Aggressively archive historical docs into `docs/archive/`
- [x] Reduce active docs to the minimal set
- [x] Move the pre-rebuild implementation into `legacy/` so old and new code cannot be confused
- [ ] Rewrite `README.md` to match the rebuild branch state
- [x] Write and freeze the v1 baseline architecture document with user review before adoption

## Working Rules
- [ ] No patch-first behavior on this branch
- [ ] No new behavior that conflicts with the architecture document
- [ ] Every major implementation slice must be followed by real LLM validation
- [ ] Prefer deletion over adaptation when both solve the same problem
