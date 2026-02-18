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
- `docs/playbooks/` and `docs/tooling/`: operator/playbook guidance (inspiration, not rigid scripts).
- `sessions/<id>/`: per-session evidence and memory artifacts.

## Build, Test, and Local Run
- `go test ./...` — run full test suite.
- `go build ./cmd/birdhackbot` — build CLI binary.
- `go run ./cmd/birdhackbot --profile <name>` — run directly with optional profile.
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

## Third-Party Inspiration Policy
- Cline is reference-only for concepts/workflow.
- Never copy third-party code/docs verbatim without proper license review and attribution.

## Near-Term Roadmap (Summary)
- Stabilize TTY rendering (fixed input row + status bar) across terminal variants.
- Continue hardening the generic agent loop (recovery, progress detection, anti-loop behavior).
- Improve report quality/templates (OWASP + NIS2-aligned output structure).
- Prepare orchestrator contracts (`plan/task/event/artifact/finding`) for multi-agent future work.

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
