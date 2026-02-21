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
  - [x] static status bar (run id, mode, risk, active tools, context/budget usage)
  - [x] command prompt line (interactive action input at bottom of TUI)
  - [x] plan summary panel (goal + task/criteria counts)
  - [x] task board panel (per-task state/worker/strategy)
  - [x] operator instruction command (`instruct <text>`) to inject new goals during active run
  - [ ] plain-text chat bridge to orchestrator LLM (ask “what is the plan?” style questions without command syntax)
  - [ ] continue-from-complete flow: accept new top-level instruction after run completion and start follow-up phase without leaving TUI
  - [ ] pipeline stage row (recon -> hypothesis -> verify -> report) with current-step summary
  - [x] multi-agent pane (state, heartbeat age, current task, last tool, queue depth)
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

## Sprint 23 — Production Worker Mode + Kali Deployment (done)
- [x] Implement production worker mode in `birdhackbot` (non-test helper):
  - [x] add `birdhackbot worker` command with orchestrator task inputs via env/flags
  - [x] emit lifecycle events: `task_started`, `task_progress`, `task_artifact`, `task_finding`, `task_completed`/`task_failed`
  - [x] guarantee monotonic per-worker sequencing and idempotent re-runs
- [x] Define worker task contract and execution protocol:
  - [x] task payload schema for goal/targets/budgets/constraints/risk tier
  - [x] explicit completion contract (required artifacts/findings + status reason)
  - [x] standardized failure reasons for replan engine compatibility
- [x] Integrate real tool execution path for worker mode:
  - [x] scoped command execution with approval/risk-tier checks
  - [x] structured evidence emission (logs/artifacts/findings metadata)
  - [x] timeout and kill-switch semantics (SIGINT/SIGTERM + child process cleanup)
- [x] End-user orchestration runbook for Kali:
  - [x] operator setup doc (build, config, permissions, scope profile)
  - [x] start/run/approval/stop/report command examples
  - [x] troubleshooting guide for approvals, stale workers, and replan triggers
- [x] Acceptance tests for real end-user workflow:
  - [x] e2e `start -> run -> approvals -> completion -> report` using production worker mode
  - [x] gated integration test on Kali toolchain (network-safe lab profile)
  - [x] regression test that standalone interactive `birdhackbot` behavior remains unchanged

## Sprint 24 — Autonomous Planner + Hypothesis Engine (planned, fundamental)
- [x] Add high-level instruction intake for orchestrator:
  - [x] `run --goal "<text>"` input path (without requiring prebuilt `plan.json`)
  - [x] persist normalized goal + operator constraints in run metadata
  - [x] enforce mandatory scope/constraints gate before planning
- [x] Implement hypothesis generation:
  - [x] generate initial hypotheses from goal + target profile
  - [x] map each hypothesis to success/fail signals and evidence requirements
  - [x] bound hypothesis count and rank by confidence/impact
- [x] Implement autonomous task-graph synthesis:
  - [x] convert hypotheses into dependency-aware task graph (`task.json` contracts)
  - [x] attach risk tier, budgets, done/fail criteria, expected artifacts to every task
  - [x] preflight validation rejects unsafe/out-of-scope/unbounded plans
- [x] Add plan review + launch controls:
  - [x] show generated plan summary in CLI/TUI before execution
  - [x] operator choices: approve all / edit / reject / regenerate
  - [x] audit log for planner version, prompt hash, and decision rationale
- [x] Implement adaptive replanning with graph mutation:
  - [x] on new evidence, add/split/prioritize tasks instead of only emitting replan events
  - [x] maintain deterministic idempotency keys for generated tasks
  - [x] cap replans by run-level budget and stop criteria
- [x] Add orchestrator memory bank (file-first, planner-oriented):
  - [x] run-level memory artifacts under `sessions/<run-id>/orchestrator/memory/` (`hypotheses.md`, `plan_summary.md`, `known_facts.md`, `open_questions.md`, `context.json`)
  - [x] persist planner decisions + rationale + confidence and link them to generated tasks/events
  - [x] add compaction/summarization strategy for long runs so planning context stays bounded and stable
  - [x] ensure worker findings/artifacts are folded into orchestrator memory before replanning
