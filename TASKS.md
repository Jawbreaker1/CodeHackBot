# Sprint Plan

This plan is a living document. Keep tasks small, testable, and tied to artifacts under `sessions/`.

## Sprint 0 — Foundations (done)
- [x] Repo scaffolding: `AGENTS.md`, `PROJECT.md`, `README.md`
- [x] `prompts/system.md` behavior prompt
- [x] `config/default.json` baseline schema
- [x] Minimal Go CLI entrypoint

## Sprint 1 — CLI Core
- [ ] Implement core CLI flags: `--version`, `--resume <id>`, `--replay <id>`
- [ ] Add slash command parser (`/init`, `/permissions`, `/context`, `/ledger`, `/resume`)
- [ ] Wire config loading and override order (default → profile → session → flags)
- [ ] Add structured logging with session IDs

## Sprint 2 — Session Management
- [ ] Create session layout (`sessions/<id>/plan.md`, `inventory.md`, `ledger.md`, `logs/`, `artifacts/`)
- [ ] Implement session start/stop and resume flow
- [ ] Implement plan recording at session start
- [ ] Implement inventory capture on `/init`

## Sprint 3 — Safety & Execution
- [ ] Enforce permissions (`readonly`, `default`, `all`) with approval gates
- [ ] Implement kill-switch handling (SIGINT/SIGTERM) and clean shutdown
- [ ] Add tool execution wrapper (Bash/Python) with timeouts + artifact logging

## Sprint 4 — Exploit Discovery & Reporting
- [ ] Metasploit discovery via scripted `msfconsole` (read-only)
- [ ] Evidence ledger updates (Markdown) with command → log links
- [ ] OWASP-style report template + generator

## Sprint 5 — Quality & Testing
- [ ] Unit tests for config, scope enforcement, planning, report generation
- [ ] Replay-based regression tests for known findings
- [ ] CLI end-to-end test harness
