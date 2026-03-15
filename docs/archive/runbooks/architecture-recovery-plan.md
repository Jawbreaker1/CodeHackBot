# Architecture Recovery Plan (Sprint 36)

Date: 2026-02-25
Status: Draft (no implementation changes required by this document)

## Why this exists

Recent runs show repeated divergence between:
- task-level completion signals, and
- goal-level truth (did we actually prove the requested security outcome).

This document defines the recovery path before adding more hardening patches.

## Observed systemic issues

1. Run success is task-state driven, not goal-truth driven.
- `cmd/birdhackbot-orchestrator/main_runtime.go:131`
- `evaluateRunTerminalOutcome` returns success when all tasks are `LeaseStatusCompleted`, regardless of whether success criteria were semantically proven.

2. Artifact completion can be syntactic instead of semantic.
- `internal/orchestrator/worker_runtime.go:752`
- `writeExpectedCommandArtifacts` may materialize expected artifacts from generic command output when concrete evidence files are absent.

3. Runtime adaptation layer has grown into policy + repair + domain behavior.
- `internal/orchestrator/worker_runtime.go` (~3.2k LOC, >120 funcs)
- Mixed responsibilities: command shaping, input repair, tool-specific adaptation, evidence validation, reporting side effects.

4. Completion-contract artifact matching has mixed strict and permissive behavior.
- `internal/orchestrator/coordinator_completion_contract.go:126`
- New matching is improved, but generic fallback paths still allow non-semantic satisfaction in some scenarios.

5. Report synthesis can over-claim findings.
- Example run artifact:
  - `sessions/run-zip-regression-20260225-223809/orchestrator/artifact/T-008/report.md`
- Report text can claim recovered password despite missing recovered-password evidence artifacts.

## Architectural intent (target state)

1. Planner is responsible for *strategy generation*.
2. Executor is responsible for *deterministic execution*.
3. Verifier is responsible for *truth claims*.
4. Reporter is responsible for *presenting verified truth + traceable evidence*.

The system should fail honestly when it cannot verify claims.

## Shared Memory Ownership Model (Parallel Agents)

To keep complexity bounded, shared memory is **orchestrator-owned**:
- Single writer: orchestrator only.
- Workers are append-only proposers (events, artifacts, candidate findings).
- Verifier gate decides promotion into shared truth.
- Workers read scoped snapshots provided by orchestrator; they do not mutate shared memory directly.

Why this is required:
- avoids concurrent write races and hidden state conflicts,
- keeps trust boundaries explicit,
- makes debugging/replay deterministic from event + promotion history.

Non-negotiable constraints:
1. No worker-direct writes to run shared memory files.
2. No report/run success claim from unpromoted candidate state.
3. Shared memory updates must be attributable to event IDs and verifier outcome.

## Implementation Checkpoint Protocol

Recovery implementation must proceed in small increments with evaluation after each increment:
1. Implement one bounded change slice.
2. Run targeted unit tests for touched contracts.
3. Run quick-gate smoke (LLM active) and compare against prior checkpoint.
4. Record results and regressions in `docs/runbooks/problem-register.md`.
5. Only continue if safety/truth-gate behavior is non-regressing.

## Hard Support Exception Policy

Default approach:
- Use generic, capability-level scaffolding (intent classification, evidence deltas, verification gates).
- Avoid command-specific or target-specific patches as primary recovery strategy.

Exceptions are allowed only when all conditions are met:
1. The workflow is common and operationally important.
2. Generic scaffolding repeatedly underperforms with clear evidence.
3. The support can be expressed as capability-level behavior (not product/device hardcoding).
4. The support is behind an explicit feature flag or policy gate.
5. The support has regression tests and measurable success criteria.
6. The support can be removed without breaking core runtime contracts.

Guardrails for exceptions:
- Never bypass verifier truth gates.
- Never bypass scope/safety policy.
- Never mark run/report success based only on exception-path completion.

Review rule:
- Every hard-support addition must include:
  - problem-register reference,
  - why generic scaffolding was insufficient,
  - rollback/removal conditions.

## Contract changes required

### A. Planner -> Executor contract

- Action contract must be strict:
  - executable + args must be valid shape
  - required inputs explicitly declared
  - expected artifacts typed (e.g., `hash_material`, `recovered_secret`, `report`), not only filenames
- Executor should reject malformed/off-contract actions instead of stacking auto-fixes.

### B. Executor -> Verifier contract

- Task completion requires:
  - execution success, and
  - verifier predicate pass for claimed outcome type.
- "Produced file exists" alone is not enough for semantic completion.

### C. Verifier -> Run terminal contract

- Run `completed` requires:
  - all required tasks terminalized, and
  - success criteria predicates satisfied by verified evidence graph.

### D. Verifier -> Reporter contract

- Claims in report findings/outcome sections must map to verifier-approved evidence.
- Unverified results must be labeled `UNVERIFIED` or excluded from findings summary.

## Cleanup sweep scope (no behavior change first)

### 1) File/module decomposition

Split `internal/orchestrator/worker_runtime.go` into:
- `worker_runtime_execute.go`
- `worker_runtime_input_repair.go`
- `worker_runtime_archive_adapters.go`
- `worker_runtime_vuln_adapters.go`
- `worker_runtime_evidence.go`

Split `internal/orchestrator/worker_runtime_assist_loop.go` into:
- `assist_turn_engine.go`
- `assist_recovery_policy.go`
- `assist_termination_policy.go`

Split `cmd/birdhackbot-orchestrator/main_planner.go` into:
- `planner_entry.go`
- `planner_normalization.go`
- `planner_diagnostics.go`

### 2) Remove/reduce redundant logic

- Consolidate overlapping adaptation paths:
  - input repair
  - command adaptation
  - post-output artifact synthesis
- Keep one clear owner per concern.

### 3) Tighten semantics

- Replace filename-only completion checks with typed evidence predicates.
- Remove/report generic fallbacks that can fabricate semantic success.

## Implementation order

1. Freeze new tactical hardening patches unless they fix a critical safety defect.
2. Land no-behavior-change refactor splits + tests.
3. Implement run-terminal semantic gate.
4. Implement report truth gate.
5. Re-run targeted regressions (zip + benchmark smoke) and compare to baseline.

## Exit criteria (for this recovery stream)

- No run marked `completed` when success criteria are not verified.
- No report claim of recovered credential/CVE exploitation without verifier-linked evidence.
- Reduced adapter churn in runtime path (ownership boundaries enforced).
- Refactored large files into focused units with unchanged behavior under existing tests.