- [x] Testing and acceptance:
  - [x] unit tests: hypothesis scoring, graph synthesis, safety validators
  - [x] simulation tests: evidence-driven replan mutation and budget/stop enforcement
  - [x] e2e: `goal -> generated plan -> approval -> worker fan-out -> report`
  - [x] regression: existing `plan.json` path still works unchanged

## Sprint 25 — LLM Planner Mode + Playbook Grounding (planned)
- [x] Add optional planner mode `--planner static|llm|auto` (default static for deterministic safety).
- [x] Implement LLM-based hypothesis + task-graph synthesis with strict JSON schema validation.
- [ ] Ground planner prompts in relevant playbooks (bounded read) so plans reuse repo guidance.
- [x] Add deterministic fallback when LLM planner is unavailable or returns invalid plan.
- [ ] Persist planner provenance (`mode`, model, prompt hash, playbooks used) in run metadata.
- [ ] Tests:
  - [x] unit tests for planner validation/fallback behavior
  - [ ] integration test for `run --planner llm` with mocked LLM output
  - [x] regression test that deterministic planner path remains unchanged

## Sprint 26 — Completion Contract Enforcement (planned)
- [x] Enforce task completion contracts in coordinator before accepting `task_completed`:
  - [x] verify required artifacts exist and are non-empty
  - [x] verify required finding types were emitted for the task/target
  - [x] emit `task_failed` with reason `missing_required_artifacts` when contract is not met
- [x] Stop marking `completion_contract.verification_status="satisfied"` unconditionally in workers.
- [x] Add contract-check diagnostics to TUI (`why task marked failed/completed`).
- [x] Tests:
  - [x] completion event with missing artifacts is converted to failure
  - [x] completion with valid artifacts/findings remains completed
  - [x] replan trigger on repeated `missing_required_artifacts` still works

## Sprint 27 — Scope Engine Unification + Fail-Closed Wrappers (planned)
- [x] Introduce shared scope package and remove duplicated logic between:
  - [x] `internal/exec/scope.go`
  - [x] `internal/orchestrator/scope.go`
- [x] Enforce fail-closed behavior for network-capable wrapper commands (`bash -c`, `sh -c`, `zsh -c`) when scope is enabled.
- [x] Add target extraction for wrapped network commands and URLs (including script body parsing for common patterns).
- [x] Add policy parity tests so CLI and orchestrator enforce identical scope decisions.
- [x] Tests:
  - [x] wrapper bypass attempts are denied when target is missing/out-of-scope
  - [x] in-scope wrapped commands are allowed
  - [x] deny-target rules are enforced in both execution paths

## Sprint 28 — Event Pipeline Performance + Robustness (planned)
- [x] Add incremental event ingestion (cursor/offset) so evidence merge processes only new events.
- [x] Replace repeated full-log `nextSeq` scans with per-worker sequence state persisted atomically.
- [x] Add resilient event reading mode:
  - [x] quarantine/skip malformed lines with explicit warning events
  - [x] avoid aborting whole run on a single corrupt event line
- [x] Add run-state/materialized snapshots to reduce repeated recomputation in TUI/status paths.
- [x] Tests:
  - [x] long-run event growth benchmark guard
  - [x] malformed event line does not kill run status processing
  - [x] seq monotonicity remains correct under concurrent workers

## Sprint 29 — Worker Assist Reliability Modes (planned)
- [x] Add worker assist modes:
  - [x] `strict` (no fallback assistant, fail-fast on LLM failure/parse failure)
  - [x] `degraded` (allow fallback assistant)
- [x] Emit explicit metadata per assist turn: model, timeout, parse-repair used, fallback used.
- [x] Improve loop handling:
  - [x] semantic repeat detection (same intent via alias/arg reorder)
  - [x] stronger recovery constraints after repeated failures
- [x] Add assistant output schema hardening for tool suggestions (`steps`, `tool.files`, `run`).
- [x] Tests:
  - [x] strict mode fails loudly without silent fallback
  - [x] degraded mode keeps progressing and surfaces fallback reason in events
  - [x] loop guard catches semantic repeats

