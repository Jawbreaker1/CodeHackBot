# Sprint Plan

This plan is a living document. Keep tasks small, testable, and tied to artifacts under `sessions/`.
Sprint flow rule: do not start a new sprint while the previous sprint has open tasks. Move unfinished items forward explicitly so each closed sprint has zero open tasks.
Sprint header convention (all new planned sprints): first checklist item must be `Prerequisite: previous sprint completed or unfinished tasks explicitly moved`.

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

## Sprint 18 — Chat vs Act UX (closed - deferred)
- [x] Closed on 2026-02-22 to keep focus on current sprint execution; remaining items deferred.
- [x] [Deferred] Lightweight “answer vs act” classification so normal conversation feels like Codex/Claude while still being agentic.
- [x] [Deferred] Reduce noise in non-verbose mode (only show task headers + key outputs).

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

## Sprint 20 — Worker Lifecycle + Scheduler (done)
- [x] Implement subprocess worker launcher (spawn new `birdhackbot` workers on demand).
- [x] Implement worker lifecycle manager:
  - [x] `worker_started` / `worker_stopped` events
  - [x] cleanup of idle/completed workers
  - [x] failed worker recovery path
- [x] Implement dependency-aware scheduler with configurable `max_workers`.
- [x] Implement explicit task state machine with transition validator:
  - [x] states: `queued`, `leased`, `running`, `awaiting_approval`, `completed`, `failed`, `blocked`, `canceled`
  - [x] reject invalid transitions with typed errors
  - [x] retry transition policy (`failed -> queued`, retryable-only, bounded attempts)
- [x] Implement lease + heartbeat flow:
  - [x] lease acquisition/release
  - [x] heartbeat every `5s`
  - [x] stale lease detection + reclaim at `20s`
  - [x] soft-stall grace handling at `30s`
  - [x] bounded retries (`2`) with backoff (`5s`, `15s`)
- [x] Implement centralized approval broker (orchestrator-owned):
  - [x] workers emit `approval_requested` events instead of blocking on stdin
  - [x] queue + resolution flow for approve/deny/expire
  - [x] approval scopes: once, task, session (never outside existing scope/permissions)
- [x] Implement risk-tier policy engine:
  - [x] classify actions into `recon_readonly`, `active_probe`, `exploit_controlled`, `priv_esc`, `disruptive`
  - [x] enforce permission-mode matrix (`readonly`, `default`, `all`) against tier
  - [x] deny out-of-scope targets before approval flow
  - [x] enforce `disruptive` deny-by-default unless explicit session opt-in is present
- [x] Implement session pre-approval grants with expiry:
  - [x] scopes: once/task/session
  - [x] attach actor, reason, expiry metadata
  - [x] never widen scope beyond run/session scope constraints
- [x] Implement timer semantics for `awaiting_approval`:
  - [x] pause execution timeout while waiting
  - [x] pause lease stale timer while waiting
  - [x] separate approval wait timeout (`45m` default) -> mark task `blocked` on expiry
- [x] Enforce stop semantics:
  - [x] global broadcast stop
  - [x] per-worker stop
  - [x] SIGTERM -> SIGKILL escalation timeout
  - [x] orchestrator control commands (`worker-stop`, `stop`) emit stop-request events for the run loop
- [x] Validate worker isolation behavior:
  - [x] per-worker session/artifact directory isolation
  - [x] confirm normal PATH tool execution works outside session directories
- [x] Add scheduler simulation tests (parallelism, reclaim, retry, stop propagation).
- [x] Add approval-flow tests:
  - [x] no false timeout while approval is pending
  - [x] approval expiry marks task blocked (not failed)
  - [x] scoped approval reuse works for task/session scopes
  - [x] risk-tier matrix tests (mode x tier expected decision)
  - [x] disruptive-action default deny tests
  - [x] out-of-scope action deny-before-approval tests
- [x] Add task-state-machine tests:
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

## Sprint 22 — Orchestrator UI (closed - deferred)
- [x] Closed on 2026-02-22 to keep focus on current sprint execution; remaining items deferred.
- [x] [Deferred] Framework decision (locked):
  - [x] [Deferred] Frontend: React + TypeScript + Vite
  - [x] [Deferred] Backend API: existing Go codebase with WebSocket/SSE stream endpoints
- [x] [Deferred] Terminal TUI mode (Codex-like operator cockpit):
  - [x] static status bar (run id, mode, risk, active tools, context/budget usage)
  - [x] command prompt line (interactive action input at bottom of TUI)
  - [x] plan summary panel (goal + task/criteria counts)
  - [x] task board panel (per-task state/worker/strategy)
  - [x] operator instruction command (`instruct <text>`) to inject new goals during active run
  - [x] [Deferred] plain-text chat bridge to orchestrator LLM (ask “what is the plan?” style questions without command syntax)
  - [x] [Deferred] continue-from-complete flow: accept new top-level instruction after run completion and start follow-up phase without leaving TUI
  - [x] [Deferred] pipeline stage row (recon -> hypothesis -> verify -> report) with current-step summary
  - [x] multi-agent pane (state, heartbeat age, current task, last tool, queue depth)
  - [x] [Deferred] approval pane with keyboard actions (approve/deny) and risk metadata
  - [x] [Deferred] evidence/log tail pane with bounded scroll + filter
  - [x] [Deferred] stable render loop (no ghost lines), resize-safe redraw, ANSI fallback support
- [x] [Deferred] Implement local dashboard shell:
  - [x] [Deferred] Runs view (active/completed orchestrator runs)
  - [x] [Deferred] Agents view (worker status, heartbeat, current task, elapsed time)
  - [x] [Deferred] Plan graph view (queued/running/blocked/completed nodes)
  - [x] [Deferred] Evidence view (artifacts/findings timeline)
