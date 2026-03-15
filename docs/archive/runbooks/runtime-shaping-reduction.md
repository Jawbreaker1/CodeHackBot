# Runtime Shaping Reduction

Date: 2026-03-07  
Status: Active (Sprint 37 architecture-reduction control doc)

## Purpose

Define which assist-mode runtime command mutations should remain, which should be
demoted, and which should be removed to keep executable action choice with the
LLM closed loop.

This is a reduction plan, not a new behavior spec.

## Why This Exists

Live evidence after fallback reduction showed the next overlap clearly:

- fallback no longer invents commands,
- but assist-mode runtime still mutates commands before execution,
- so the system still has more than one owner for executable action shape.

That contradicts the target model in `docs/runbooks/closed-loop-reduction-plan.md`.

## Current Mutation Owners

Primary current-state map:
- `docs/runbooks/runtime-mutation-stage-map.md`

Main assist-mode/runtime shaping owners:
- `internal/orchestrator/runtime_command_pipeline.go`
- `internal/orchestrator/command_adaptation.go`
- `internal/orchestrator/runtime_archive_workflow.go`
- `internal/orchestrator/runtime_vulnerability_evidence.go`
- `internal/orchestrator/runtime_input_repair.go`
- `internal/msf/runtime_command.go`

## Current-State Findings

### 1. `command_adaptation.go`

Still owns broad `nmap` rewrites:
- downgrade `-sS` for privilege/runtime cases
- broad-target discovery rewrites
- script sanitation
- host-timeout / retry / max-rate guardrails

Assessment:
- some of this is legitimate runtime bounding,
- some is still behavioral reshaping.

Keep candidate:
- bounded runtime/safety flags that do not change task intent.

Remove/demote candidate:
- intent-changing rewrites that replace the user's / LLM's scan shape.

### 2. `runtime_archive_workflow.go`

Still owns archive-specific command/input adaptation:
- inject ZIP path into `zip2john` / `zipinfo`
- discover archive/hash inputs from workspace and dependency artifacts
- tool-specific environment shaping for `john`

Assessment:
- this is the clearest remaining assist-mode action owner overlap.
- it is capability-aware and partially decides how archive tasks should run.

Keep candidate:
- isolated runtime env for `john` if required for safe local execution.

Remove/demote candidate:
- command/input injection for assist-mode tasks.
- archive input discovery should become evidence exposure, not command mutation.

### 3. `runtime_vulnerability_evidence.go`

Still owns vulnerability-action enforcement and weak-command rewrites:
- inject `--script "vuln and safe"` / `--script-timeout`
- rewrite weak vuln actions to concrete `nmap` actions

Assessment:
- this is still a second planner for assist-mode vuln work.

Keep candidate:
- evidence validation after execution.

Remove/demote candidate:
- pre-exec command rewrites that choose scan content for the LLM.

### 4. `runtime_input_repair.go`

Owns missing-input repair and typed path substitution.

Assessment:
- this remains necessary, but must stay narrow.
- exact/lossless canonicalization is acceptable.
- semantic or capability-driven substitution is not.

Keep candidate:
- exact basename/canonical path resolution
- fail-closed typed repair

Remove/demote candidate:
- behavior-shaping substitutions that infer intended workflow beyond exact input repair.

## Reduced Contract

For assist-mode command execution, runtime may only:

1. normalize shell wrapping (`bash -lc` vs argv),
2. enforce scope/safety policy,
3. perform exact/lossless path canonicalization,
4. attach safe runtime env needed by the chosen tool,
5. capture exact result and artifacts.

Runtime must not:

1. inject missing capability-specific task inputs,
2. rewrite weak commands into concrete scan/crack/report commands,
3. expand scan shape beyond safety bounding,
4. choose evidence-producing tools for the model.

## First Deletion Targets

### Slice 1

Reduce assist-mode archive command shaping:
- stop `adaptArchiveWorkflowCommand(...)` from injecting ZIP args for assist-mode tasks.
- keep only safe runtime env setup (`john` HOME/JOHN isolation) if needed.

Expected gain:
- archive workflows either execute exactly as the LLM chose, or fail and feed
  the exact error back into the loop.

Status:
- implemented on 2026-03-07.
- validation:
  - `internal/orchestrator/runtime_command_pipeline_test.go:TestApplyRuntimeCommandPipelineDoesNotInjectArchiveInputForAssistTask`
  - `internal/orchestrator/worker_runtime_assist_builtin_test.go:TestApplyAssistExternalCommandEnvAddsArchiveRuntimeEnvForJohn`
- live evidence:
  - ZIP run `run-s37-rtshape-zip-20260307-151500` no longer emitted `adapt_archive_workflow`; remaining mutation came from generic path repair.
  - This confirms the owner removal is correct, but the next overlap is still generic runtime shaping (`prepare_runtime_command`, `repair_missing_inputs`) and recovery semantics.

### Slice 2

Reduce assist-mode weak vulnerability rewrites:
- stop `adaptWeakVulnerabilityAction(...)` from replacing weak actions with concrete `nmap`.
- keep evidence authenticity checks and scope enforcement.

Expected gain:
- vulnerability mapping stays LLM-owned; runtime no longer chooses scan content.

Status:
- implemented on 2026-03-07 together with assist-mode `prepare_runtime_command` and weak report rewrite removal.
- validation:
  - `internal/orchestrator/runtime_command_pipeline_test.go:TestApplyRuntimeCommandPipelineSkipsPrepareStageForAssistNmapTask`
  - `internal/orchestrator/runtime_command_pipeline_test.go:TestApplyRuntimeCommandPipelineSkipsWeakVulnerabilityRewriteForAssistTask`
  - `internal/orchestrator/runtime_command_pipeline_test.go:TestApplyRuntimeCommandPipelineSkipsWeakReportRewriteForAssistTask`
  - `internal/orchestrator/worker_runtime_assist_scope_sync_test.go:TestRunWorkerTaskAssistCommandScopeValidationDoesNotInjectTargetForAssistTask`
- live evidence:
  - ZIP run `run-s37-rtshape2-zip-20260307-153200` showed no `runtime_adapt` stage before failure; command-shape ownership stayed with the LLM.
  - Router run `run-s37-rtshape2-router-20260307-153600` showed no `runtime_adapt` stage on the initial port scan; remaining failure moved to degraded recovery after a primary assistant error.
  - This confirms the next overlap is no longer command shaping for these assist tasks; it is recovery/fallback behavior and artifact truth during replans.

### Slice 3

Narrow `command_adaptation.go` to safety-only shaping:
- keep bounded host-timeout / retry / max-rate guardrails where they only cap runtime,
- remove intent-changing rewrites that change scan class or discovery depth.

Expected gain:
- runtime bounding remains, but action shape stays closer to the LLM decision.

## Acceptance Signal

This reduction is working when live ZIP/router runs show:

- fewer `runtime_adapt` decisions,
- command lines closer to raw LLM choices,
- more failures surfaced as exact execution feedback instead of mutated alternatives,
- recovery driven by the latest result, not by runtime-invented variants.
