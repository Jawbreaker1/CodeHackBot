# Repository Guidelines

## Project Purpose
BirdHackBot is an AI-assisted, CLI-first security testing agent for authorized lab environments.  
The MVP focus is: plan work, run safe/approved actions, adapt to failures, preserve evidence, and generate reproducible reports.

All authorization/scope constraints in `AGENTS.md` are mandatory.

## Current Repository Structure
- `cmd/birdhackbot/main.go`: CLI entrypoint (`--resume`, `--replay`, config/profile/permission overrides, signal handling).
- `internal/cli/`: interactive shell, command routing, agent loop, prompt/status rendering, reporting, web helpers.
- `internal/assist/`: LLM suggestion protocol (`command`, `question`, `plan`, `tool`, `complete`) + fallback assistant.
- `internal/exec/`: guarded command runner, scope checks, approval integration, live output/log capture.
- `internal/memory/`: file-first memory manager (summary/facts/focus/context state, compaction).
- `internal/llm/`: LMStudio-compatible chat client + failure/cooldown guard.
- `internal/report/`, `internal/msf/`, `internal/plan/`, `internal/replay/`, `internal/session/`: reporting, metasploit search parsing, planning, replay, session scaffolding.
- `config/default.json` (+ optional `config/profiles/*.json`): runtime behavior and thresholds.
- `docs/playbooks/`, `docs/tooling/`, `docs/runbooks/`: playbooks, tool references, and operator runbooks.
- `sessions/<id>/`: per-session evidence and memory artifacts.

## Build, Test, and Local Run
- `go test ./...` — run full test suite.
- `go build -buildvcs=false ./cmd/birdhackbot` — build CLI binary (avoids VCS stat-cache warnings in restricted environments).
- `go build -buildvcs=false ./cmd/birdhackbot-orchestrator` — build orchestrator binary (avoids VCS stat-cache warnings in restricted environments).
- `go run ./cmd/birdhackbot --profile <name>` — run directly with optional profile.
- `./birdhackbot-orchestrator tui --sessions-dir sessions --run <run-id>` — interactive orchestrator TUI (status, workers, approvals, events, prompt commands).
- `./birdhackbot-orchestrator benchmark --sessions-dir sessions --worker-cmd ./birdhackbot --worker-arg worker --repeat 5 --seed 42 --lock-baseline` — run autonomy benchmark pack and optionally lock baseline metrics.
- `./birdhackbot --resume <session-id>` — continue an interrupted session.
- `./birdhackbot --replay <session-id>` — replay `sessions/<id>/replay.txt` with current scope/permissions.

## CLI Behavior (Implemented)
- Plain text defaults to agentic `/assist` mode.
- `/ask` is explicit non-agentic chat.
- Safety/ops commands: `/permissions`, `/status`, `/context`, `/context usage`, `/ledger`, `/clean`, `/resume`, `/stop`.
- Execution commands: `/run`, `/msf`, `/browse`, `/crawl`, `/links`, `/read`, `/ls`, `/write`, `/script`.
- Planning/report commands: `/plan`, `/next`, `/summarize`, `/report`.

## Context & Memory Model (Implemented)
- File-first memory under `sessions/<id>/`:
  - `summary.md`, `known_facts.md`, `focus.md`, `context.json`, `chat.log`
  - command logs in `logs/`
  - generated artifacts in `artifacts/`
- Auto-compaction runs on configured thresholds (`max_recent_outputs`, `summarize_every_steps`, `summarize_at_percent`).
- `/status` and `/context usage` report window-fill usage (buffer and summarize windows), not raw model token count.

## Session Artifact Expectations
Minimum session outputs:
- `plan.md`, `inventory.md`, `logs/`, `artifacts/`, `summary.md`, `known_facts.md`, `focus.md`, `context.json`
- optional `ledger.md` when `/ledger on`
- optional `report.md` via `/report`