- [x] [Deferred] Control actions:
  - [x] [Deferred] pause/resume/stop run
  - [x] [Deferred] stop single worker
  - [x] [Deferred] broadcast kill to all workers
- [x] [Deferred] Live communication:
  - [x] [Deferred] stream `event.jsonl` updates into UI
  - [x] [Deferred] highlight stale workers and failed tasks in real time
- [x] [Deferred] Tests:
  - [x] [Deferred] backend API contract tests
  - [x] [Deferred] frontend component tests for critical state transitions
  - [x] [Deferred] e2e smoke test for one orchestrated run lifecycle

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
- [x] Ground planner prompts in relevant playbooks (bounded read) so plans reuse repo guidance.
- [x] Add deterministic fallback when LLM planner is unavailable or returns invalid plan.
- [x] Persist planner provenance (`mode`, model, prompt hash, playbooks used) in run metadata.
- [x] Tests:
  - [x] unit tests for planner validation/fallback behavior
  - [x] integration test for `run --planner llm` with mocked LLM output
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
- [x] Refactor high-complexity files into smaller units:
  - [x] `internal/assist/assistant.go`
  - [x] `internal/cli/assist_state.go`
  - [x] `internal/orchestrator/worker_runtime_assist.go`
  - [x] `cmd/birdhackbot-orchestrator/tui_render.go`
- [x] Add package-level architecture notes for assist loop, scope enforcement, and event pipeline.
- [x] Add CI checks for complexity/size thresholds and enforce test updates for refactors.

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

## Sprint 35 — Autonomy Benchmark Harness + Baseline (closed, carryover moved)
- [x] Create a fixed benchmark scenario pack under `docs/runbooks/autonomy-benchmark-program.md`.
- [x] Add one-command benchmark runner for orchestrator runs (repeatable seeds, fixed scopes).
- [x] Persist per-run scorecard artifacts with machine-readable metrics JSON.
- [x] Run baseline 5x per scenario and record median + P90 as the locked baseline.
- [x] Harden run terminalization:
  - [x] if no active workers and no runnable tasks remain, force deterministic terminal state (avoid `state=running` with `running_tasks=1` deadlock).
  - [x] add regression test for "stopped worker + no pending approvals + one stale running task" to guarantee report finalization.
- [x] Improve benchmark diagnostics to speed adaptation:
  - [x] include dominant failure reason breakdown in scorecard/summary (`assist_loop_detected`, `assist_timeout`, `assist_no_action`, timeout).
  - [x] include terminalization reason in summary when run is manually or automatically stopped.
  - [x] auto-approve pending benchmark approvals during `benchmark` runs (avoid `approval_timeout`-driven blocked tasks in unattended runs).
