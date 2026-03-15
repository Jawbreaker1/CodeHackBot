# Roadmap

This file is directional, not executable.

Rules:
- `ROADMAP.md` describes future phases at a high level.
- `ROADMAP.md` must not contain detailed task lists.
- `TASKS.md` is authoritative for current and next-phase execution.
- When a future phase becomes active, its detailed task list is created in `TASKS.md`, not here.

## Planned Phases

### Phase 1 — Minimal Worker Loop
- Build the smallest interactive worker loop from `docs/architecture.md`
- Validate with real LLM runs

### Phase 2 — Context And Memory Foundations
- Add inspectable context construction
- Add minimal memory-bank support without complex reconciliation

### Phase 3 — Reporting Foundations
- Build human-readable, evidence-first reporting
- Support OWASP-style output

### Phase 4 — Orchestrator Foundations
- Build run-level coordination on the same worker loop
- Add task contracts, validation gate, and operator-facing orchestrator UI

### Phase 5 — Capability Reintroduction
- Add optional capabilities only after the core loop is stable
- Reintroduce runbooks, CVE handling, and other extensions carefully
