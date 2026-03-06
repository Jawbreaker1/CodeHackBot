# Salvage Sprint 2 Discoveries

Purpose
- Preserve high-signal findings from Sprint 37 so they are not lost during context compaction.
- Keep this log append-only and evidence-linked.

Entry format
- `id`: `S37-D###`
- `date_utc`: `YYYY-MM-DD`
- `area`: `cli | orchestrator | shared`
- `symptom`: what failed (observable behavior)
- `hypothesis`: likely root cause (before fix)
- `evidence`: run/session IDs + artifact paths
- `decision`: action taken (or deferred)
- `task_map`: Sprint 37 task(s) this maps to
- `status`: `open | validating | resolved | deferred`

Checkpoint discipline (locked)
- A Sprint 37 checkpoint is only complete when both are updated in the same pass:
  - this file (`salvage2_discoveries.md`) with discovery + `task_map` + `status`
  - `TASKS.md` with matching task-state/priority changes

---

## S37-D001
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: successful outcomes were not always emitted as verified completion contracts.
- `hypothesis`: completion contract enforcement differed across execution paths and could be bypassed by terminalization path differences.
- `evidence`: multiple `secret.zip` runs showing answer success without consistent contract success (see session artifacts in `sessions/*/artifacts/assist/` and `result_snapshot.md` availability).
- `decision`: treat as core reliability issue; enforce unified completion contract and fail-closed terminalization.
- `task_map`: Sprint 37 `Critical 1`, `Critical 2`
- `status`: `open`

## S37-D002
- `date_utc`: `2026-03-06`
- `area`: `cli`
- `symptom`: recovery could return to prompt early after partial repair instead of finishing plan/finalize path.
- `hypothesis`: plan execution loop allowed handled-failure branch to terminate the loop in some cases.
- `evidence`: runs where intermediate recovery succeeded but objective remained unmet and execution returned prematurely.
- `decision`: keep as regression guard focus; verify bounded post-plan recovery before terminal return.
- `task_map`: Sprint 37 `Critical 1`, `Critical 2`
- `status`: `open`

## S37-D003
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: stopped/interrupted runs could present incoherent end state vs counters/report readiness.
- `hypothesis`: terminal status projection and report assembly were not guaranteed to execute through one consistent path.
- `evidence`: prior runs where stop behavior required manual status/report checks to confirm final state.
- `decision`: make terminalization pipeline mandatory on stop/interrupt with explicit regression checks.
- `task_map`: Sprint 37 `Critical 1`, `High — State and reporting consistency`
- `status`: `open`

## S37-D004
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: runtime command mutation/adaptation stack produced hard-to-predict behavior and occasional intent drift.
- `hypothesis`: overlapping rewrite/repair stages with unclear ownership and ordering.
- `evidence`: command mutation issues observed across recovery/adaptation paths and previous cleanup discussions.
- `decision`: collapse to one ordered pre-exec pipeline with stage telemetry; remove overlapping transforms.
- `task_map`: Sprint 37 `Critical 3`
- `status`: `open`

## S37-D005
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: incorrect path/type substitutions during repairs (archive/hash/wordlist confusion) caused non-productive loops.
- `hypothesis`: path repair lacked strict artifact-type matching before substitution.
- `evidence`: recovery attempts using incompatible artifacts after command failures.
- `decision`: enforce typed input-repair contract and add cross-type rejection tests.
- `task_map`: Sprint 37 `Critical 4`
- `status`: `open`

## S37-D006
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: state/status behavior is difficult to reason about during long runs and recoveries.
- `hypothesis`: state model has overlapping meanings across CLI/orchestrator and ambiguous transition ownership.
- `evidence`: repeated operator confusion around run/task status vs actual execution phase in live sessions.
- `decision`: perform explicit state/status audit and publish simplified canonical state map before further behavior patches.
- `task_map`: Sprint 37 `Phase 0`, `High — State and reporting consistency`
- `status`: `open`

