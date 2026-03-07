# Agent Workflow Checkpoint (Sprint 37)

Date: 2026-03-07  
Status: Active checkpoint (implementation pause marker)

## Purpose

Stop incremental patching long enough to evaluate whether the current agent flow is still too layered to let the LLM operate effectively.

This checkpoint is intentionally narrow. It covers only the repeatedly failing chain:

`planner task contract -> runtime action shaping -> artifact truth -> recovery input model`

## Why This Checkpoint Exists

Recent ZIP orchestrator runs showed progress after each fix, but each fix exposed another boundary failure:
- planner emitted under-specified direct commands,
- scope validation misread local file tokens as host targets,
- recovery preferred basename placeholders over concrete artifact paths,
- metadata recovery still rereads the same evidence instead of converting it into the next action.

This pattern suggests the main risk is no longer “missing capability”.  
The risk is fragmented ownership across too many workflow layers.

## Main Takeaway

The LLM is not the primary bottleneck in the observed failures.

The primary bottleneck is **information fidelity loss across workflow boundaries**.

The current system still makes the LLM work through:
- multiple action-shaping layers,
- multiple artifact/path rewrite layers,
- multiple recovery-context projections,
- mixed ownership of “what is executable” and “what counts as evidence”.

That is why tiny details keep surfacing as blockers.

## Updated Checkpoint Decision (2026-03-07)

This checkpoint now has a stricter interpretation:

- no more patch-driven behavior changes unless they clearly remove an owner or mutation stage,
- no more assist-mode capability shaping justified only by “current scenario failure”,
- fallback behavior must be treated as a likely architecture smell whenever it invents new commands.

The reduction target is now documented in:
- `docs/runbooks/closed-loop-reduction-plan.md`

Use this checkpoint doc as the current-state overlap map, and the reduction-plan doc as the target-state contract.

## Current Ownership Map

### 1. Planner task contract

Primary owners:
- `internal/orchestrator/llm_planner.go`
- `cmd/birdhackbot-orchestrator/main_planner.go`
- `cmd/birdhackbot-orchestrator/main_planner_archive.go`
- `cmd/birdhackbot-orchestrator/main_planner_actionshape.go`

Current responsibilities:
- build structured planner prompt,
- parse/normalize planner JSON,
- archive workflow normalization,
- executability normalization,
- retry diagnostics / planner trace capture.

Assessment:
- too many post-planner mutations already happen before execution,
- planner intent and executor-ready action shape are not owned in one place.

### 2. Runtime action shaping

Primary owners:
- `internal/orchestrator/runtime_command_pipeline.go`
- `internal/orchestrator/runtime_input_repair.go`
- `internal/orchestrator/worker_runtime.go`
- `internal/orchestrator/worker_runtime_assist_exec.go`
- `internal/orchestrator/scope.go`
- `internal/scopeguard/policy.go`

Current responsibilities:
- normalize/prepare command shape,
- repair missing inputs,
- adapt archive / vulnerability workflows,
- apply scope validation,
- builtin command execution,
- assist-mode execution mutation.

Assessment:
- action shape is still influenced in too many places,
- assist path and direct task path do not cleanly share one “final executable action” owner,
- scope policy is small at the wrapper level but behaviorally entangled via token inference.

### 3. Artifact truth

Primary owners:
- `internal/orchestrator/worker_runtime.go`
- `internal/orchestrator/worker_runtime_assist_loop_emit.go`
- `internal/orchestrator/worker_runtime_assist_loop_helpers.go`
- `internal/orchestrator/runtime_input_repair.go`
- `internal/orchestrator/report.go`

Current responsibilities:
- expected artifact materialization,
- completion-contract produced-artifact recording,
- dependency artifact candidate discovery,
- artifact-path extraction from logs,
- report-time evidence rendering.

Assessment:
- produced artifact truth and “artifact candidates for future repair” are coupled,
- basename aliasing still competes with concrete produced paths,
- artifact truth is not represented as one canonical evidence map.

### 4. Recovery input model

Primary owners:
- `internal/orchestrator/worker_runtime_assist_loop.go`
- `internal/orchestrator/worker_runtime_assist_context_layered.go`
- `internal/orchestrator/worker_runtime_assist_exec.go`

Current responsibilities:
- assist loop budgeting / turn engine,
- context layering,
- execution feedback packaging,
- repeat detection,
- recovery transitions,
- completion emission.