- [ ] Address current `host_discovery_inventory` smoke blockers before re-locking baseline:
  - [x] fix terminal-state task counter consistency (`state=stopped` still reports `running_tasks=1` in status/report).
  - [x] cap recon artifact size/output volume (observed ~800MB `nmap_hosts.txt` from `/8` host sweep).
  - [x] enforce runtime nmap guardrails for direct commands and worker-generated scripts (`-n`, `--host-timeout`, `--max-retries`, `--max-rate`, loopback `/8` cap) to reduce host DNS/network instability.
  - [x] prevent repeated `assist_loop_detected` failure on `task-h02` across retries.
  - [x] reduce/route `scope_denied` outcomes so baseline failures reflect capability gaps rather than scope-policy collisions.
  - [x] treat local script filenames (for example `scan.sh`) as file-like tokens in scope extraction to avoid false `scope_denied` host matches.
  - [x] normalize wrapped URL token parsing for scope extraction so shell punctuation around URLs (`(...)`, trailing `|`/`.`/`,` etc.) does not create false out-of-scope literals (for example `scanme.nmap.org)`).
  - [x] sanitize unsupported nmap `--script` entries and auto-resolve bare shell script names to `tools/<name>.sh` when present.
  - [x] reduce `assist_no_action` failures from recover-mode `plan`/`question` churn (recover non-action loops now classified/handled as `assist_loop_detected`; latest smoke did not emit `assist_no_action`).
  - [x] add bounded no-new-evidence completion fallback for repeated recover churn (identical results, extended recover tool churn, and recover tool-call cap).
  - [x] reduce remaining `assist_loop_detected` churn on `task-h01`/`task-h02` under approved active-probe runs (`host_discovery_inventory` targeted `repeat=2` now 2/2 with loop incident rate 0 and no `assist_loop_detected` on `task-h01`/`task-h02`).
  - [x] [Moved to Sprint 38] reduce `assist_no_new_evidence` dependence for `task-h01`/`task-h02` auth/recon loops when the model alternates helper scripts instead of issuing explicit completion (latest spot checks still show mixed `assist_complete` vs `assist_no_new_evidence` behavior across seeds).
  - [x] [Moved to Sprint 38] tighten bounded fallback trigger so long-lived recover churn terminates earlier without masking genuine progress (improved from `task-h01` fallback at step/tool-call 14 to step/tool-call 8 in `benchmark-20260223-124754` and `benchmark-20260223-125200`; keep open for variance in scenario runtime).
  - [x] harden goal-seeded command compatibility for internal assist builtins (`report`/`browse`/`crawl`) so planner-generated command actions cannot execute unresolved internal command literals as shell binaries (observed `exec: \"report\": executable file not found` loop in `run-scanme-goal-20260223-195012`).
    - command tasks now execute `browse`/`crawl` via internal runtime builtins instead of shell binary lookup; assist tool suggestions reuse the same builtin path with scope validation.
    - report-literal commands are now classified as weak report synthesis inputs for OWASP/report tasks and rewritten to deterministic local report synthesis.
    - regression coverage added for command-action `browse`/`crawl` builtin execution and `report` literal rewrite.
  - [x] fix `goal_llm_v1` completion-contract artifact mismatch that can fail successful scans (`missing_required_artifacts` for planner-named files like `service_version_output.txt`) and block downstream CVE/report tasks (observed in `run-20260223-130819-7e29`; fixed via expected-artifact materialization + dependency-input path repair, validated in `run-20260223-132458-094a`).
  - [x] add regression coverage for goal-seeded router-style runs so downstream vulnerability-mapping/report tasks continue when planner artifact names are not emitted verbatim (`TestRunWorkerTaskRepairsMissingDependencyInputPaths` + existing expected-artifact materialization coverage).
  - [x] normalize LLM planner risk tiers for synthesized safety bounds (clamp `exploit_controlled`/higher to `active_probe`, invalid to `recon_readonly`) to avoid run-start failure from out-of-policy planner output (seen before `run-20260223-132458-094a`).
  - [x] add profile-aware nmap runtime guardrails for synthesized command tasks (`discovery`/`service_enum`/`vuln_mapping`) plus one-shot relaxed retry when host timeout is detected.
  - [x] add deep-scan evidence gate: mark task as `insufficient_evidence` when nmap host-timeout persists without actionable service/vulnerability output (prevents false-success reports).
  - [x] calibrate generic service-enum/vuln bounds so host-level scans complete within 2m budgets more consistently (`--top-ports` cap, `--version-light`, `--script vuln` -> `vuln and safe`, `--script-timeout 20s`); validated by actionable `T-02` output in `run-20260223-142849-69c5`.
  - [x] reject synthetic placeholder vuln-mapping command output (for example `python -c ... Example: ... CVE-...`) as `insufficient_evidence` to avoid false-positive CVE claims.
  - [x] harden LLM planner prompt to forbid placeholder/demo-only command actions and require concrete tool-backed execution against in-scope targets/artifacts.
  - [x] improve planner/runtime contract for vulnerability-mapping tasks to avoid passthrough/placeholder execution:
    - weak vuln-mapping commands (`cat`/`echo`/placeholder shell/python) now auto-rewrite to concrete bounded `nmap` vuln-mapping action against in-scope targets.
    - local shell artifact-processing commands no longer fail closed on wrapper scope checks when no network-sensitive command is present.
    - validated with concrete vuln-script output in `run-20260223-144000-9687` (`http-vuln-cve2010-0738` evidence in `T-03`/`T-04` logs).
  - [x] improve report-task contract so OWASP report generation is tool-backed synthesis from prior artifacts (not fallback vulnerability re-scan) while retaining evidence integrity:
    - weak OWASP/report commands (`cat`/`echo`/placeholder shell-python/network rescans) now auto-rewrite to a local deterministic synthesis action over dependency artifacts.
    - vulnerability-command rewrite now skips report-synthesis tasks to prevent report steps from being hijacked into new scans.
    - added runtime/unit coverage for rewrite behavior and synthesized CVE evidence rendering in report-task logs.
  - [x] improve orchestrator run-report readability for human operators (especially local multi-step workflows like encrypted-archive recovery):
    - added explicit `Plan Overview` and `Execution Narrative` sections with per-task intent, dependencies, execution method, outcome, duration, and key outputs.
    - retained evidence/artifact links while capping per-finding link dumps with overflow markers so reports stay readable.
    - added regression coverage for narrative sections and mixed task outcomes (`TestAssembleRunReportIncludesExecutionNarrative`).
  - [x] harden vulnerability-mapping execution reliability for goal-seeded runs where planner emits tool-wrapper commands (`python3 -c subprocess ... cve-search`):
    - classify shell/python wrappers as weak for vulnerability-mapping tasks and rewrite to bounded concrete `nmap --script "vuln and safe"` action against in-scope targets.
    - keeps CVE-capable evidence generation deterministic when optional tools are unavailable in the worker runtime.
  - [x] prevent duplicate/conflicting nmap script flags in enforced vulnerability evidence profile:
    - when vulnerability evidence enforcement applies, normalize to a single `--script "vuln and safe"` and a single `--script-timeout 20s` (replace existing values instead of appending).
    - avoids unstable runtime behavior observed with stacked script expressions during vuln-mapping retries.
  - [x] tighten report-synthesis classification and CVE extraction quality:
    - avoid misclassifying CVE-mapping tasks as report generation unless task intent is explicitly report-generation (`generate`/`compile`/`aggregate` + report semantics or `owasp`).
    - normalize non-canonical NSE CVE tokens (for example `http-vuln-cve2010-0738`) into canonical `CVE-YYYY-NNNN` identifiers in synthesized OWASP findings.
  - [x] harden subnet-to-device targeting contract for host-specific goals (for example "identify iPhone then scan that host"):
    - require concrete target attribution for vulnerability/report command tasks: pinned host target or dependency `resolved_target.json`; unresolved attribution now fails as `insufficient_evidence` instead of scanning a broad CIDR.
    - persist and consume generic target-attribution artifacts (`resolved_target.json`) so downstream tasks can bind to an attributed host without device-specific hardcoding.
    - enforce attributed nmap execution target rewriting to avoid broad-target drift once a concrete host is resolved.
    - include attribution metadata in synthesized OWASP output (`Attribution confidence` + `Attribution source`) to keep report claims explicit.
  - [x] extend missing-input artifact repair to local shell wrappers (`bash -lc`, `sh -c`) so parser tasks consuming dependency artifacts do not fail on planner `/tmp/...` paths.
    - added shell-wrapper-aware path repair for `bash/sh/zsh -c/-lc` command bodies and regression coverage (`TestRunWorkerTaskRepairsMissingDependencyInputPathsForShellWrapper`).
    - tightened shell path matching boundaries to avoid corrupting embedded relative path literals in quoted strings (regression: `TestRepairMissingCommandInputPathsForShellWrapperSkipsEmbeddedRelativeSegments`).
  - [x] improve dependency-artifact selection quality for repaired report tasks so report commands prefer semantically strongest scan artifacts over generic worker logs when multiple candidates overlap (current heuristic succeeded but selected `T-03` worker log in `run-20260223-132458-094a`).
    - strengthened artifact-hint scoring to downweight generic tokens (`log`/`output`/`report`) and promote scan/vulnerability semantic overlap instead of generic worker-log matches.
    - added tie-break specificity scoring that penalizes generic `worker-*.log` command logs when better artifact candidates are available.
    - added regression coverage for candidate ranking (`TestBestArtifactCandidateForMissingPathPrefersSpecificArtifactOverWorkerLog`, `TestBestArtifactCandidateForMissingPathFallsBackToExactBaseMatch`).
  - [x] cap adaptive `execution_failure` replan fan-out for repeated identical `command_failed` paths (deduped mutation key for adaptive execution-failure chains + regression test).
  - [x] prevent `task-plan-summary` recover question/plan churn from failing runs (summary-task bounded autocomplete fallback + tests).
  - [x] add local-goal scope UX alias for non-network tasks:
    - `birdhackbot-orchestrator run --goal ... --scope-local` now maps to local-only targets (`127.0.0.1`, `localhost`) so local file workflows do not require network CIDR flags.
    - goal-input validation/help text updated to include `--scope-local`.
    - CLI tests added for missing-scope messaging and `--scope-local` acceptance.
  - [x] prevent report-command rewrite hijack for non-OWASP local workflows:
    - tightened report-rewrite predicate to explicit OWASP/security-report intent (OWASP markers, security-report artifact naming, or report+security context), while excluding generic local report tasks.
    - added regression tests for local archive-report classification and rewrite skip behavior.
    - validated in `run-20260223-172947-zipsecret8`: `t5` executed native bash report command, produced `zip_crack_report.md`, and emitted zero `rewrote weak report command` events.
  - [x] prevent stale report file reuse during OWASP synthesis tasks when expected artifact names already exist in the working directory:
    - prefer current command stdout for security-report artifacts (`owasp_report.md`/`security_report`/`vulnerability_report`) in report-synthesis tasks instead of copying pre-existing files.
    - added regression coverage (`TestRunWorkerTaskReportSynthesisDoesNotReuseStaleReportFile`) to ensure stale local report files are not re-materialized into orchestrator artifacts.
  - [x] enforce goal-context CVE evidence collection for nmap service-scan steps in OWASP/security goals, including shell-wrapped commands:
    - when goal context requires vulnerability evidence and a task is service/recon nmap (non-`-sn`), auto-inject `--script "vuln and safe"` + `--script-timeout 20s` even for `bash -lc "nmap ..."` actions.
    - guarded command-token matching to avoid false rewrites when hostnames contain `nmap` (for example `scanme.nmap.org` in `dig` commands).
    - validated with `run-scanme-owasp-20260223-193859`: report task now synthesized evidence-backed CVE findings (`117` CVE IDs) from `t2/service_scan.txt`.
  - [x] tighten OWASP synthesis CVE evidence quality to reduce duplicate excerpts and prioritize high-confidence, target-relevant CVE findings (current report may include repeated lines and broad version-feed noise from vuln scripts).
    - report synthesis now scores per-line CVE evidence confidence (`high`/`medium`/`low`) using vulnerability markers + target relevance, then prioritizes stronger evidence over noisy reference/feed lines.
    - duplicate CVE evidence snippets are de-duplicated per file/line and capped to top-ranked entries to reduce repeated excerpts in the findings section.
    - added regression coverage (`TestRunWorkerTaskReportSynthesisPrioritizesHighConfidenceCVEEvidence`) to ensure high-confidence target-relevant excerpts are retained while low-quality feed/reference lines are excluded.
  - [x] fix expected-artifact materialization for command tasks:
    - prefer copying produced files from task working directory into orchestrator artifact store when present; use stdout fallback only when no produced file exists.
    - validated in local encrypted-archive workflow rerun (`run-20260223-172436-zipsecret5`): `t2/john_show.txt`, `t4/recovered_password.txt`, `t4/extraction_status.txt`, and `t4/extracted_preview.txt` now preserve real produced content instead of placeholder fallback text.
  - [x] add regression coverage for local encrypted-archive workflow:
    - added `TestLocalArchiveWorkflowArtifactsAndReportNoRewrite` to validate parallel crack-task execution (`t2`/`t3`), real artifact propagation (`john_show.txt`, `recovered_password.txt`, `extraction_status.txt`, `extracted_preview.txt`), and final `zip_crack_report.md` content in orchestrator artifact paths.
    - asserts local non-security report task (`t5`) is not auto-rewritten into OWASP synthesis (no `rewrote weak report command` event for `t5`).
  - [x] reduce benchmark terminal failures still dominated by `assist_loop_detected`/`assist_budget_exhausted` in `cross_agent_validation_false_claim` and `evidence_first_reporting_quality` (targeted `repeat=2` reruns now 2/2 pass each; post-hardening spot checks also 1/1 pass each in `benchmark-20260223-115835` and `benchmark-20260223-120100`; full-suite confirmation deferred to Sprint 37).
    - latest targeted stability check (`benchmark-20260223-170436`, `repeat=2` over both risky scenarios) regressed only on `evidence_first_reporting_quality-r02` with `command_failed` + downstream `assist_loop_detected`; failure trace shows assist emitted `bash` with a single string arg (`"nmap -sV -p- 127.0.0.1"`) causing `bash: ... No such file or directory` before recovery loop failure.
    - post-fix reruns recovered stability: `benchmark-20260223-170955` (`evidence_first_reporting_quality`, `repeat=2`) and `benchmark-20260223-171117` (both risky scenarios, `repeat=2`) completed `4/4` with zero loop incidents and zero failures.
  - [x] normalize assist-command shell invocation when assistant emits `bash`/`sh` with a single string command arg (auto-rewrite to `-lc <cmd>`) to prevent false `command_failed` on valid shell payloads.
    - `normalizeWorkerAssistCommand` now rewrites shell invocations like `bash ["nmap -sV -p- 127.0.0.1"]` to `bash ["-lc", "nmap -sV -p- 127.0.0.1"]` while preserving real script execution args.
    - added regression coverage: `TestNormalizeWorkerAssistCommandRewritesSingleArgShellCommand` and `TestNormalizeWorkerAssistCommandKeepsSingleArgShellScript`.
    - revalidated with targeted scenario rerun `benchmark-20260223-170955` (`evidence_first_reporting_quality`, `repeat=2`): both runs completed (`2/2`) with zero loop incidents and zero failures.
  - [x] make static planning fallback explicit-only for goal runs:
    - `--planner auto`/`--planner llm` now require LLM availability and no longer silently fall back to static; failures return an explicit hint to rerun with `--planner static`.
    - added bounded LLM planner retry (`2` attempts) before failing, with provenance note when retry succeeds.
    - CLI defaults for `run`/`benchmark` planner mode now `auto`; static mode remains available only when explicitly selected.
    - added regression coverage for auto-mode no-fallback behavior and retry-success behavior (`TestBuildGoalPlanFromModeAutoRequiresLLMWhenStaticNotExplicit`, `TestBuildGoalPlanFromModeAutoRetriesLLMPlannerThenSucceeds`).