## Safety and Scope Enforcement
- Permission levels: `readonly`, `default` (approval required), `all`.
- Scope enforcement uses `scope.networks`, `scope.targets`, `scope.deny_targets`.
- Runner blocks obvious out-of-scope targets and enforces approvals in default mode.
- Kill/interrupt behavior: Ctrl-C/SIGTERM should stop active actions and close session cleanly.

## Coding Conventions
- Language: Go.
- Formatting: always `gofmt` (and keep imports clean).
- Tests: colocated `*_test.go`, favor focused regression tests for each bug fix.
- Keep files modular; refactor early when files become hard to navigate.
- Prefer internal primitives (`/browse`, `/crawl`, `/read`, `/ls`, `/write`) over brittle shell pipelines.

## Commit & PR Conventions
- Follow Conventional Commit style (observed in history): `feat:`, `fix:`, `refactor:`, `docs:`.
- Include tests with behavior changes.
- For UI/TTY changes, include screenshot evidence in PR notes.

## Reporting Standards
- OWASP-style reporting is baseline.
- Findings must be reproducible and linked to evidence (commands + log paths).
- Include impact, reproduction steps, and remediation guidance.
- Runtime finding policy: vulnerability claims must be re-evaluated near discovery time and only promoted to trusted context after verification.

## Third-Party Inspiration Policy
- Cline is reference-only for concepts/workflow.
- Never copy third-party code/docs verbatim without proper license review and attribution.

## Near-Term Roadmap (Summary)
- Stabilize TTY rendering (fixed input row + status bar) across terminal variants.
- Continue hardening the generic agent loop (recovery, progress detection, anti-loop behavior).
- Improve report quality/templates (OWASP + NIS2-aligned output structure).
- Prepare orchestrator contracts (`plan/task/event/artifact/finding`) for multi-agent future work.
- Execute the autonomy benchmark program and Kali transfer gate in `docs/runbooks/autonomy-benchmark-program.md`.

## Orchestrator Decisions (Locked for MVP)
### Binary and Runtime Boundary
- Orchestrator is a separate binary (`cmd/birdhackbot-orchestrator`), not a mode flag in `birdhackbot`.
- Worker agent remains independently runnable as today; orchestrator must never be required for single-agent usage.
- Shared logic stays in `internal/*` packages; binaries differ only at orchestration/control layer.

### Worker Launch Model
- Orchestrator launches new worker processes on demand (subprocess model), each with isolated session workspace.
- MVP does not attach to already-running worker sessions.
- Orchestrator is responsible for worker lifecycle:
  - start workers for leased tasks,
  - stop workers when task completes or lease expires,
  - cleanup idle/failed workers and close session metadata.
- Isolation means artifact/log/work-session separation, not OS-level chroot/jail:
  - worker writes default to `sessions/<worker-session-id>/...`
  - worker still uses system tools from normal PATH (`nmap`, `msfconsole`, `python3`, etc.)
  - orchestrator may pass write allowlists but must not expand scope.

### Worker Liveness and Stall Policy (MVP)
- Worker heartbeat is emitted concurrently (background goroutine) while task execution is in progress.
- Liveness signals used by orchestrator:
  - process alive check (PID still running),
  - heartbeat events in `event.jsonl`,
  - progress events (step/action updates).
- Locked defaults:
  - heartbeat interval: `5s`
  - stale timeout: `20s`
  - soft-stall grace: `30s`
  - retries per task: `2`
  - retry backoff: `5s`, then `15s`

### Approval and Timeout Policy (MVP)
- Approvals are orchestrator-managed, not worker-stdin prompts.
- Worker emits `approval_requested` event with command/risk/context and transitions task to `awaiting_approval`.
- Orchestrator owns approval queue + user actions:
  - approve once,
  - approve by task scope,
  - approve by session scope (within existing permission/scope limits).
- While task is `awaiting_approval`, execution timers are paused:
  - lease stale timer paused,
  - execution timeout paused,
  - no retry consumed.
- Separate approval-wait timeout is used:
  - default `45m`,
  - on expiry task becomes `blocked` (not `failed`) with reason `approval_timeout`.
