# Rebuild Plan

This task list derives from `docs/architecture.md`.
The architecture document is the source of truth for the rebuild branch.

Planning rules:
- `TASKS.md` is the executable plan for the current phase and, optionally, the next phase.
- `ROADMAP.md` is directional only and must not contain detailed task lists.
- `DISCOVERIES.md` records lessons, risks, and future-phase notes; it is not a task list.
- If `TASKS.md` and `ROADMAP.md` differ, `TASKS.md` is authoritative for execution.

## Phase 0 — Reset
- [x] Aggressively archive historical docs into `docs/archive/`
- [x] Reduce active docs to the minimal set
- [x] Move the pre-rebuild implementation into `legacy/` so old and new code cannot be confused
- [x] Write and freeze the v1 baseline architecture document with user review before adoption
- [x] Rewrite `README.md` to match the rebuild branch state

Exit criteria:
- architecture frozen at `docs/architecture.md`
- legacy/new separation is clear
- active planning/documentation structure is clean

## Phase 1 — Minimal Worker Loop

Status:
- complete

Goal:
- build the smallest interactive worker loop that follows `docs/architecture.md`

Tasks:
- [x] Create the new rebuild-root Go module and minimal package layout
- [x] Implement the minimal behavior-frame loader, including `AGENTS.md`
- [x] Implement minimal worker session foundation:
  - goal
  - reporting requirement
- [x] Implement the minimal worker context packet v1
- [x] Implement exact-action execution with:
  - minimal shell wrapping only when needed
  - full command logging
  - execution-result capture
- [x] Implement the minimal worker closed loop:
  - ask LLM for next action
  - validate approval/executability
  - execute exact action
  - feed result back
- [x] Implement the minimal approval model:
  - `this time`
  - `always allow` (session-scoped)
  - `no`
  - `--allow-all`
- [x] Implement context inspection for live diagnosis
- [x] Implement state-based session resume
- [x] Run live validation on:
  - one `secret.zip` workflow
  - one router workflow
- [x] Improve generic execution-result assessment:
  - add result `assessment`
  - add generic result `signals`
  - surface ambiguous/suspicious outcomes in active context
- [x] Improve generic completion judgment:
  - prefer `step_complete` when the goal is already satisfied by evidence
  - avoid rereading the same evidence indefinitely
- [x] Improve generic bounded-action judgment:
  - prefer actions that fit an interactive loop
  - avoid broad expensive commands when a smaller action can establish the next fact
- [x] Add minimal structured worker task context:
  - task state
  - current target
  - missing fact
- [x] Implement the minimal interactive worker CLI shell:
  - persistent prompt loop
  - session continuity across turns
  - reuse the same worker loop
  - keep it simple; no full TUI yet

Exit criteria:
- worker loop runs end-to-end with real LLM calls
- no fallback command synthesis exists in the new path
- exact commands and execution results are logged
- active context packet is inspectable during runs
- both live scenarios have been exercised with understandable behavior, even if not yet perfect
- router-style reconnaissance can complete cleanly on a bounded local target
- a minimal interactive worker CLI shell exists for direct user testing

## Phase 2 — Active Context Quality

Goal:
- make active context truth, target stability, and inspectability reliable before adding deeper memory mechanisms

Planned tasks:
- [x] Implement running summary as an explicit active section
- [x] Tighten truth ordering inside active context
- [ ] Improve target stability against noisy latest evidence without adding scenario-specific guardrails
- [x] Add visibility into included vs excluded context material and approximate size
- [x] Add interactive shell inspection commands for live multi-turn context testing:
  - `/stats`
  - `/packet`
  - `/lastlog`
- [ ] Re-run repeated live validation on:
  - `secret.zip`
  - router/local recon
- [ ] Only after active-context quality is stable, begin minimal memory-bank v1

## Working Rules
- [ ] No patch-first behavior on this branch
- [ ] No new behavior that conflicts with the architecture document
- [ ] Every major implementation slice must be followed by real LLM validation
- [ ] Behavioral conclusions from live validation should use repeated runs, not single examples
- [ ] Prefer deletion over adaptation when both solve the same problem