## S37-D007
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: agent behavior still appears constrained by static instruction/rewrite paths in some flows.
- `hypothesis`: legacy adapters/fallbacks and instruction patches remain in runtime, reducing LLM-driven correction space.
- `evidence`: observed behavior drift toward deterministic/static handling in cases expected to be model-driven.
- `decision`: inventory and classify all static instruction paths; keep only required safety contracts and sunset temporary supports.
- `task_map`: Sprint 37 `Phase 0`, `Medium — Static-instruction minimization policy`
- `status`: `open`

## S37-D008
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: no single document captured control flow, state overlap, static-path ownership, and patch sequencing together.
- `hypothesis`: without one canonical Phase 0 diagnostic artifact, implementation will drift back to tactical fixes.
- `evidence`: repeated context resets and mixed remediation discussions in recent sessions.
- `decision`: publish consolidated Phase 0 diagnostic in `docs/sprints/sprint37_phase0_diagnostic.md`.
- `task_map`: Sprint 37 `Phase 0` (all checklist items)
- `status`: `resolved`

## S37-D009
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: run/task state reporting is difficult to reason about during failures and stops.
- `hypothesis`: multiple domains (`RunPhase`, `RunOutcome`, run/task/worker projections) are valid but not clearly prioritized as authoritative vs derived.
- `evidence`: state model and projection code reviewed in `internal/orchestrator/state_machine.go`, `internal/orchestrator/run_phase.go`, `internal/orchestrator/run_outcome.go`, `internal/orchestrator/event_projection.go`, `internal/orchestrator/event_cache.go`.
- `decision`: lock canonical map: task state + run outcome authoritative; projections read-only.
- `task_map`: Sprint 37 `Critical 1`, `Critical 2`, `High — State and reporting consistency`
- `status`: `open`

## S37-D010
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: static adapter and rewrite layers remain broad and can reduce model autonomy.
- `hypothesis`: capability-level supports are mixed with tool-specific convenience behaviors in production paths.
- `evidence`: inventory across `assistant_fallback.go`, `command_adaptation.go`, `runtime_input_repair.go`, `runtime_archive_workflow.go`, `runtime_vulnerability_evidence.go`, and `cmd_exec.go`.
- `decision`: classify into `required safety`, `temporary support`, `remove`; treat `cmd_exec.go` tool-specific augmentation as remove candidate.
- `task_map`: Sprint 37 `Critical 3`, `Medium — Static-instruction minimization policy`
- `status`: `open`

## S37-D011
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: patch order changed frequently based on latest failure, not root-cause sequence.
- `hypothesis`: missing explicit ordering from root causes to Critical 1-4 tasks.
- `evidence`: prior iterations prioritized tactical reliability fixes before shared invariants.
- `decision`: lock Phase 0 sequence in diagnostic doc: terminal truth -> mutation stack -> typed repair -> state/report coherence -> context/static minimization.
- `task_map`: Sprint 37 `Critical 1-4`, `High`, `Medium`
- `status`: `resolved`

## S37-D012
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: focus can drift when discoveries are recorded but sprint tasks are not updated (or vice versa).
- `hypothesis`: missing an explicit checkpoint rule allows planning artifacts to diverge under high iteration speed.
- `evidence`: repeated priority shifts and context compaction pressure during salvage loops.
- `decision`: lock two-file checkpoint discipline: every completed investigation/implementation checkpoint must update both `docs/sprints/salvage2_discoveries.md` and `TASKS.md` in the same pass.
- `task_map`: Sprint 37 process control (all phases)
- `status`: `resolved`

## S37-D013
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: interrupt path (`ctx.Done`) could terminate loop without assembling report artifacts, leaving `report_ready=false` on aborted runs.
- `hypothesis`: `executeCoordinatorLoop` had separate stop branches and only one branch emitted run report.
- `evidence`: code path audit in `cmd/birdhackbot-orchestrator/main_runtime.go`; regression coverage added in `cmd/birdhackbot-orchestrator/main_runtime_terminal_test.go`.
- `decision`: unify interrupted/stopped terminalization into a shared aborted-terminalizer that always performs stop, stop-event convergence, evidence ingest, run outcome/phase updates, and report emission.
- `task_map`: Sprint 37 `Critical 1`
- `status`: `resolved`

