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

## Sprint 16 — True Agent Loop (done)
- [x] Extend assistant protocol with explicit completion (`type=complete`) and richer observations (avoid `cat`-the-log loops).
- [x] Add step-level Observation plumbing (exit code + key output) and feed it back into each subsequent LLM call.
- [x] Add deterministic Finalize phase for goals like “create a report” (write report artifact every time).
- [x] Add agent-loop tests (complete handling, observation carry-forward, finalize artifact creation).
- [x] Refactor: split `internal/cli/cli.go` (3300+ LOC) into focused files (input, assist loop, run/browse/msf, reporting, helpers) without behavior changes; keep tests passing.

## Sprint 17 — Tool Primitives + Tool Forge (done)
- [x] Add first-class tool primitives:
  - [x] `fetch_url(url)` (replacement for brittle `/browse` shelling); save body to `sessions/<id>/artifacts/web/`. (implemented via `/browse` saving body artifacts)
  - [x] `parse_links(html_path|html)` (extract + normalize links; bounded). (implemented via `/parse_links` and saved `links-*.txt`)
  - [x] `read_file(path)` / `list_dir(path)` (bounded to session dir + repo docs by default). (implemented via `/read` + `/ls` bounded to session dir or CWD)
  - [x] `write_file(path, content)` (bounded to `sessions/<id>/artifacts/tools/` by default). (implemented via `/write`)
- [x] Implement “tool forge” loop for on-demand utilities:
  - [x] Assistant can return `type=tool` with `{language, name, files, run, purpose}`.
  - [x] Execution flow: write files -> run -> capture observation -> iterate (bounded) until `type=complete`. (bounded auto-fix loop)
  - [x] Default languages for MVP: Python + Bash; allow Go later.
- [x] Safety boundaries:
  - [x] Path sandbox for tool forge (no writes outside tool dir unless explicitly allowed).
  - [x] Approval gates: write + run require confirmation in `permissions=default`.
  - [x] Timeouts + max tool-build iterations per goal (configurable). (`agent.tool_max_fixes`, `agent.tool_max_files`)
  - [x] Scope enforcement applies to all tool runs that include targets.
- [x] Reuse & traceability:
  - [x] Persist a per-session `sessions/<id>/artifacts/tools/manifest.json` (what was built, hashes, purpose, run command).
  - [x] Prefer reuse over rebuild when a matching tool exists.
- [x] Wire into `/assist`:
  - [x] Prefer primitives (fetch/read/parse) over `bash -c` pipelines when accomplishing goals. (guardrail blocks fragile curl|grep pipelines)
  - [x] Web recon flow: fetch -> parse -> bounded crawl (N pages) -> summarize -> persist artifacts. (`/crawl` + `crawl-index.json` + `crawl-summary.md`)
- [x] Tests:
  - [x] Tool forge: create tool, fix failure via recovery, rerun, complete. (bash-based test)
  - [x] Sandbox: attempts to write outside `artifacts/tools/` are blocked.
  - [x] Observation carry-forward: tool outputs influence the next assistant step.
  - [x] Bounded crawl: never exceeds page/byte limits; produces crawl index + artifacts + summary artifact.

## Sprint 17.1 — Agentic Control Refinement (in progress)
- [x] Step 1: Add a generic controller-side goal evaluator that inspects latest artifacts and decides done/continue (avoid task-specific hardcoding).
- [x] Step 2: Canonicalize action signatures for loop detection (aliases and semantically equivalent actions should count as repeats).
- [x] Step 3: Separate recovery retries from progress-step budget (retry loops should not consume the full execution budget).
- [x] Step 4: Refine fallback assistant behavior to be domain-aware (local-file/web/network) without defaulting to scan-only prompts.
- [x] Step 5: Add focused tests for each refinement and keep `go test ./...` + `go build ./cmd/birdhackbot` green.

## Sprint 18 — Chat vs Act UX (future)
- [ ] Lightweight “answer vs act” classification so normal conversation feels like Codex/Claude while still being agentic.
- [ ] Reduce noise in non-verbose mode (only show task headers + key outputs).

## Sprint 19 — Orchestrator Contracts + Binary Skeleton (planned)
- [x] Enforce test-first baseline for orchestrator core (new behavior requires tests in same change).
- [x] Add separate orchestrator binary scaffold: `cmd/birdhackbot-orchestrator`.
- [x] Create orchestrator package boundary (`internal/orchestrator/*`) with headless engine interfaces.
- [x] Implement JSON schemas + validators:
  - [x] `plan.json`
  - [x] `task.json`
  - [x] `event.jsonl` event structs
  - [x] `artifact.json`
  - [x] `finding.json`
  - [x] approval event structs (`approval_requested`, `approval_granted`, `approval_denied`, `approval_expired`)
