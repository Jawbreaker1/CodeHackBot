# Input Repair Family Design

Date: 2026-03-06  
Status: Implemented (Sprint 37)

## Purpose

Replace brittle finite artifact-kind matching with a generic, intent-first repair family model for runtime missing-input substitution.

This design is scoped to orchestrator runtime input repair (`repair_missing_inputs` stage) and is explicitly **plan-first**.

## Problem

Current typed repair safety (`archive|hash|wordlist|log|report`) blocks obvious cross-type aliasing, but it does not scale as artifact diversity grows.

Risk:
- pressure to keep adding new hard-coded kind labels,
- higher chance of ambiguous substitutions,
- maintenance drift away from capability-level guardrails.

## Design Goals

- Keep repairs generic and capability-level (no scenario literals, no tool-sequence hardcoding).
- Derive expected substitution family from task/command intent first.
- Fail closed when family confidence is low.
- Keep LLM freedom: guardrails constrain unsafe substitution, not strategy choice.

## Safety Families

Runtime repair should use these families:
- `input_source`: primary target input for execution (archives, binaries, configs, capture files, etc.).
- `validation_input`: proof/check inputs consumed by validation tools (hash dumps, fingerprints, verifier snapshots, etc.).
- `report_output`: human-facing report/result destinations.
- `auxiliary_log`: supporting logs/transcripts/intermediate traces.
- `unknown`: cannot confidently classify.

These are intentionally broad and reusable across tasks.

## Family Derivation Order

1. Task contract + action intent (primary)
- infer expected family from task goal/strategy, expected artifacts, and command role in the workflow.
- examples:
  - extraction/cracking action -> expected missing positional input is usually `input_source`.
  - verification/show/check action -> expected missing input is usually `validation_input`.
  - report synthesis action -> output path is `report_output`.

2. Command/flag semantics (secondary)
- derive family from argument position/flags (`--output`, `--log`, `--wordlist`, etc.).
- this is capability-level inference, not tool-specific completion logic.

3. Filename/path heuristics (fallback only)
- basename/extensions/tokens may refine classification when (1) and (2) are inconclusive.
- heuristics must not override strong task/intent classification.

## Substitution Contract

- `input_source` may substitute only with candidate artifacts classified as `input_source`.
- `validation_input` may substitute only with `validation_input`.
- `report_output` may substitute only with `report_output`.
- `auxiliary_log` may substitute only with `auxiliary_log`.
- `unknown` is fail-closed:
  - allow exact-basename replacement only,
  - block semantic/token-similarity aliasing and wildcard repairs.

Candidate selection remains bounded and deterministic.

## Soundness Mapping (Existing Control Docs)

This design is valid against active control documents:
- `docs/runbooks/workflow-state-contract.md`:
  - no new state domains introduced; only pre-exec repair behavior refinement.
- `docs/runbooks/execution-contract-matrix.md`:
  - `execute` remains bounded and evidence-first; ambiguous repair now fail-closed.
- `docs/runbooks/failure-taxonomy.md`:
  - ambiguous/unsupported repair stays `contract_failure` or `strategy_failure`, not silent success.
- `docs/runbooks/runtime-mutation-stage-map.md`:
  - remains inside `repair_missing_inputs`; no new hidden mutation stages.
- `docs/runbooks/llm-freedom-guardrail-audit.md`:
  - policy remains generic_capability; avoids scenario-literal growth.

## Test Plan (Implementation Gate)

Required before marking implementation complete:
- unknown-family exact-basename substitution accepted.
- unknown-family semantic/token alias substitution rejected.
- unknown-family wildcard/relative alias substitution rejected.
- family mismatch rejection (`validation_input` cannot bind to `auxiliary_log`, etc.).
- intent-first precedence tests (task/flag inference beats filename heuristic when conflicting).

## Rollout Notes

- Implemented as a single bounded slice under Sprint 37 `High — Input-repair type system generalization`.
- Validation run:
  - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
- Follow-up (separate gate):
  - live smoke for ZIP + router scenarios with decision/evidence artifacts preserved.
