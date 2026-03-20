# Repository Guidelines

## Purpose

BirdHackBot is an AI-assisted security testing project for authorized lab environments.

Authorization and scope rules live in `AGENTS.md`.
The rebuild source of truth is `docs/architecture.md`.

## Active Documentation

Current active docs are intentionally minimal:
- `AGENTS.md`
- `PROJECT.md`
- `README.md`
- `TASKS.md`
- `ROADMAP.md`
- `DISCOVERIES.md`
- `docs/roe/public-test-targets.md`
- `docs/runbooks/acceptance-gates.md`
- `docs/architecture.md`

Historical material lives in `docs/archive/` and is not authoritative.
The `legacy/` tree is also non-authoritative: it is preserved as historical reference only and reflects the pre-rebuild codebase that became too complex and difficult to reason about.

## Repository Structure

- `legacy/`: pre-rebuild implementation snapshot, including the old Go module
- `cmd/`: rebuild implementation root for new entrypoints
- `internal/`: rebuild implementation root for new packages
- `config/`: rebuild runtime defaults
- `docs/`: active governance docs
- `docs/archive/`: historical reference only
- `sessions/`: local session artifacts and evidence
- `scripts/`: local helper scripts

## Build

- `go build -buildvcs=false ./cmd/birdhackbot`
- `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
- The legacy snapshot remains buildable from `legacy/` if needed for reference.

## Test

- Rebuild-root tests now exist for the implemented core packages.
- Real LLM validation remains required for major behavior slices.
- Repeated live validations can be run with `scripts/repeat_worker_run.sh`.

## Working Rules

- Prefer deletion over patching on this branch.
- Do not implement new behavior that conflicts with `docs/architecture.md`.
- Validate major implementation slices with real LLM runs.
- Keep files modular and refactor early when ownership becomes unclear.
- Do not treat `legacy/` or old main-branch behavior as design truth for the rebuild; use `docs/architecture.md`, `TASKS.md`, and active docs instead.

## Locked Invariants

These are stable project truths and must be re-anchored before making non-trivial behavior changes.

- The interactive CLI worker is the primary product surface.
- Direct single-command execution paths are development/debugging support only and must not become the main behavioral truth of the system.
- The runtime should guide and enforce hard boundaries, not replace LLM reasoning with hardcoded workflow logic.
- Do not add scenario-specific guardrails, tool-specific steering, hidden workflow phases, or fallback logic that acts like a second planner.
- Use the LLM for task logic and adaptive behavior to the greatest extent possible; keep runtime-owned logic minimal and generic.
- `constraints` and richer policy/permission systems are intentionally deferred until the core mechanics are sound.
- Every real session must define target boundaries clearly enough for the runtime to enforce scope honestly, but that does not justify adding heavy scope-inference or action-target policy machinery before the core loop is sound.

Canonical live validation scenarios:

- `secret.zip`: recover the password for the protected ZIP, extract its contents, and produce evidence-backed reporting.
- `192.168.50.1`: analyze the authorized lab router for weaknesses with reproducible evidence.

Testing discipline:

- Major behavior slices require live LLM validation, not only local tests.
- Repeated live runs are the default for behavioral conclusions because outputs are non-deterministic.
- Use 3 runs per scenario unless the check is explicitly labeled as a smoke test.
- Smoke/debug runs must never be presented as acceptance evidence for the canonical scenarios.
- Live validation is not a pass unless the actual context snapshots and persisted session state are inspected and found sound.

## Implementation Discipline

Before coding a non-trivial implementation slice, state:
- `Objective`
- `Architecture anchor`
- `Why this is not a patch`
- `Validation plan`

If these cannot be stated clearly, stop and reassess before changing code.