Assessment:
- recovery reads a projected context, not a simple canonical “previous action + outputs + artifacts + open contract gap” object,
- that makes it too easy to reread evidence instead of concluding from it.

## Overlap / Complexity Hotspots

### Hotspot A: Action-shape ownership is split

Observed owners:
- planner normalization,
- planner executability gate,
- archive planner normalization,
- runtime command pipeline,
- assist execution mutation,
- input repair.

Failure pattern:
- a task can be shaped at planning time, reshaped again at runtime, and reshaped again in assist mode.

Risk:
- no single place guarantees “this is the exact action the model intended to run”.

### Hotspot B: Artifact references have no single truth owner

Observed owners:
- expected artifacts in task spec,
- produced artifacts in completion contract,
- task artifact directories,
- dependency artifact walkers,
- referenced-path extraction from artifact contents,
- runtime path repair.

Failure pattern:
- recovery can choose basename placeholders even when concrete artifact paths already exist.

Risk:
- the LLM receives weaker artifact references than the system already has.

### Hotspot C: Recovery is too projection-heavy

Observed inputs:
- observations,
- recent artifacts,
- dependency artifacts,
- recover hint,
- compaction notes,
- execution feedback,
- task contract,
- memory facts.

Failure pattern:
- the model sometimes re-inspects instead of concluding from evidence already present.

Risk:
- context gets broader, but not necessarily more actionable.

## Simplification Targets

### Target 1: One owner for executable action shape

Desired rule:
- after planning, there should be exactly one normalization pass that produces the executable action contract.
- execution paths may validate, but should not silently reinterpret intent again.

Practical direction:
- merge planner post-processing + runtime pre-exec shaping into a clearer two-stage contract:
  - `plan_intent`
  - `exec_action`

### Target 2: One owner for artifact truth

Desired rule:
- every produced artifact should have one canonical absolute path entry available to future tasks/recovery.
- basename aliases should be secondary hints, never the primary resolution path.

Practical direction:
- maintain a canonical produced-artifact index per run/task and feed that first into repair/recovery.

### Target 3: Recovery should operate on a smaller, more explicit state object

Desired rule:
- recovery input should prioritize:
  - exact previous command,
  - exact stderr/stdout summary,
  - exact produced artifact paths,
  - exact unmet contract gap.

Practical direction:
- reduce reliance on mixed observation churn and log rereads when a contract gap can be stated directly.

Implemented slice (`2026-03-07`):
- recovery prompts now expose explicit state first:
  - previous command,
  - previous exit code/runtime error,
  - primary evidence refs,
  - contract gap summary.
- recover-mode execution-log reread chains are now treated as low-value churn and blocked before adding more scenario-shaped routing.

## Candidate Merges / Deletions

These are evaluation targets, not implementation orders yet.

1. Merge action-shape normalization responsibilities
- current candidates:
  - `cmd/birdhackbot-orchestrator/main_planner_actionshape.go`
  - `cmd/birdhackbot-orchestrator/main_planner_archive.go`
  - `internal/orchestrator/runtime_command_pipeline.go`

2. Shrink `runtime_input_repair.go`
- current file: `internal/orchestrator/runtime_input_repair.go` (~1243 LOC)
- likely split into:
  - artifact-candidate discovery
  - repair-family classification
  - substitution policy

3. Shrink `worker_runtime_assist_loop.go`
- current file: `internal/orchestrator/worker_runtime_assist_loop.go` (~1265 LOC)
- likely split by:
  - turn engine
  - recovery transition policy
  - completion/failure policy

4. Remove basename-first fallback where canonical produced artifact path exists
- this is likely the highest-value deletion candidate in the short term.

## Immediate Conclusions

1. We should pause broad tactical fixes and simplify this chain first.
2. The system still appears over-layered around action shaping and artifact truth.
3. The current failures do **not** justify more tool-specific hardcoding.
4. The next fixes should reduce ownership overlap, not add new adapters.
5. The next architecture slice should remove decision ownership from fallback and assist-mode runtime shaping before any further ZIP/router-specific hardening.

## Sprint 37 Gate

Before further ZIP/router tactical hardening, Sprint 37 should require:
- this checkpoint doc,
- updated task/discovery references,
- the next behavior change to explicitly reduce overlap in one of:
  - action-shape ownership,
  - artifact truth ownership,
  - recovery input projection complexity.
