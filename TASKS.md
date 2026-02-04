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