- Required approval events:
  - `approval_requested`,
  - `approval_granted`,
  - `approval_denied`,
  - `approval_expired`.
- Goal: preserve human control without stalling automation or creating false timeout failures.

### Risk Tier and Approval Matrix (MVP)
- Every task/tool action is tagged with one risk tier. Tier controls approval behavior and defaults.
- Tier definitions (locked):
  - `recon_readonly`: passive or read-only intelligence gathering (DNS lookups, banner/version reads, local file reads, metadata collection). No state change on target.
  - `active_probe`: direct probing/enumeration that actively touches targets (port scans, directory discovery, protocol negotiation, safe fuzzing). Intended non-destructive but can increase load/noise.
  - `exploit_controlled`: controlled exploit validation to prove a finding with minimal-impact payloads and explicit scope target.
  - `priv_esc`: privilege-escalation attempts after foothold to validate lateral/vertical impact in-scope.
  - `disruptive`: DoS, persistence, destructive actions, or real data exfiltration. Prohibited by default.
- Approval matrix by permission mode:
  - `readonly`:
    - allow: `recon_readonly`
    - deny: `active_probe`, `exploit_controlled`, `priv_esc`, `disruptive`
  - `default`:
    - auto-allow: `recon_readonly`
    - per-action approval: `active_probe`, `exploit_controlled`, `priv_esc`
    - deny by default: `disruptive` (requires explicit session opt-in + documented approval)
  - `all`:
    - pre-approved within scope: `recon_readonly`, `active_probe`, `exploit_controlled`
    - high-risk confirmation or session pre-approval required: `priv_esc`
    - deny by default: `disruptive` unless explicit session opt-in is set
- Session pre-approvals are scoped and expiring:
  - scope: once, task, or session
  - must never override target scope restrictions
  - each grant/deny is auditable (actor, reason, scope, expiry).
- Hard guardrails:
  - out-of-scope target actions are always denied, regardless of permission mode or pre-approval.

### Task State Machine Policy (MVP)
- Task lifecycle is explicit and transition-validated:
  - `queued -> leased -> running -> completed`
  - `queued -> leased -> running -> failed`
  - `queued -> leased -> running -> blocked`
  - `queued -> leased -> running -> awaiting_approval -> running`
  - `queued -> leased -> running -> canceled`
- Invalid direct jumps are rejected (for example `queued -> completed`, `blocked -> running` without requeue).
- Retry rules:
  - retries are only allowed from `failed` with retryable reason,
  - retry transition is `failed -> queued` with `attempt + 1`,
  - retries are bounded by configured max attempts.
- Blocked rules:
  - `approval_timeout`, `missing_prereq`, `scope_denied` are `blocked` outcomes (not `failed`).
- Idempotency:
  - each lease execution uses a stable idempotency key (`run_id + task_id + attempt + lease_id`) to prevent duplicate side effects after restarts.
- Crash/restart reconciliation:
  - on orchestrator startup, tasks in `leased`/`running`/`awaiting_approval` are reconciled via PID + heartbeat checks,
  - stale or orphaned leases are moved through deterministic recovery path (`requeue`, `blocked`, or `failed` with reason).

### Worker-Orchestrator Communication Protocol (MVP)
- Transport is file-based only for MVP (single Kali VM runtime, no network transport/API dependency).
- Primary channel is append-only `event.jsonl` under orchestrator run directory.
- Required event envelope fields:
  - `event_id`
  - `run_id`
  - `worker_id`
  - `task_id` (or empty for run-level events)
  - `seq` (monotonic per worker)
  - `ts` (RFC3339/UTC)
  - `type`
  - `payload` (typed object)
- Task handoff contract:
  - orchestrator writes lease/task assignment artifact,
  - worker must emit `task_started` within startup SLA or lease is reclaimed.
- Write durability/safety:
  - workers and orchestrator use atomic write pattern (`*.tmp` + rename),
  - never emit partial JSON objects to event stream.
