# Workflow State Contract

Date: 2026-03-06  
Status: Active (Sprint 37 control doc)

## Purpose

Single operator/reference doc for:
- state/status ownership,
- task workflow phases (`plan -> execute -> recover -> finalize`),
- terminal invariants.

Use this to prevent overlap, ambiguous transitions, and accidental state-model growth.

## Authoritative vs Derived

Authoritative lifecycle domains:
- `TaskState` (`internal/orchestrator/state_machine.go`)
- `RunOutcome` (`internal/orchestrator/run_outcome.go`)
- `RunPhase` (`internal/orchestrator/run_phase.go`)

Derived projection domains (read-only, never truth source):
- `RunStatus.State` (`internal/orchestrator/event_projection.go`)
- `WorkerStatus.State` (`internal/orchestrator/event_projection.go`)
- task projection (`queued|running|done`)

## Workflow Model

### Orchestrator task workflow
1. `plan`:
   - run plan + task graph prepared.
2. `execute`:
   - task leased/running and command or assist action executes.
3. `recover`:
   - bounded retries/repairs/replan when execution contract fails.
4. `finalize`:
   - emit terminal task event + completion contract or failure reason.

### CLI goal workflow
1. `plan`:
   - optional plan suggestion and operator approval.
2. `execute`:
   - step command/tool execution.
3. `recover`:
   - bounded correction after failed/invalid step.
4. `finalize`:
   - explicit completion contract (`objective_met`, `why_met`, `evidence_refs`) or explicit not-met.

## Terminal Invariants

Run terminal invariants:
- interrupted/stopped runs must still emit report artifact/event,
- `run_outcome` and `run_phase` must be terminalized coherently,
- terminal run status headline counters must show:
  - `active_workers=0`
  - `running_tasks=0`

Task terminal invariants:
- no `task_completed` without completion contract,
- completion contract must be evidence-backed,
- usage/help/no-op outputs cannot produce `task_completed`.

## Change Control (Maintenance Rule)

Any PR that changes state enums, transitions, completion payload shape, or terminalization flow must update:
- this file,
- `docs/runbooks/state-inventory.md`,
- related regression tests.

If these are not updated together, the change is incomplete.
