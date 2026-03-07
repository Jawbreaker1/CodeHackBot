# Execution Contract Matrix

Date: 2026-03-06  
Status: Active (Sprint 37 control doc)

## Purpose

Define one shared contract for `plan -> execute -> recover -> finalize` so CLI and orchestrator behavior stay aligned.

## Contract Matrix

| Phase | CLI contract | Orchestrator contract | Fail-closed rule |
|---|---|---|---|
| `plan` | Goal captured; plan may require operator approval before action. | `plan.json` + task graph validated before lease; direct command tasks must be self-executable or be promoted back to assist expansion. | No execution starts without a valid plan/task contract. |
| `execute` | Runs one bounded action, records command/output/evidence links. | Worker runs leased task, emits progress/artifact/finding events. | Usage/help/no-op output cannot count as success. |
| `recover` | LLM must choose: `retry_modified`, `pivot_strategy`, `ask_user`, `step_complete`. | Runtime repair/replan is bounded and explicit in events. | Repeating near-identical failed action is rejected. |
| `finalize` | Must emit terminal completion contract: `objective_met`, `why_met`, `evidence_refs` (or explicit `not_met`). | Must emit terminal `task_completed`/`task_failed` with evidence-backed contract fields. | No terminal success without objective-proof + evidence refs. |

## Required Completion Fields (Shared)

- `objective_met`: boolean
- `why_met`: concise machine-checkable reason
- `evidence_refs`: artifact/log paths used as proof
- `semantic_verifier`: verifier result used for contradiction checks

## Change Control

Any PR that changes completion payloads, phase behavior, or terminal rules must update:
- `docs/runbooks/workflow-state-contract.md`
- `docs/runbooks/runtime-mutation-stage-map.md`
- this file
- related regression tests
