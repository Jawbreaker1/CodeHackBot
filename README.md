# CodeHackBot Rebuild

This branch is a controlled rebuild of BirdHackBot/CodeHackBot.

## Current State

- The old implementation has been isolated under `legacy/`.
- The rebuild source of truth is `docs/architecture.md`.
- Active execution planning lives in `TASKS.md`.
- Future high-level phase direction lives in `ROADMAP.md`.
- Lessons and future-phase notes live in `DISCOVERIES.md`.

## Active Docs

- `AGENTS.md`
- `PROJECT.md`
- `README.md`
- `TASKS.md`
- `ROADMAP.md`
- `DISCOVERIES.md`
- `docs/architecture.md`
- `docs/roe/public-test-targets.md`
- `docs/runbooks/acceptance-gates.md`

## Legacy Code

The pre-rebuild codebase is preserved under `legacy/`.

Rules:
- do not treat `legacy/` as the active implementation
- do not mix new rebuild code into `legacy/`
- reuse from `legacy/` only deliberately and selectively

## Rebuild Approach

The rebuild follows these rules:
- keep the core small
- prefer deletion over patching
- let the LLM own problem solving
- keep context generous and inspectable
- validate every major slice with real LLM runs

## Current Implementation Plan

See `TASKS.md` for the executable plan.

Near-term focus:
1. minimal worker loop
2. context and memory foundations
3. reporting foundations
4. orchestrator rebuilt on the same worker loop

## Build And Test

The rebuild root now exists as a fresh Go module with minimal entrypoint stubs.

Current build commands:
- `go build -buildvcs=false ./cmd/birdhackbot`
- `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`

Tests for the rebuild root will be added with the first implemented behavior slices.

The legacy snapshot can still be inspected and built from `legacy/` if needed for reference.