- Ordering and dedupe:
  - ordering is guaranteed per worker by `seq`,
  - orchestrator deduplicates by `event_id` and ignores duplicates.
- Recovery semantics:
  - orchestrator can rebuild in-memory state by replaying `event.jsonl` deterministically on restart.

### Planning and Replanning Policy (MVP)
- Plan-first gate is mandatory:
  - no worker execution starts before a run plan exists with scope, constraints, success criteria, and stop criteria.
- Task quality contract is mandatory before lease:
  - `done_when`,
  - `fail_when`,
  - `expected_artifacts`,
  - `risk_level`,
  - execution budget (`max_steps`, `max_tool_calls`, `max_runtime`).
- Budget model:
  - each task has local execution budget,
  - run has global budget ceiling to prevent unbounded exploration.
- Replan triggers (must be explicit and machine-detectable):
  - repeated-step loop detection,
  - approval denied/expired,
  - required artifact missing after retries,
  - stale lease recovery or worker crash.
- Replan outcomes are bounded and explicit:
  - refine existing task,
  - split task into smaller tasks,
  - terminate task as `blocked`/`failed` with reason.
- Silent infinite retries are prohibited; each replan consumes bounded budget and emits reasoned events.
- Finding promotion policy: candidate vulnerability findings should be independently validated by a separate worker/agent before being promoted to verified context (especially for high/critical severity).
- End-of-task output contract:
  - `result` status,
  - `summary`,
  - `evidence_refs`,
  - `next_suggested_tasks`.

### Plan and Task Contracts
- `plan.json` (global):
  - run metadata, scope/constraints, success criteria, stop criteria, max parallelism.
  - task list with `task_id`, `title`, `goal`, `depends_on`, `priority`, `strategy`.
- `task.json` (per task/lease):
  - `task_id`, `lease_id`, `worker_id`, `status`, `attempt`, `started_at`, `deadline`.
- `event.jsonl` (append-only):
  - canonical events: `run_started`, `task_leased`, `task_started`, `task_progress`, `task_artifact`, `task_finding`, `task_failed`, `task_completed`, `worker_started`, `worker_stopped`, `run_stopped`, `run_completed`.
- `artifact.json` / `finding.json`:
  - normalized outputs for evidence merge and report generation.

### Scheduler Policy (MVP)
- Queue + dependency-aware scheduler with configurable `max_workers`.
- Lease/heartbeat model:
  - lease timeout and heartbeat interval per task,
  - reclaim task on stale lease,
  - bounded retries with backoff.
- Scheduling is strategy-driven (parallel lanes), not tool-specific hardcoded flows.

### Stop and Safety Semantics
- Global stop broadcasts to all workers.
- Per-worker stop is supported.
- Stop sequence:
  - soft cancel request,
  - timed SIGTERM,
  - final SIGKILL if still running.
- Worker-side scope and permission gates remain enforced locally; orchestrator may only tighten limits.

### Evidence Merge Policy
- Findings deduplicated by stable key (`target + finding_type + location + normalized_title`).
- Conflicting findings are retained with confidence/source metadata (never silently dropped).
- Final report is generated from merged findings + linked artifacts/log paths.

### Interface Plan
- MVP interface is CLI-only for orchestrator.
- Future GUI will consume the same event/contracts via API adapter; no orchestration logic in UI layer.
- Core orchestrator engine must remain headless and reusable by both CLI and future web server.

### Orchestrator Test Policy (MVP)
- Orchestrator core changes are test-first: state transitions, scheduler behavior, lease/approval logic, and replay correctness.
- No orchestrator feature is considered complete without tests for expected behavior and failure behavior.
- Test layers from project start:
  - unit tests (validation, transition rules, policy matrix),
  - simulation tests (parallel scheduling, retries, reclaim, stop propagation),
  - end-to-end run tests (start -> worker fan-out -> evidence merge -> complete).
- Event/replay tests must use deterministic fixtures under `testdata/` when practical.
- Build gate remains mandatory before merge: `go test ./...` and `go build -buildvcs=false ./cmd/birdhackbot`.