- [x] prevent terminal-stop collapse after repeated `assist_loop_detected` on seed tasks by promoting to adaptive replan mutation instead of run terminalization.
    - map repeated `assist_loop_detected` (post-retry exhaustion) into event-driven graph mutation path so a recovery task is added, not just a `run_replan_requested` audit event.
    - ensure downstream tasks blocked on failed seed dependencies are rewired/superseded so queue remains runnable after recovery insertion.
    - added regression test `TestCoordinator_RepeatedSeedAssistLoopPromotesAdaptiveReplanAndRewiresDependencies` proving second seed-loop failure triggers adaptive replan and does not immediately terminalize the coordinator.
- [x] Re-run 5x baseline on current commit and refresh locked baseline after smoke is healthy (`benchmark-20260223-092006` locked to `docs/runbooks/autonomy-benchmark-baseline.json`).
- [x] Exit criteria deferred to Sprint 37 (full-suite rerun postponed by operator request).

## Sprint 36 — Project Salvage Attack Plan (planned, blocking)
- [ ] Phase 1 — Stabilize and instrument before more fixes:
  - [x] freeze non-salvage feature work and treat Sprint 37+ as provisional.
    - codified freeze policy in `docs/runbooks/salvage-experiment-ops.md` (`Sprint Freeze Policy`): Sprint 36 allows salvage-phase changes only; Sprint 37+ stays provisional until Sprint 36 exit gate passes.
  - [x] add a diagnostic run mode that minimizes adaptive rewrites so traces show root behavior.
    - added `--diagnostic` for `run` and `benchmark`, propagated to workers via `BIRDHACKBOT_ORCH_DIAGNOSTIC_MODE=true`.
    - diagnostic mode disables adaptive worker rewrites (target auto-injection, runtime command adaptation, missing-input auto-repair) while preserving fail-closed scope checks.
    - diagnostic mode auto-enables assist strict mode + per-turn raw LLM trace artifacts unless explicitly overridden by `--worker-env`.
    - added regressions: `TestRunWorkerTaskAssistDiagnosticModeSkipsShellRewrite`, `TestBenchmarkScenarioRunArgsIncludesDiagnosticFlag`, `TestAppendEnvIfMissing`.
  - [x] standardize run-ID + artifact checklist for every salvage experiment.
    - added `docs/runbooks/salvage-experiment-ops.md` with run-ID convention (`salvage-<problem-id>-<slice>-<timestamp>-s<seed>`) and required artifact checklist.
  - [x] define quota-aware validation cadence (fast smoke per change, focused regression on milestone, full suite manual/periodic only).
    - added explicit cadence tiers and command templates in `docs/runbooks/salvage-experiment-ops.md`.
