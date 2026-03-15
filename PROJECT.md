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
- `docs/roe/public-test-targets.md`
- `docs/runbooks/acceptance-gates.md`
- `docs/architecture.md`

Historical material lives in `docs/archive/` and is not authoritative.

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

- Build instructions for the rebuild root will be reintroduced once the new Go module and entrypoints exist.
- The legacy snapshot remains buildable from `legacy/` if needed for reference.

## Test

- Test instructions for the rebuild root will be reintroduced with the new module.

## Working Rules

- Prefer deletion over patching on this branch.
- Do not implement new behavior that conflicts with `docs/architecture.md`.
- Validate major implementation slices with real LLM runs.
- Keep files modular and refactor early when ownership becomes unclear.
