# CLI Live-Gate Harness Plan

Date: 2026-03-06  
Status: Planned (Sprint 37)

## Goal

Define a bounded, deterministic way to run live CLI acceptance gates without queued-input overhang, while preserving normal agent autonomy inside each run.

## Scope

- In scope:
  - Live gate driver behavior for CLI runs.
  - Terminal outcome detection (`complete` or explicit `objective_not_met`).
  - Evidence capture required by acceptance gates.
- Out of scope:
  - Changing core CLI reasoning strategy.
  - Hardcoding task/tool sequences for specific scenarios.

## Proposed Harness Behavior

1. Start one session per scenario run with a single goal prompt.
2. Drive interaction adaptively:
   - send `continue` only when objective is still unresolved.
   - stop sending input once terminal outcome is detected.
3. Enforce per-run wall-clock timeout (fail-closed on timeout).
4. Detect terminal outcome from structured signals first, not prose:
   - objective complete, or
   - explicit objective not met.
5. Persist run evidence bundle:
   - session id,
   - terminal snapshot,
   - completion/contract evidence refs,
   - artifact/report/log paths.

## Soundness Validation Against Control Docs

- `docs/runbooks/workflow-state-contract.md`:
  - honors `plan -> execute -> recover -> finalize`; harness waits for true finalize state, not intermediate chatter.
- `docs/runbooks/execution-contract-matrix.md`:
  - enforces terminal decision from contract-level outcome (`objective_met` / `not_met`) before pass/fail scoring.
- `docs/runbooks/failure-taxonomy.md`:
  - classifies timeout/loop churn as bounded failure, not implicit success.
- `docs/runbooks/acceptance-gates.md`:
  - captures required evidence checklist and computes gate metrics from terminal outcomes.

## Acceptance Criteria (Harness Implementation)

1. No queued-input overhang after first terminal outcome.
2. Every run ends in one of:
   - pass (objective achieved with evidence),
   - explicit not-met,
   - timeout/failure class with reason.
3. Gate output includes metrics:
   - `answer_success`,
   - `contract_success`,
   - `avg_steps_to_complete`.
4. Evidence bundle written per run under `sessions/<id>/`.