- [x] Phase 2 — Problem register before implementation:
  - [x] create `docs/runbooks/problem-register.md` with severity, reproducibility, owner, and component mapping.
  - [x] log current known failures: zip discovery loops, repeated no-op commands, planner intermittency, report truth gaps, approval UX ambiguity.
  - [x] require a concrete repro recipe and acceptance signal for every registered problem.
- [ ] Phase 3 — Deterministic repro harness:
  - [x] add minimal reproducible scenarios for top failures (zip-local, OWASP report synthesis, approval stalls, planner truncation).
    - added `docs/runbooks/sprint36-repro-scenarios.json` with command templates and scenario mapping to problem-register IDs.
    - added deterministic active-probe approval template plan: `docs/runbooks/repro/approval-stall-plan.template.json`.
  - [x] pin seed/model/runtime flags and expected event signatures per scenario.
    - repro matrix now pins planner mode (`auto` for runtime cases), seed placeholders, approval timeout, worker command/args, and expected event/metric signatures.
  - [x] add regression checks that fail when known loop signatures reappear.
    - added `scripts/check_benchmark_gate.py` to fail on loop signatures (`assist_loop_detected`, `assist_budget_exhausted`, repeated low-value listing streaks, and `assist_no_new_evidence` task completion).
  - [x] add a "quick gate" benchmark pack (`<=10 min`) for routine iteration with hard pass/fail thresholds.
    - added `docs/runbooks/sprint36-quick-gate-scenarios.json` and runner `scripts/run_sprint36_quick_gate.sh`.
  - [x] quick gate must run with `--planner auto` (LLM active) across at least 2 seeds so exploration remains model-driven.
    - quick-gate runner executes two pinned seeds (`11`, `17`) with `--planner auto`.
  - [x] quick gate must enforce anti-loop limits (for example max identical low-value action streak per task) and fail on repeated `list_dir`/`ls -la` churn without new evidence.
    - quick-gate checker enforces `--max-low-value-streak` (default `3`) on command-log event traces.
  - [x] quick gate must require bounded progress semantics: each pivot cites new evidence or a concrete unknown; otherwise terminate as `no_progress` (not `completed`).
    - quick-gate checker rejects runs that complete tasks via `assist_no_new_evidence`.
  - [x] add a "full gate" benchmark pack (`repeat=5`) marked manual/periodic so it is not run on every change.
    - added `docs/runbooks/sprint36-full-gate-scenarios.json` and manual runner `scripts/run_sprint36_full_gate.sh`.