## S37-D014
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: terminal run status could still show non-zero `active_workers`/`running_tasks` headline counters after stop/completion.
- `hypothesis`: projection counters were derived from task/worker event history without terminal-state normalization.
- `evidence`: run projection logic in `internal/orchestrator/event_cache.go` and `internal/orchestrator/run.go`; validated by new terminalization regression tests.
- `decision`: normalize terminal headline counters to zero when state is `stopped|completed` while preserving unfinished detail in underlying events/artifacts.
- `task_map`: Sprint 37 `Critical 1`, `High — State and reporting consistency`
- `status`: `resolved`

## S37-D015
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: command-action `task_completed` contracts lacked objective-proof fields, while CLI assist contracts required explicit objective/evidence rationale.
- `hypothesis`: command completion path used artifact/finding presence only and did not emit a parity contract payload (`objective_met`, `why_met`, `evidence_refs`).
- `evidence`: command completion emission in `internal/orchestrator/worker_runtime.go`; completion payload schema mismatch vs CLI contract model.
- `decision`: add command completion-contract builder (`buildCommandCompletionContract`) and emit fail-closed proof fields for command actions; align assist/validator completion payloads with the same core fields.
- `task_map`: Sprint 37 `Critical 2`
- `status`: `resolved`

## S37-D016
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: successful command exit could still produce `task_completed` after usage/help or effectively empty/no-op evidence.
- `hypothesis`: evidence validation focused on specific workflows and missed generic usage/no-op completion blocking.
- `evidence`: runtime validation behavior in `internal/orchestrator/runtime_nmap_evidence_retry.go` and command completion emission path in `internal/orchestrator/worker_runtime.go`.
- `decision`: enforce generic usage/help rejection and fail-closed no-op completion gate (empty output without meaningful evidence) before `task_completed`.
- `task_map`: Sprint 37 `Critical 2`
- `status`: `resolved`

## S37-D017
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: state/workflow/mutation references were scattered across multiple files, making it easy to lose focus and reintroduce overlap.
- `hypothesis`: missing concise control docs for workflow and mutation ownership increases planning drift during salvage iterations.
- `evidence`: repeated operator confusion around state/status semantics and rewrite ownership in live iterations.
- `decision`: add dedicated control docs:
  - `docs/runbooks/workflow-state-contract.md`
  - `docs/runbooks/runtime-mutation-stage-map.md`
  and require same-PR maintenance when these contracts change.
- `task_map`: Sprint 37 process control, `Critical 3`, `High — State and reporting consistency`
- `status`: `resolved`

## S37-D018
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: control docs existed for states/mutations, but execution contract, failure mapping, and acceptance gates were still spread across tasks/chat context.
- `hypothesis`: missing compact reference docs increases drift risk and weakens consistent checkpointing.
- `evidence`: operator request to centralize workflow/state references and reduce overlap during salvage sprint execution.
- `decision`: add three control docs and wire them into project references:
  - `docs/runbooks/execution-contract-matrix.md`
  - `docs/runbooks/failure-taxonomy.md`
  - `docs/runbooks/acceptance-gates.md`
  plus add them to Sprint 37 control-doc maintenance scope in `TASKS.md`.
- `task_map`: Sprint 37 process control (focus discipline + state/workflow contract continuity)
- `status`: `resolved`

## S37-D019
- `date_utc`: `2026-03-06`
- `area`: `shared`
- `symptom`: control docs were added, but sprint execution lacked a single always-visible sync checklist.
- `hypothesis`: without a top-of-file checklist, checkpoint maintenance can drift during fast iteration.
- `evidence`: operator request to keep control docs continuously up to date.
- `decision`: add a locked control-doc sync checklist near the top of `TASKS.md` to enforce review at sprint checkpoints and sprint close.
- `task_map`: Sprint 37 process control (checkpoint discipline)
- `status`: `resolved`

