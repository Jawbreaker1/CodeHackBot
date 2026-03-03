# Orchestrator Ownership Boundaries

Date: 2026-03-01  
Scope: Sprint 36 Phase 7 cleanup

## Purpose

Define explicit ownership boundaries so planner, executor, verifier, and reporter do not overlap responsibilities.
This is the contract for future refactors and feature work.

## Component Roles

### Planner

- Inputs:
  - run goal, scope, constraints, playbook hints, prior verified findings.
- Outputs:
  - task graph (`task.json` units), dependencies, budgets, risk tiers, completion contracts.
- Owns:
  - *what should be attempted* and task decomposition.
- Does not own:
  - command execution results, finding verification, report claim truth.

### Executor (Worker Runtime + Coordinator Dispatch)

- Inputs:
  - planned task action, scope policy, risk policy, approvals, runtime context.
- Outputs:
  - events, artifacts, candidate findings, task terminal events.
- Owns:
  - *how work is executed safely* (scope enforcement, retries, bounded recovery, tool execution).
- Does not own:
  - promotion of shared memory truth, acceptance of high-confidence claims as verified truth, final report truth decisions.

### Verifier

- Inputs:
  - candidate findings + evidence artifacts/events.
- Outputs:
  - verifier verdict state (`verified` / `rejected` / `unverified`) with provenance.
- Owns:
  - claim truth adjudication against evidence contracts.
- Does not own:
  - planning decomposition, executor command orchestration, report narrative formatting.

### Reporter

- Inputs:
  - scope/metadata, artifacts, verifier-backed findings, run outcomes.
- Outputs:
  - OWASP-style report artifacts.
- Owns:
  - reproducible human-readable narrative and remediation structure.
- Does not own:
  - inventing facts, promoting unverifiable claims, bypassing verifier gates.

## Shared Memory Contract

- Orchestrator is the single writer for shared-memory truth files.
- Workers are append-only proposers through events/artifacts/findings.
- Promoted facts require provenance (`source_event_ids`, verdict state).

## Event Mutation Contract

- Event-to-state ownership is defined only in `internal/orchestrator/event_mutation_ownership.go`.
- Any new event mutation must update:
  - mutation table,
  - guard tests,
  - state inventory doc.

## Terminal Truth Contract

- Run terminal truth uses verification/completion gates plus `run_outcome`.
- Projection states (`RunStatus.State`, `WorkerStatus.State`) remain read models only.
- `completed` must not be inferred from projection state alone.

## Known Open Gaps

- `PR-010`: non-summary tasks can still complete via `assist_no_new_evidence`; this violates desired executor->completion contract and remains a blocking fix item.