- [ ] Phase 4 — Root-cause telemetry and context integrity:
  - [ ] instrument observation truncation, memory-window eviction, retry-attempt resets, and repeated-command fingerprints.
  - [ ] emit per-attempt "what changed since prior attempt" summaries to detect blind repeats early.
  - [ ] classify each failure as context-loss, strategy-failure, or contract-failure before patching behavior.
  - [x] add context diagnostics artifact per task attempt (`context_envelope.json`) capturing: prompt payload sizes, observation counts, truncation counters, and retained anchors.
  - [x] persist critical execution anchors across retries (last successful target path(s), last command/result fingerprint, last concrete failure cause).
  - [x] stop resetting effective context on retry: carry forward bounded prior-attempt observations into next attempt.
  - [x] replace single rolling text blob with layered context payload (`facts`, `recent_actions`, `recent_artifacts`, `open_unknowns`) for assist input.
  - [x] add oversized-context smoke regression (`TestBuildWorkerAssistLayeredContextStressCompaction`) to prove compaction caps and retained-vs-dropped slices are deterministic.
  - [x] enforce context compaction retention policy: always keep anchors (`Goal`, `Planner decision`), keep newest dynamic facts/questions/actions/artifacts, and inject explicit `compaction_summary` lines into assist context.
  - [x] raise minimum retained observation budget and enforce token-aware compaction that preserves file paths/errors/targets first.
  - [x] add regression test: repeated `list_dir`/`ls -la` without new evidence must trigger alternate strategy or `no_progress`, never silent completion.
  - [ ] enforce orchestrator-owned shared-memory contract for parallel workers (single writer orchestrator; workers append-only via events/artifacts).
  - [ ] attach event-id provenance for promoted shared-memory facts (candidate -> verified/rejected auditability).
  - [ ] checkpoint protocol: after each Phase 4 implementation slice, run targeted tests + quick gate and log outcome in `docs/runbooks/problem-register.md`.
- [ ] Phase 5 — Contract corrections (minimal behavior changes first):
  - [ ] enforce run success semantics: `completed` requires goal-truth checks, not only task lease completion.
  - [ ] enforce report truth gates: findings/claims must map to verifier-backed evidence.
  - [ ] block terminal success when required artifacts/verifications are unresolved.
  - [ ] require every exploratory pivot to cite either a new evidence anchor or explicit unknown under test.
  - [ ] adopt and enforce "hard support exception policy" from `docs/runbooks/architecture-recovery-plan.md` (generic-first, capability-scoped exceptions only).
  - [ ] missing-tool recovery contract: if a recommended tool is unavailable, request operator approval for install (when policy allows); otherwise force LLM replan against available tools without repeated missing-tool retries.
- [ ] Phase 6 — Early role deployment (thin slice):
  - [ ] introduce a read-only validator role/worker that independently confirms or rejects candidate findings.
  - [ ] route report synthesis through validator verdict states (`verified`/`rejected`/`unverified`).
  - [ ] defer full multi-role expansion until execution/context reliability is stable.
- [ ] Phase 7 — Cleanup sweep to reduce accidental complexity:
  - [ ] split oversized runtime/planner files by responsibility with no behavior change.
  - [ ] remove overlapping adapters/fallbacks after contracts are enforced.
  - [ ] document ownership boundaries for planner, executor, verifier, and reporter.
- [ ] Phase 8 — Exit gate (must pass before Sprint 37 starts):
  - [ ] zip regression passes `>=5/5` under LLM planner without repeated no-op loop signatures.
  - [ ] planner success/retry metrics meet agreed thresholds on the salvage smoke matrix.
  - [ ] reports include concise human-readable method/results and zero unverified critical claims.
  - [ ] quick-gate benchmark trend is non-regressing across salvage commits (token/time budget respected).
  - [ ] hold sprint replanning review to rewrite Sprint 37-43 scope based on measured outcomes.

## Sprint 37 — Evidence-Backed Exploration State (planned, provisional)
- [ ] Prerequisite: Sprint 36 exit gate completed; otherwise keep this sprint queued and revise.
- [ ] [Deferred from Sprint 35] Full benchmark-suite confirmation:
  - [ ] prerequisite: restore stable LLM endpoint availability for unattended reruns.
  - [ ] rerun full benchmark suite (`repeat=5`) on current hardening commit.
  - [ ] confirm baseline is reproducible on the same commit.
  - [ ] confirm scorecard artifacts are complete for every scenario run.
