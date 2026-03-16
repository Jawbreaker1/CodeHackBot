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

## Implementation Discipline

Before coding a non-trivial implementation slice, state:
- `Objective`
- `Architecture anchor`
- `Why this is not a patch`
- `Validation plan`

If these cannot be stated clearly, stop and reassess before changing code.