## Sprint 30 — TUI Operator UX Semantics (planned)
- [x] Lock command semantics:
  - [x] `ask` is read-only and never queues instructions
  - [x] `instruct` always queues execution changes
- [x] Expand command log viewport and preserve full assistant replies (no premature truncation).
- [x] Add scroll controls for both command log and recent events.
- [x] Keep active workers pinned at top of worker debug pane; completed/stopped workers collapsed by default.
- [x] Tests:
  - [x] `ask` vs `instruct` behavior contract tests
  - [x] snapshot tests for worker pane ordering/collapse
  - [x] long-message rendering tests (no dropped critical lines)

## Sprint 31 — Evidence-First Reporting + Templates (planned)
- [x] Make report generation strictly evidence-backed:
  - [x] each finding requires linked artifact/log evidence
  - [x] unresolved claims are labeled `UNVERIFIED`
- [x] Add report validators for required sections per profile (`standard`, `owasp`, `nis2`, `internal`).
- [x] Improve orchestrator default report discoverability (path emitted in status/TUI and on terminal outcomes).
- [x] Add report quality tests using real run artifacts (network scan + web recon cases).

## Sprint 32 — Maintainability Refactor Guardrails (planned)
- [ ] Refactor high-complexity files into smaller units:
  - [ ] `internal/assist/assistant.go`
  - [ ] `internal/cli/assist_state.go`
  - [ ] `internal/orchestrator/worker_runtime_assist.go`
  - [ ] `cmd/birdhackbot-orchestrator/tui_render.go`
- [ ] Add package-level architecture notes for assist loop, scope enforcement, and event pipeline.
- [ ] Add CI checks for complexity/size thresholds and enforce test updates for refactors.

## Sprint 33 — Interactive Orchestrator Planning Mode (planned)
- [x] Make `--goal` optional for orchestrator startup:
  - [x] keep `--goal` path for automation/non-interactive runs
  - [x] support `tui` startup with no initial goal
- [x] Add explicit run phases: `planning -> review -> approved -> executing -> completed`.
- [x] Add planner chat loop in TUI before execution:
  - [x] operator enters testing intent in plain text
  - [x] orchestrator LLM proposes a draft plan + rationale
  - [x] operator can discuss/refine plan in multiple turns
- [x] Add plan editing commands (pre-execution):
  - [x] add task
  - [x] remove task
  - [x] modify task (goal/targets/risk/budget/dependencies)
  - [x] reorder/prioritize tasks
- [x] Add plan diff + validation gate before execution:
  - [x] show what changed from last draft
  - [x] run full safety/scope validator on every revision
  - [x] block execute if plan is invalid or out of scope
- [x] Add explicit execution controls:
  - [x] `execute` to start workers from approved plan
  - [x] `regenerate` to ask planner for a new draft
  - [x] `discard` to reset planning session
- [x] Persist planning conversation and revisions under run artifacts:
  - [x] planner transcript
  - [x] plan versions (`v1..vn`) + diff metadata
  - [x] final approved plan provenance (model, prompt hash, operator approvals)
- [x] Tests:
  - [x] no-goal startup enters planning mode and does not execute workers
  - [x] multi-turn plan refinement updates task graph deterministically
  - [x] add/remove/modify commands mutate plan as expected
  - [x] execute only works from approved valid plan
  - [x] `--goal` automation path remains unchanged (regression)

## Sprint 34 — LLM Stability Controls (planned)
- [x] Make LLM behavior tunable via config/env (with safe defaults):
  - [x] `llm.temperature` base setting
  - [x] optional per-role overrides (`assist`, `planner`, `summarize`, `recovery`, `tui_assistant`)
  - [x] optional `max_tokens` controls per role
- [x] Lower default temperatures for high-trust paths:
  - [x] planner and evaluation/repair paths deterministic (`0.0-0.1`)
  - [x] summaries/recovery conservative (`~0.1`)
- [x] Add tests ensuring configured values are actually used by call sites.