- [ ] Architecture evaluation + cleanup sweep before further hardening:
  - [ ] produce architecture recovery doc with current failure classes, ownership boundaries, and acceptance criteria (`docs/runbooks/architecture-recovery-plan.md`).
  - [ ] define explicit planner->executor contract (action shape, artifact contract, success semantics) and reject off-contract behavior instead of adding runtime special-cases.
  - [ ] run a no-behavior-change cleanup pass to split oversized files (`worker_runtime.go`, `worker_runtime_assist_loop.go`, `main_planner.go`) into focused units.
  - [ ] remove or consolidate redundant adapters/heuristics that overlap in responsibility (input repair vs command adaptation vs artifact synthesis).
  - [ ] add regression coverage for run terminal semantics so `completed` requires success criteria evidence, not only task lease completion.
  - [ ] gate report claims behind verified evidence (no "password recovered"/"CVE found" statements without matching verifier evidence).
- [x] Harden LLM planner reliability contract for goal runs:
  - [x] enforce schema-constrained planner output (`response_format=json_schema`) before parse/validate.
  - [x] replace fixed retry with bounded adaptive retries (attempt cap + wall-clock cap + backoff + context narrowing).
  - [x] capture LLM `finish_reason` and classify length-cutoff planner responses as truncation failures for explicit diagnostics.
  - [x] clamp LLM planner task budgets to synthesized safety caps (`max_steps`, `max_tool_calls`, `max_runtime`) before plan validation to reduce avoidable retry churn.
  - [x] add planner action-shape validation for shell wrapper commands (`bash|sh|zsh -c/-lc`) to reject malformed/truncated command bodies before execution.
  - [x] persist planner-attempt diagnostics (stage/cause/fingerprint + raw/extracted payload artifacts) under sessions for postmortem.
  - [x] keep failure explicit when LLM planning cannot recover, with static-planner rerun hint.
  - [x] validation: `go test ./cmd/birdhackbot-orchestrator ./internal/orchestrator` and `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`.
- [x] Harden local archive runtime resilience for goal-seeded zip workflows:
  - [x] fix scheduler terminalization for queued tasks with transitive failed dependencies (avoid `state=running` with no runnable work).
  - [x] improve runtime missing-path repair to prefer valid local workspace/dependency paths for command and shell-wrapper actions.
  - [x] auto-bootstrap missing wordlists from compressed archives (for example `rockyou.txt.gz`) into short local cache path (`/tmp/birdhackbot-wordlists/`).
  - [x] validation: `run-zip-wordlistfix2-20260225-200114` and `run-zip-validate-20260225-201152` both completed with `8/8` tasks.
- [ ] Resolve residual local-file scope false positives for relative artifact arguments (for example `zip.hash` misclassified as out-of-scope target in `run-zip-reg3-20260225-201808-2` `T-004`/`T-005`) and revalidate zip regression to `3/3` pass.
- [ ] Add explicit hypothesis/evidence state tracking for assist worker decisions.
- [ ] Require exploratory pivots to cite either new evidence or a concrete unknown/hypothesis gap.
- [x] Add finding lifecycle states in runtime flow: `hypothesis -> candidate -> verified|rejected`.
- [x] Ensure planner/recovery context only treats `verified` findings as assumptions.
  - finding ingestion now normalizes/persists `finding_state` (`hypothesis`/`candidate_finding`/`verified_finding`/`rejected_finding`) with deterministic merge behavior.
  - memory bank known facts now include only `verified_finding` items; unverified findings are excluded from assumption context and replaced with explicit `No verified findings yet.` when applicable.
  - runtime-emitted execution-result findings are tagged `verified_finding`; tests added for lifecycle state merge and memory filtering.
- [ ] Enforce discovery-time verification (`verify-now`) immediately after any vulnerability claim before downstream planning continues.
- [ ] Add tests for evidence-linked pivots vs blind repeat pivots.
- [ ] Add tests that hallucinated findings are marked `rejected` and do not influence subsequent steps.
- [ ] Exit criteria:
  - [ ] reduced blind pivots in benchmark traces
  - [ ] improved verified finding precision vs Sprint 35 baseline
  - [ ] verification lag (claim -> verified/rejected) stays within bounded step budget

## Sprint 38 — Recovery Strategy Diversification (planned, provisional)
- [ ] Prerequisite: Sprint 36 exit gate completed; revise scope if salvage findings invalidate assumptions.
- [ ] Add recovery policy that enforces strategy-class changes after repeated failures.
- [ ] Prevent near-duplicate retry loops (semantic intent class, not just exact command string).
- [ ] Add bounded guard for repeated non-tool churn (`command`/`plan`) similar to tool-loop guard.
- [ ] Reduce dependence on `assist_no_new_evidence` terminal fallback for auth/recon helper-script alternation in `host_discovery_inventory`.
- [ ] Tighten no-new-evidence fallback trigger thresholds so recover churn terminates earlier without suppressing real progress.
- [ ] Add benchmark regression test where assistant alternates near-duplicate command intents and verify fail-fast classification.
- [ ] Add tests for forced alternative strategy paths after repeated blocks.
- [ ] Add independent finding validation lane in orchestrator:
  - [ ] route candidate findings to a separate verifier worker/agent
  - [ ] verifier must reproduce or reject without reusing discoverer conclusions as truth
  - [ ] discoverer cannot self-mark findings as `verified`
- [ ] Add high-severity skeptic pass (attempt falsification) before `critical/high` findings are accepted.
- [ ] Exit criteria:
  - [ ] loop incident rate reduced vs baseline
  - [ ] recovery success rate improved vs baseline
  - [ ] reduced false-positive carry-forward from initial discovery steps

