# Runtime Mutation Stage Map

Date: 2026-03-06  
Status: Active (Critical 3 reference)

## Purpose

Track exactly where command intent can be changed before execution.
This is the control doc for Sprint 37 Critical 3 (mutation-stack simplification).

Important: this is a current-state map, not a target-state endorsement.
Sprint 37 architecture reduction aims to remove assist-mode mutation stages rather than refine them indefinitely.

## Current Stage Map (Orchestrator Command Actions)

Primary execution path anchors:
- `internal/orchestrator/worker_runtime.go`
- `internal/orchestrator/runtime_command_pipeline.go`
- `internal/orchestrator/runtime_command_prepare.go`
- `internal/orchestrator/runtime_input_repair.go`
- `internal/orchestrator/runtime_archive_workflow.go`
- `internal/orchestrator/runtime_vulnerability_evidence.go`
- `internal/orchestrator/worker_runtime_command_repair.go`

Ordered pre-exec mutation stages (command actions):
1. action normalization (`normalizeTaskAction`)
2. optional target attribution resolution (`resolveTaskTargetAttribution`)
3. unified runtime mutation pipeline (`applyRuntimeCommandPipeline`):
   - `target_attribution` (`enforceAttributedCommandTarget`)
   - `prepare_runtime_command` (`prepareRuntimeCommand`)
   - `adapt_weak_report_action`
   - `enforce_vulnerability_evidence`
   - `adapt_weak_vulnerability_action`
   - `adapt_runtime_command` (metasploit/runtime command normalization)
   - `repair_missing_inputs` (missing local/dependency input path repair)
   - `adapt_archive_workflow` (archive capability adaptation)
4. scope validation (`ValidateCommandTargets`) on fully mutated command
5. execute

Post-failure mutation/retry stages (separate by design):
1. command contract repair (`attemptCommandContractRepair`)
2. retry-specific adaptation (for example nmap host-timeout retry)

## Assist Command Path Alignment

- `worker_runtime_assist_loop.go` now runs assistant `type=command` suggestions through the same `applyRuntimeCommandPipeline(...)` before scope validation.
- Execution then uses prevalidated args with mutation disabled in `executeWorkerAssistCommandWithOptions(...skipRuntimeMutation=true)`.
- This removes hidden post-validation rewrites in the assist command path.

## Current Risks

- Assist execution path still has partial independent mutation logic and is not fully converged with command-action pipeline.
- Post-failure repair can still re-shape command args; keep this bounded and telemetry-visible.
- Capability-level adapters remain necessary, but must stay free of scenario literals.
- Input-repair safety now uses generic repair families (`input_source`, `validation_input`, `report_output`, `auxiliary_log`, `unknown`) per `docs/runbooks/input-repair-family-design.md`; keep unknown-family behavior fail-closed.
- The assist/orchestrator reduction target in `docs/runbooks/closed-loop-reduction-plan.md` explicitly calls for reducing assist-mode capability shaping and fallback command invention.
- `docs/runbooks/runtime-shaping-reduction.md` is the current deletion-oriented plan for shrinking these assist-mode mutation stages.

## Target Contract (Critical 3)

Move toward one explicit ordered mutation pipeline:
1. normalize
2. safety/scope-required adaptation
3. capability support adaptation (bounded and auditable)
4. validation
5. execute
6. post-failure repair (single bounded path)

Rules:
- every mutation stage must be explicit in progress telemetry,
- no silent overlap of equivalent rewrite responsibility,
- no scenario-literal rewrite behavior in production paths.

Sprint 37 reduction addendum:
- assist-mode should move toward near-direct execution with only safety/scope validation,
- this file should get shorter over time, not longer.

## Change Control (Maintenance Rule)

Any PR that adds/removes/reorders runtime command adaptation must update:
- this file,
- `docs/runbooks/llm-freedom-guardrail-audit.md`,
- associated regression tests.
