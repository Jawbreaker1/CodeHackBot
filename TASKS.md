# Sprint Plan

This plan is a living document. Keep tasks small, testable, and tied to artifacts under `sessions/`.

## Sprint 0 — Foundations (done)
- [x] Repo scaffolding: `AGENTS.md`, `PROJECT.md`, `README.md`
- [x] `prompts/system.md` behavior prompt
- [x] `config/default.json` baseline schema
- [x] Minimal Go CLI entrypoint

## Sprint 1 — CLI Core
- [x] Implement core CLI flags: `--version`, `--resume <id>`, `--replay <id>`
- [x] Add slash command parser (`/init`, `/permissions`, `/context`, `/ledger`, `/resume`)
- [x] Wire config loading and override order (default → profile → session → flags)
- [x] Add structured logging with session IDs

## Sprint 2 — Session Management
- [x] Create session layout (`sessions/<id>/plan.md`, `inventory.md`, `ledger.md`, `logs/`, `artifacts/`)
- [x] Implement session start/stop and resume flow
- [x] Implement plan recording at session start
- [x] Implement inventory capture on `/init`

## Sprint 3 — Safety & Execution
- [x] Enforce permissions (`readonly`, `default`, `all`) with approval gates
- [x] Implement kill-switch handling (SIGINT/SIGTERM) and clean shutdown
- [x] Add tool execution wrapper (Bash/Python) with timeouts + artifact logging

## Sprint 4 — Exploit Discovery & Reporting
- [x] Metasploit discovery via scripted `msfconsole` (read-only)
- [x] Evidence ledger updates (Markdown) with command → log links
- [x] OWASP-style report template + generator

## Sprint 5 — Quality & Testing
- [x] Unit tests for config, scope enforcement, planning, report generation
- [x] Replay-based regression tests for known findings (basic replay harness)
- [x] CLI end-to-end test harness

## Sprint 6 — Scope Enforcement
- [x] Enforce scope allow/deny lists for `/run` and replay (CIDR + hostname literals)
- [x] Add scope enforcement tests
- [x] Document scope configuration details

## Sprint 7 — LLM Core & Planning
- [x] Add LLM client interface + LMStudio config (base URL, model, timeout)
- [x] Implement planning phase command (`/plan`) that writes `sessions/<id>/plan.md`
- [x] Add context artifact stubs (`summary.md`, `known_facts.md`, optional `focus.md`)
- [x] Implement `/summarize` (manual) to update summaries from recent logs/ledger
- [x] Add auto-summarize hooks (threshold/step-based) for `/run` and `/msf`
- [x] Robust tests for context management (file creation, append/update, thresholds, size limits, readonly behavior)

## Sprint 8 — LLM Guidance & Reporting
- [x] LLM-backed `/plan auto` and `/next` guidance (fallback when offline)
- [x] Minimal report enhancement: include evidence ledger in report output
- [x] Tests for planner outputs and report evidence inclusion

## Sprint 9 — Interactive Assistant Loop
- [x] Add `/assist` to request and optionally execute a suggested command
- [x] LLM assistant prompt + fallback assistant
- [x] Tests for assistant parsing + e2e assist flow

## Sprint 10 — Script Helper
- [x] Add `/script <py|sh> <name>` to write and run scripts
- [x] Script helper tests (e2e)

## Sprint 11 — Session Cleanup
- [x] Add `/clean [days]` to remove session folders
- [x] Cleanup e2e test

## Sprint 12 — LLM Fail-Safe
- [x] Add max failure + cooldown guard for LLM calls
- [x] Tests for guard behavior

## Sprint 13 — Context Visibility
- [x] Add `/context show` for summary/facts/focus/state

## Sprint 14 — Chat Input
- [x] Route plain text input to LLM (`/ask`)
- [x] Add `/ask` command

## Sprint 15 — Agent Default Mode
- [x] Route plain text input to `/assist` (agent mode)

## Sprint 16 — True Agent Loop (in progress)
- [x] Extend assistant protocol with explicit completion (`type=complete`) and richer observations (avoid `cat`-the-log loops).
- [x] Add step-level Observation plumbing (exit code + key output) and feed it back into each subsequent LLM call.
- [x] Add deterministic Finalize phase for goals like “create a report” (write report artifact every time).
- [x] Add agent-loop tests (complete handling, observation carry-forward, finalize artifact creation).
- [x] Refactor: split `internal/cli/cli.go` (3300+ LOC) into focused files (input, assist loop, run/browse/msf, reporting, helpers) without behavior changes; keep tests passing.

## Sprint 17 — Tool Primitives (future)
- [ ] Introduce first-class tools (fetch URL, parse links, read file, write file) to reduce fragile `bash -c` pipelines.
- [ ] Migrate web recon to primitives + parsing (multi-page crawl with bounds).

## Sprint 18 — Chat vs Act UX (future)
- [ ] Lightweight “answer vs act” classification so normal conversation feels like Codex/Claude while still being agentic.
- [ ] Reduce noise in non-verbose mode (only show task headers + key outputs).