## S37-D020
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: command-action runtime mutations were split between an early prepare/adapt path and a second late pre-exec block (`msf` adaptation + input repair + archive adaptation), increasing overlap and intent-drift risk.
- `hypothesis`: command behavior remained hard to reason about because pre-exec transforms were not enforced through one ordered pipeline.
- `evidence`: code-path review and implementation changes in:
  - `internal/orchestrator/runtime_command_pipeline.go`
  - `internal/orchestrator/worker_runtime.go`
  - `internal/orchestrator/runtime_command_pipeline_test.go`
- `decision`: unify command-action pre-exec mutations under `applyRuntimeCommandPipeline` (with explicit `runtime_stage` telemetry) and remove duplicated late mutation block from worker runtime.
- `task_map`: Sprint 37 `Critical 3`
- `status`: `resolved`

## S37-D021
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: assist command execution could still mutate commands after scope validation (hidden second pass), creating overlap and potential intent drift.
- `hypothesis`: assist loop prepared/validated one command version while execution path reapplied runtime repair/adaptation on another pass.
- `evidence`: code-path convergence in:
  - `internal/orchestrator/worker_runtime_assist_loop.go`
  - `internal/orchestrator/worker_runtime_assist_exec.go`
  - `internal/orchestrator/runtime_command_pipeline.go`
  with regression validation via orchestrator test suite.
- `decision`: route assist `type=command` suggestions through `applyRuntimeCommandPipeline` before validation, then execute with `skipRuntimeMutation=true` to prevent hidden post-validation rewrites.
- `task_map`: Sprint 37 `Critical 3`
- `status`: `resolved`

## S37-D022
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: proof-sensitive zip/local workflows could still end as `task_completed` via `assist_no_new_evidence`, producing false-positive completed runs with synthetic fallback artifacts.
- `hypothesis`: objective truth (`objective_met`) was not enforced in coordinator completion-contract validation/gate checks, and no-new-evidence fallback still terminalized as completion for local workflow tasks.
- `evidence`:
  - live run `run-live-llm-zip-all-regen-20260306-152801` completed with `assist_no_new_evidence` while `crack_log.txt` stated `No new evidence...`.
  - report showed completed state without proof-backed objective satisfaction.
- `decision`:
  - reject no-new-evidence completion for local proof-sensitive workflows and emit `objective_not_met` failure instead of `task_completed`.
  - enforce `objective_met=true` for proof-sensitive archive completion contracts in coordinator and completion gate verification.
  - prevent expected-artifact synthesis when objective is not met.
- `task_map`: Sprint 37 `Critical 2` (completion truthfulness), carryover completion gate objective-proof enforcement
- `status`: `resolved`

## S37-D023
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: live `planner=llm` goal runs intermittently fail before execution with scheduler preflight cycle errors (`cycle detected at task ...`) after planner retries/regeneration.
- `hypothesis`: planner output repair/regeneration path can still emit cyclic task dependencies that are not normalized to acyclic DAG before scheduler preflight.
- `evidence`:
  - `run-live-llm-zip-all-20260306-152307`
  - `run-live-llm-zip-postfix-20260306-155241`
  - `run-live-llm-zip-postfix-20260306-155706-r2`
- `decision`:
  - add bounded planner dependency repair on scheduler-preflight cycle/unknown-dependency failures: prune `depends_on` edges that are `self`, `unknown`, `duplicate`, or `forward` (non-DAG) and revalidate before accepting plan.
  - preserve fail-closed behavior if repaired graph still fails validation.
  - add regression coverage for direct cycle repair and `buildGoalPlanFromMode(auto)` recovery.
  - run minimal live `planner=llm` smoke to verify preflight-cycle abort no longer blocks goal planning.
- `evidence_after_fix`:
  - tests: `go test ./cmd/birdhackbot-orchestrator ./internal/orchestrator`
  - live smoke: `run-live-llm-cyclefix-20260306-161737` (planner succeeded; no scheduler preflight cycle abort)
- `task_map`: Sprint 37 carryover completion gate (`planner=llm` live reliability)
- `status`: `resolved`