## Sprint 39 — Novelty Scoring + Anti-Redundancy (planned, conditional, provisional)
- [ ] Prerequisite: Sprint 36 exit gate completed.
- [ ] Prerequisite: execute only if Sprint 38 loop/fallback metrics are still below exit criteria.
- [ ] Add novelty scoring for actions/evidence and feed score into recovery/planning prompts.
- [ ] Penalize repeated low-value actions when no new evidence is produced.
- [ ] Add tests for novelty gain and anti-redundancy behavior.
- [ ] Exit criteria:
  - [ ] higher novel-evidence-per-step vs baseline
  - [ ] no regression in safety metrics

## Sprint 40 — Kali Controlled Pilot (planned, provisional)
- [ ] Prerequisite: Sprint 36 exit gate completed and Sprint 37/38 contract metrics are stable.
- [ ] Run the same benchmark pack on Kali in authorized internal lab only.
- [ ] Compare Kali metrics to local baseline with documented tolerance bands.
- [ ] Capture full event/artifact bundles for reproducibility.
- [ ] Exercise independent validator flow on Kali scenarios (discoverer -> verifier -> skeptic for high severity).
- [ ] Exit criteria:
  - [ ] Kali medians within tolerance on core metrics
  - [ ] no policy/scope violations during pilot runs
  - [ ] no candidate finding is reported as verified without validator evidence

## Sprint 41 — Regression Gates + Revert Discipline (planned, provisional)
- [ ] Prerequisite: Sprint 36 exit gate completed.
- [ ] Add targeted benchmark regression gate to CI for key scorecard metrics (`smoke` + highest-risk scenarios); keep full-suite gate as periodic/manual until runtime budget is acceptable.
- [ ] Add explicit revert policy and threshold checks (auto-fail gate on severe regressions).
- [ ] Keep report-time backstop gate strict: only `verified` findings in confirmed findings section; all other claims excluded or labeled `UNVERIFIED`.
- [ ] Document operator workflow for rollback when metrics degrade.
- [ ] Exit criteria:
  - [ ] merge blocked on benchmark regression
  - [ ] revert decision path documented and tested

## Sprint 42 — Wireless Access Security (planned, lab-only, provisional)
- [ ] Prerequisite: Sprint 36 exit gate completed.
- [ ] Define wireless scope contract (SSID/BSSID/channel/interface allowlists + deny lists) for authorized internal lab environments.
- [ ] Add wireless command policy/guardrails in runtime:
  - [ ] classify wireless tooling risk tiers (passive recon, auth probing, handshake capture, rogue-AP simulation).
  - [ ] require explicit session opt-in + per-action approval for disruptive wireless actions (for example deauth/evil-twin simulation).
  - [ ] keep fail-closed scope validation for wireless identifiers (SSID/BSSID/channel/interface) to prevent out-of-scope capture/transmit.
- [ ] Add orchestrator/worker adapters for core Kali wireless tooling with bounded defaults (timeouts, channel pinning, capture limits, output normalization).
- [ ] Add goal/planner playbook for wireless assessments:
  - [ ] exposed/weak Wi-Fi posture discovery (open/WEP/WPA/WPA2/WPA3, PMF, WPS, management exposure).
  - [ ] controlled access validation workflow for owner-authorized lab APs.
  - [ ] controlled spoofed-AP client-behavior simulation in isolated lab segment (no third-party/client impact).
- [ ] Add evidence schema and artifacts for wireless runs:
  - [ ] AP inventory, client association map, auth posture matrix, capture metadata, and test-attempt ledger.
  - [ ] explicit chain-of-custody metadata for capture files (timestamp, interface, channel, BSSID scope match).
- [ ] Add OWASP-style wireless reporting extensions:
  - [ ] human-readable execution narrative (steps/method/results) plus evidence links.
  - [ ] findings taxonomy for wireless misconfigurations and spoofing exposure with clear remediation guidance.
- [ ] Add regression tests for wireless guardrails and parsing:
  - [ ] out-of-scope BSSID/SSID rejection.
  - [ ] approval-gated rogue-AP/deauth execution.
  - [ ] deterministic artifact materialization and report synthesis from wireless evidence.
- [ ] Add lab-only benchmark scenarios for wireless workflows and include them in controlled Kali pilot once stable.
- [ ] Exit criteria:
  - [ ] no out-of-scope wireless capture/transmit in tests.
  - [ ] operator-visible approvals explain exactly what wireless action is requested and why.
  - [ ] generated wireless reports include concise human-readable method/results plus reproducible evidence links.

## Sprint 43 — Bluetooth Security (planned, blocked until Sprint 42 is complete, provisional)
- [ ] Prerequisite: Sprint 36 exit gate completed.
- [ ] Prerequisite: Sprint 42 must be completed (or unfinished tasks explicitly moved) before Sprint 43 starts.
- [ ] Keep Bluetooth work explicitly lower priority than Sprint 42 Wi-Fi scope.
- [ ] Define Bluetooth lab scope contract (adapter/controller allowlist, target device allowlist, prohibited actions).
- [ ] Add runtime guardrails for Bluetooth tooling:
  - [ ] fail-closed scope validation for device identifiers and adapter selection.
  - [ ] explicit approval gating for risky actions (pairing attempts, active exploitation, long-running captures).
- [ ] Add orchestrator/worker adapters for core Kali Bluetooth tooling with bounded defaults (timeouts, retry limits, output normalization).
- [ ] Add planner playbook for Bluetooth assessments:
  - [ ] discovery and service enumeration of authorized lab targets.
  - [ ] controlled auth/pairing posture validation and known-vulnerability checks.
- [ ] Add evidence/reporting support:
  - [ ] concise execution narrative (method/results) plus reproducible evidence links.
  - [ ] findings taxonomy for exposure, weak configuration, and vulnerability remediation.
- [ ] Exit criteria:
  - [ ] no out-of-scope Bluetooth interaction in tests.
  - [ ] approvals clearly explain requested action and reason.