- [x] Add file-based run directory layout: `sessions/<run-id>/orchestrator/{plan,task,event,artifact,finding}/`.
- [x] Lock file-based transport protocol (single-host MVP):
  - [x] append-only `event.jsonl` envelope with required fields (`event_id`, `run_id`, `worker_id`, `task_id`, `seq`, `ts`, `type`, `payload`)
  - [x] per-worker monotonic `seq`
  - [x] dedupe by `event_id`
  - [x] deterministic replay to rebuild orchestrator state
- [x] Implement task handoff SLA:
  - [x] orchestrator lease write -> worker must emit `task_started` within configured startup window
  - [x] reclaim lease on missed startup SLA
- [x] Implement atomic file-write helpers for orchestrator artifacts/events (`tmp + rename`).
- [x] Add CLI commands for orchestrator MVP: `start`, `status`, `workers`, `events`, `stop`.
- [x] Add tests for schema round-trip and backward-compatible parsing.
  - [x] add tests for event ordering, dedupe, and replay reconstruction.
- [x] Implement plan-first validation:
  - [x] reject `start` when scope/constraints/success/stop criteria are missing
  - [x] validate task quality fields before lease (`done_when`, `fail_when`, `expected_artifacts`, `risk_level`, budgets)

## Sprint 20 — Worker Lifecycle + Scheduler (planned)
- [x] Implement subprocess worker launcher (spawn new `birdhackbot` workers on demand).
- [ ] Implement worker lifecycle manager:
  - [x] `worker_started` / `worker_stopped` events
  - [x] cleanup of idle/completed workers
  - [x] failed worker recovery path
- [x] Implement dependency-aware scheduler with configurable `max_workers`.
- [ ] Implement explicit task state machine with transition validator:
  - [x] states: `queued`, `leased`, `running`, `awaiting_approval`, `completed`, `failed`, `blocked`, `canceled`
  - [x] reject invalid transitions with typed errors
  - [x] retry transition policy (`failed -> queued`, retryable-only, bounded attempts)
- [ ] Implement lease + heartbeat flow:
  - [x] lease acquisition/release
  - [x] heartbeat every `5s`
  - [x] stale lease detection + reclaim at `20s`
  - [x] soft-stall grace handling at `30s`
  - [x] bounded retries (`2`) with backoff (`5s`, `15s`)
- [ ] Implement centralized approval broker (orchestrator-owned):
  - [x] workers emit `approval_requested` events instead of blocking on stdin
  - [x] queue + resolution flow for approve/deny/expire
  - [x] approval scopes: once, task, session (never outside existing scope/permissions)
- [ ] Implement risk-tier policy engine:
  - [x] classify actions into `recon_readonly`, `active_probe`, `exploit_controlled`, `priv_esc`, `disruptive`
  - [x] enforce permission-mode matrix (`readonly`, `default`, `all`) against tier
  - [x] deny out-of-scope targets before approval flow
  - [x] enforce `disruptive` deny-by-default unless explicit session opt-in is present
- [ ] Implement session pre-approval grants with expiry:
  - [x] scopes: once/task/session
  - [x] attach actor, reason, expiry metadata
  - [x] never widen scope beyond run/session scope constraints
- [ ] Implement timer semantics for `awaiting_approval`:
  - [x] pause execution timeout while waiting
  - [x] pause lease stale timer while waiting
  - [x] separate approval wait timeout (`45m` default) -> mark task `blocked` on expiry
- [x] Enforce stop semantics:
  - [x] global broadcast stop
  - [x] per-worker stop
  - [x] SIGTERM -> SIGKILL escalation timeout
  - [x] orchestrator control commands (`worker-stop`, `stop`) emit stop-request events for the run loop
- [ ] Validate worker isolation behavior:
  - [x] per-worker session/artifact directory isolation
  - [x] confirm normal PATH tool execution works outside session directories
- [x] Add scheduler simulation tests (parallelism, reclaim, retry, stop propagation).
- [ ] Add approval-flow tests:
  - [x] no false timeout while approval is pending
  - [x] approval expiry marks task blocked (not failed)
  - [x] scoped approval reuse works for task/session scopes
  - [x] risk-tier matrix tests (mode x tier expected decision)
  - [x] disruptive-action default deny tests
  - [x] out-of-scope action deny-before-approval tests
