# Closed-Loop Reduction Plan

Date: 2026-03-07  
Status: Active (Sprint 37 architecture gate)  
Scope: CLI + orchestrator assist-mode execution

## Purpose

Define the reduced agent workflow we are trying to reach before more tactical fixes.

This document is intentionally deletion-oriented. It does not add new scenario behavior.
It identifies which responsibilities should remain, which should move, and which should be removed.

## Problem Statement

Live runs still show the same structural failure:

- the LLM can often choose a sensible local action,
- intermediate system layers mutate, reinterpret, or re-suggest actions,
- recovery then operates on projected/stale context instead of one exact prior result,
- the system compensates with more tactical fixes.

That is the wrong direction.

## Target Model

The target is an Open Interpreter-style closed loop with strict safety and evidence contracts.

### Closed Loop

1. LLM receives:
   - goal / task contract,
   - exact previous command,
   - exact args,
   - exit code,
   - output summary / tail,
   - canonical produced artifacts,
   - unmet contract gap.
2. LLM chooses one of:
   - `retry_modified`
   - `pivot_strategy`
   - `step_complete`
   - `ask_user`
3. Runtime does only:
   - contract validation,
   - scope / safety validation,
   - minimal command wrapping,
   - execution,
   - artifact capture,
   - feedback packaging.
4. Generic progress gate decides whether the latest action materially changed the search.

Everything else should be treated as suspect.

## Ownership Model

### Planner

Owns:
- task decomposition,
- task intent,
- dependencies,
- completion expectations.

Does not own:
- detailed shell command shaping for assist-mode steps.

### Assist Loop

Owns:
- executable action choice,
- step-to-step adaptation,
- interpretation of execution feedback,
- deciding whether a step is complete or needs a pivot.

Does not own:
- scope bypass,
- path mutation,
- synthetic evidence truth,
- implicit report/finding promotion.

### Runtime

Owns:
- minimal executable normalization,
- scope/safety validation,
- tool invocation,
- exact result capture.

Does not own:
- inventing replacement commands,
- capability-specific workflow shaping,
- “helpful” archive/vuln/report action rewrites for assist-mode work.

### Artifact Truth

Owns:
- canonical produced artifact index,
- exact artifact paths attached to each execution result.

Does not own:
- semantic next-step suggestions.

### Progress Gate

Owns:
- generic material-progress decision.

Does not own:
- tool-specific anti-loop rules.

## Required Simplifications

### 0. Fallback must stop being a second planner

This is now broken out in:
- `docs/runbooks/fallback-ownership-reduction.md`

It is the first reduction slice because it affects both CLI and orchestrator and
directly determines whether the LLM still owns executable action choice.

Status:
- first slice implemented on 2026-03-07 by removing executable command/tool synthesis from `FallbackAssistant`.
- live evidence (`run-s37-fallbackless-zip-20260307-143338`, `run-s37-fallbackless-router-20260307-143338`) confirms this reduction should remain; the remaining issue shifted to non-interactive degraded recovery semantics, not fallback command invention.

### 1. Assist-mode should be near-direct execution

For assist-mode tasks, the runtime should stop doing capability-specific command shaping.

Detailed reduction note:
- `docs/runbooks/runtime-shaping-reduction.md`

Status:
- archive input injection for assist-mode tasks has been removed.
- generic assist-mode command shaping (`prepare_runtime_command`, weak vulnerability/report rewrites) has also been removed.
- recover-mode fallback question churn now fails fast with latest-result metadata, and latest execution feedback now captures shell wrapper output/input refs.
- adaptive replans now reuse explicit source-task dependencies for evidence access, with scheduler support for failed-source evidence dependencies.
- live router/ZIP smokes (`run-s37-arttruth-router-20260307-144314`, `run-s37-arttruth-zipall-20260307-145040`) confirm that source evidence now flows through normal dependency artifacts instead of prompt-only hints.
- remaining overlap is now concentrated in any residual basename-first repair paths during replans, plus any remaining non-assist runtime mutation.

Keep:
- scope enforcement,
- policy enforcement,
- minimal shell wrapping (`bash -lc` vs argv),
- required path canonicalization only when it is exact and lossless.

Remove or demote:
- archive workflow command shaping,
- weak vulnerability action adaptation,
- weak report action adaptation,
- fallback command invention from hint/path scraps.

### 2. One canonical execution result object

Every assist turn should emit one shared result object:

- `command`
- `args`
- `exit_code`
- `runtime_error`
- `result_summary`
- `output_tail`
- `produced_artifact_refs`
- `input_refs`
- `contract_gap`

Normal recovery, parse-repair, fallback, and adaptive replan must all consume this same object.

### 3. One generic progress rule

Replace layered anti-loop heuristics with one generic rule:

The next action must materially differ by at least one of:
- command,
- target/input artifact,
- validation objective,
- evidence source.

If not, the loop must pivot or fail that step.

This rule is generic and does not encode tool/scenario knowledge.

### 4. Findings vs recovery ownership must stay separate

Telemetry findings such as `task_execution_failure` should remain reportable,
but they must not create a second adaptive recovery path.

Recovery should come from the failure event that already has the latest execution result.

## Deletion Targets

These are the highest-value deletion/reduction areas.

1. Assist-mode runtime shaping in:
   - `internal/orchestrator/runtime_archive_workflow.go`
   - `internal/orchestrator/runtime_vulnerability_evidence.go`
   - `internal/orchestrator/runtime_command_pipeline.go`

2. Fallback command synthesis in:
   - `internal/assist/assistant_fallback.go`

3. Duplicate recovery ownership in:
   - finding-driven replan paths for execution failures,
   - parse-failure fallback paths that invent commands instead of returning to the LLM loop.

4. Path/evidence inference layers that compete with canonical produced artifact truth.

## Current Docs Relevance Audit

### Still authoritative

- `docs/runbooks/workflow-state-contract.md`
  - still correct for lifecycle ownership and terminal invariants.
- `docs/runbooks/state-inventory.md`
  - still correct as the enum/status authority.
- `docs/runbooks/ownership-boundaries.md`
  - still relevant, but needs assist-loop reduction wording aligned with this doc.
- `docs/runbooks/agent-workflow-checkpoint-s37.md`
  - still relevant as the current-state overlap audit.

### Still useful, but only as current-state maps

- `docs/runbooks/runtime-mutation-stage-map.md`
  - useful to show where intent is still being changed today.
  - should not be read as endorsement of keeping all those stages.
- `docs/runbooks/planner-action-executability.md`
  - still relevant as a guard against bare-tool planner output.
  - should remain subordinate to the larger goal of reducing action-shape ownership.
- `docs/runbooks/input-repair-family-design.md`
  - still relevant for fail-closed substitution safety.
  - should remain narrow and not grow into behavior shaping.

### No longer sufficient as guidance by themselves

- any doc that justifies adding more tactical mutation/rewrite layers without removing an owner first.

## Implementation Gate

Before more ZIP/router tactical fixes:

1. Next implementation must remove a decision owner or mutation stage.
2. No new tool-specific assist-mode command shaping.
3. No new fallback command synthesis.
4. Every proposed change must name:
   - the owner being removed,
   - the contract being simplified,
   - the code path expected to shrink or disappear.

## Recommended Order

1. Reduce fallback to non-planner behavior.
2. Carve assist-mode out of most runtime command shaping.
3. Standardize one canonical execution result object across recovery/replan/finalize.
4. Replace layered anti-loop patches with one material-progress gate.
5. Then rerun live ZIP/router/cross-scenario smokes before any further tactical adjustment.
