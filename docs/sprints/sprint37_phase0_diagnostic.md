# Sprint 37 Phase 0 Diagnostic (Investigation Only)

Date: 2026-03-06  
Scope: CLI + orchestrator stability-critical paths only.  
Constraint: no implementation in this phase; diagnostics and sequencing only.

## Inputs Reviewed

- CLI control loop and plan/recovery/finalize flow:
  - `internal/cli/cli.go` (`handleAssistAgentic`, `handlePlanSuggestion`, `runBoundedPostPlanRecovery`)
  - `internal/cli/assist_finalize.go`
  - `internal/cli/runner.go` (runtime state fields)
  - `internal/cli/assist_goal_approval.go`, `internal/cli/assist_plan_confirm.go`, `internal/cli/assist_execution_confirm.go`
- Orchestrator lifecycle and terminalization:
  - `cmd/birdhackbot-orchestrator/main_runtime.go`
  - `internal/orchestrator/coordinator.go`
  - `internal/orchestrator/worker_runtime.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
- State/projection model:
  - `internal/orchestrator/state_machine.go`
  - `internal/orchestrator/run_phase.go`
  - `internal/orchestrator/run_outcome.go`
  - `internal/orchestrator/event_projection.go`
  - `internal/orchestrator/event_mutation_ownership.go`
  - `internal/orchestrator/event_cache.go`
- Static instruction/adapter paths:
  - `internal/assist/assistant_prompt.go`
  - `internal/assist/assistant_fallback.go`
  - `internal/orchestrator/runtime_command_prepare.go`
  - `internal/orchestrator/command_adaptation.go`
  - `internal/orchestrator/runtime_input_repair.go`
  - `internal/orchestrator/runtime_archive_workflow.go`
  - `internal/orchestrator/runtime_vulnerability_evidence.go`
  - `internal/orchestrator/worker_runtime_command_repair.go`
  - `internal/cli/cmd_exec.go`

## 1) Control-Flow Review (Plan -> Execute -> Recover -> Finalize)

### CLI

Observed flow:
1. `handleAssistAgentic` owns the high-level action loop and budget.
2. Plan path enters `handlePlanSuggestion`.
3. Step failures route through `handleAssistCommandFailure` and may continue plan execution.
4. Post-plan bounded recovery (`runBoundedPostPlanRecovery`) attempts closure.
5. Terminalization for action goals uses `ensureActionGoalTerminalState`.

Primary risks:
- State is implicit and spread across mutable fields (`pendingAssistGoal`, `pendingAssistQ`, `assistExecApproved`, `assistObjectiveMet`, `assistRuntime.CurrentMode`) instead of one typed transition graph.
- Multiple completion paths (direct complete, artifact conclusion, finalize fallback) make terminal behavior harder to reason about.
- Operator interrupt intentionally pauses/resumes by setting pending goal state, which is safe but easy to misread as "did not stop."

### Orchestrator

Observed flow:
1. `executeCoordinatorLoop` drives `Coordinator.Tick`.
2. `Coordinator.Tick` runs guard/reclaim/approval/replan/dispatch sequence.
3. Worker execution goes through `RunWorkerTask` and `runWorkerAssistTask`.
4. Terminal outcome set in `evaluateRunTerminalOutcome` and report emitted by `emitRunReport`.

Primary risks:
- Run-level truth uses several domains (`RunStatus.State`, `RunPhase`, `RunOutcome`) and can appear inconsistent if projections lag.
- Worker command mutation is layered at multiple points before/after execution, which increases intent drift risk.

## 2) State/Status Audit and Simplified Canonical Map

### Current model (audited)

Authoritative lifecycle domains:
- Task lifecycle: `TaskState` (`queued` -> ... -> terminal) in `internal/orchestrator/state_machine.go`.
- Run phase: control-plane workflow in `internal/orchestrator/run_phase.go`.
- Run outcome: terminal truth in `internal/orchestrator/run_outcome.go`.
- Finding lifecycle: `hypothesis/candidate/verified/rejected`.

Derived projection domains:
- Run projection: `unknown/running/stopped/completed` in `internal/orchestrator/event_projection.go`.
- Task projection: `queued/running/done` (collapsed terminal types).
- Worker projection: `seen/active/stopped`.

Ambiguities:
- `taskProjectionStateDone` collapses completed/failed/canceled for run-status summaries.
- Run can simultaneously have a projection state and a run outcome; operators can see one before the other updates.
- CLI has no explicit typed state machine for assist loop runtime.

### Canonical simplified map (target for implementation phase)

1. Keep `TaskState` as the only authoritative task lifecycle.
2. Keep `RunOutcome` as the only terminal truth for success/fail/aborted/partial.
3. Keep `RunPhase` as control-plane progress only (never used as truth verdict).
4. Keep run/worker/task projections as read-only UI cache, never gate decisions.
5. Introduce one explicit CLI assist-loop state enum (single owner) to replace implicit boolean/string combinations.

## 3) Static Instruction/Rewrite Audit (Classification)

Classification rules:
- `required safety`: needed for scope/safety/contract integrity.
- `temporary support`: helpful now but should be reduced or sunset.
- `remove`: should not stay in production runtime.

| Path | Current role | Classification | Notes |
| --- | --- | --- | --- |
| `internal/assist/assistant_prompt.go` | global schema/safety/system contract | `required safety` | Keep, but split into composable sections to reduce prompt entropy. |
| `internal/assist/assistant_fallback.go` | deterministic fallback behaviors | `temporary support` | Useful for degraded/no-LLM mode; should be minimal and capability-level only. |
| `internal/orchestrator/runtime_command_prepare.go` | ordered prep entry point | `required safety` | Keep as single mutation gateway. |
| `internal/orchestrator/command_adaptation.go` | nmap runtime guardrails | `temporary support` | High utility, but currently dense and tool-biased. |
| `internal/orchestrator/runtime_input_repair.go` | missing-path/wordlist repairs | `temporary support` | Large heuristic surface; biggest typed-repair risk. |
| `internal/orchestrator/runtime_archive_workflow.go` | archive workflow aids | `temporary support` | Capability exception path; requires strict sunset policy. |
| `internal/orchestrator/runtime_vulnerability_evidence.go` | vulnerability evidence rewrites | `temporary support` | High impact on agent autonomy; keep bounded and auditable. |
| `internal/orchestrator/worker_runtime_command_repair.go` | command-contract repair | `required safety` | Needed for malformed command recovery, should stay generic. |
| `internal/cli/cmd_exec.go` (`maybeAugmentJohnShowResult`) | tool-specific completion augmentation | `remove` | Cross-cutting tool-specific behavior in generic executor path. |

## 4) Root-Cause Register (Top 5)

### RC-1: Overlapping mutation stack causes intent drift
- Anchors: `runtime_command_prepare.go`, `command_adaptation.go`, `runtime_input_repair.go`, `runtime_vulnerability_evidence.go`, `worker_runtime_command_repair.go`.
- Impact: model-selected action can be changed by multiple layers, reducing predictable agent behavior.
- Task mapping: Sprint 37 `Critical 3`, `Critical 4`.

### RC-2: Terminal truth split across multiple run/task representations
- Anchors: `main_runtime.go`, `event_projection.go`, `event_cache.go`, `run_outcome.go`.
- Impact: inconsistent operator-visible end state and hard-to-verify completion semantics.
- Task mapping: Sprint 37 `Critical 1`, `Critical 2`, `High — State and reporting consistency`.

### RC-3: CLI assist loop has implicit state machine
- Anchors: `runner.go`, `cli.go`, `assist_finalize.go`.
- Impact: brittle pause/resume/finalize behavior and edge-case regressions under long recoveries.
- Task mapping: Sprint 37 `High — State and reporting consistency`, carryover completion-gate tasks.

### RC-4: Static fallback/adaptation pressure reduces LLM autonomy
- Anchors: `assistant_fallback.go`, `runtime_archive_workflow.go`, `runtime_vulnerability_evidence.go`.
- Impact: deterministic behavior appears where adaptive model-driven correction is expected.
- Task mapping: Sprint 37 `Medium — Static-instruction minimization policy`, `Critical 3`.

### RC-5: Context relevance competes with rewrite-heavy recovery
- Anchors: `worker_runtime_assist_loop.go`, `internal/cli/assist_context*.go`, `runtime_input_repair.go`.
- Impact: stale/low-value artifacts can dominate near decision points and lower recovery quality.
- Task mapping: Sprint 37 `High — Context quality under long recovery`.

## 5) Patch Order (Sequencing Only, No Implementation Here)

1. Terminal truth integrity first (`Critical 1` + `Critical 2`).
- Fail-closed terminalization and unified completion contract semantics.

2. Mutation-stack reduction second (`Critical 3`).
- Enforce one ordered pre-exec pipeline and remove overlapping transforms.

3. Typed repair safety third (`Critical 4`).
- Add strict artifact-type contracts before any substitution.

4. State/report projection coherence fourth (`High`).
- Keep projections informative but never authoritative.

5. Context relevance and static-instruction minimization fifth (`High` + `Medium`).
- Improve evidence-anchor prioritization and reduce temporary supports.

## 6) Phase 0 Exit Record

- This document satisfies Sprint 37 Phase 0 deliverables as investigation output.
- No runtime behavior change is introduced by this phase.
- Next execution phase should start at Sprint 37 Critical 1 in the order above.