- [ ] Add task-state-machine tests:
  - [x] valid transition matrix
  - [x] invalid transition rejection
  - [x] blocked vs failed reason mapping
  - [x] restart reconciliation for `leased`/`running`/`awaiting_approval`

## Sprint 21 — Evidence Merge + Replan Loop (planned)
- [x] Implement finding/artifact ingestion pipeline from worker events.
- [x] Add deterministic dedupe key for findings (`target + type + location + normalized title`).
- [x] Implement confidence/source-aware conflict handling (retain conflicting evidence).
- [x] Implement orchestrator state updater + replan triggers on blockers/findings.
- [x] Implement replan policy engine with bounded outcomes:
  - [x] trigger on repeated-step loops
  - [x] trigger on approval denied/expired
  - [x] trigger on missing required artifacts after retries
  - [x] trigger on stale lease/worker crash recovery
  - [x] outcomes: refine task, split task, or terminate with explicit reason
- [x] Enforce no-silent-retry rule (every replan/retry decision emits explicit event).
- [x] Add report assembly from merged findings + artifact links.
- [x] Add end-to-end orchestrator test:
  - [x] run start -> worker fan-out -> evidence merge -> run completion
  - [x] regression test that standalone `birdhackbot` behavior remains unchanged.
  - [x] budget guard test (`max_steps`/`max_tool_calls`/`max_runtime`) and deterministic stop on exhaustion.
  - [x] env-gated integration scenario: create encrypted zip and validate solvable vs unsolved orchestrated outcomes.

## Sprint 22 — Orchestrator UI (future)
- [ ] Framework decision (locked):
  - [ ] Frontend: React + TypeScript + Vite
  - [ ] Backend API: existing Go codebase with WebSocket/SSE stream endpoints
- [ ] Terminal TUI mode (Codex-like operator cockpit):
  - [ ] static status bar (run id, mode, risk, active tools, context/budget usage)
  - [ ] pipeline stage row (recon -> hypothesis -> verify -> report) with current-step summary
  - [ ] multi-agent pane (state, heartbeat age, current task, last tool, queue depth)
  - [ ] approval pane with keyboard actions (approve/deny) and risk metadata
  - [ ] evidence/log tail pane with bounded scroll + filter
  - [ ] stable render loop (no ghost lines), resize-safe redraw, ANSI fallback support
- [ ] Implement local dashboard shell:
  - [ ] Runs view (active/completed orchestrator runs)
  - [ ] Agents view (worker status, heartbeat, current task, elapsed time)
  - [ ] Plan graph view (queued/running/blocked/completed nodes)
  - [ ] Evidence view (artifacts/findings timeline)
- [ ] Control actions:
  - [ ] pause/resume/stop run
  - [ ] stop single worker
  - [ ] broadcast kill to all workers
- [ ] Live communication:
  - [ ] stream `event.jsonl` updates into UI
  - [ ] highlight stale workers and failed tasks in real time
- [ ] Tests:
  - [ ] backend API contract tests
  - [ ] frontend component tests for critical state transitions
  - [ ] e2e smoke test for one orchestrated run lifecycle

## Sprint 23 — Production Worker Mode + Kali Deployment (planned)
- [ ] Implement production worker mode in `birdhackbot` (non-test helper):
  - [x] add `birdhackbot worker` command with orchestrator task inputs via env/flags
  - [x] emit lifecycle events: `task_started`, `task_progress`, `task_artifact`, `task_finding`, `task_completed`/`task_failed`
  - [x] guarantee monotonic per-worker sequencing and idempotent re-runs
- [ ] Define worker task contract and execution protocol:
  - [x] task payload schema for goal/targets/budgets/constraints/risk tier
  - [x] explicit completion contract (required artifacts/findings + status reason)
  - [x] standardized failure reasons for replan engine compatibility
- [ ] Integrate real tool execution path for worker mode:
  - [x] scoped command execution with approval/risk-tier checks
  - [x] structured evidence emission (logs/artifacts/findings metadata)
  - [x] timeout and kill-switch semantics (SIGINT/SIGTERM + child process cleanup)
- [ ] End-user orchestration runbook for Kali:
  - [x] operator setup doc (build, config, permissions, scope profile)
  - [x] start/run/approval/stop/report command examples
  - [x] troubleshooting guide for approvals, stale workers, and replan triggers
- [ ] Acceptance tests for real end-user workflow:
  - [ ] e2e `start -> run -> approvals -> completion -> report` using production worker mode
  - [ ] gated integration test on Kali toolchain (network-safe lab profile)
  - [ ] regression test that standalone interactive `birdhackbot` behavior remains unchanged
