# Roadmap

This file is directional, not executable.

Rules:
- `ROADMAP.md` describes future phases at a high level.
- `ROADMAP.md` must not contain detailed task lists.
- `TASKS.md` is authoritative for current and next-phase execution.
- When a future phase becomes active, its detailed task list is created in `TASKS.md`, not here.

## Direction Updated 2026-09-19

The primary product is the multi-agent orchestrator. The worker is its reusable execution engine. Phase names below align with `TASKS.md`; earlier roadmap numbering is superseded.

## Phases

### Phase 1 — Minimal Worker Loop
- Build the smallest interactive worker loop from `docs/architecture.md`
- Validate with real LLM runs

### Phase 2 — Active Context Quality
- Establish inspectable context and provenance-backed execution truth

### Phase 3 — Minimal Planning
- Establish worker planning, interaction modes, and observable task lifecycle
- Retain unfinished worker work as supporting debt for orchestration

### Phase 4 — Orchestrator And Subscription Access (Current)
- Make run-level coordination the main operator experience
- Reuse workers through bounded task contracts, parallel scheduling, and shared evidence
- Deliver validation and evidence-first reporting as part of the assessment
- Add a local REST bridge for subscription-backed OpenAI access with explicit usage limits
- Establish a matched Codex baseline and trustworthy evaluation artifacts

### Phase 5 — Source-Assisted Assessment (Next)
- Connect software identification, matching source acquisition, code investigation, and live validation
- Measure value against Codex on clean, independently scored lab fixtures

### Phase 6 — Capability Expansion
- Expand browser/API workflows, current vulnerability references, and optional advisory capabilities based on measured gaps
- Keep extensions removable and preserve one execution/evidence contract
