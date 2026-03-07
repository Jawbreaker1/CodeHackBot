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

## S37-D024
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: runtime input repair could substitute incompatible artifact paths (for example hash/log artifacts for archive/report inputs) because selection used token similarity without typed compatibility checks.
- `hypothesis`: missing input arguments lacked explicit artifact-kind expectations, so dependency candidate matching could cross alias between artifact classes during repair, especially with wildcard/relative paths.
- `evidence`:
  - code path: `internal/orchestrator/runtime_input_repair.go` used untyped `bestArtifactCandidateForMissingPath(...)` for both direct and shell-wrapper path replacement.
  - regression need: wildcard and relative-path inputs could be over-matched by generic token scoring.
- `decision`:
  - add generic typed matching layer (`archive|hash|wordlist|log|report`) in runtime input repair and enforce kind compatibility before substitution.
  - reject wildcard path auto-repair (`* ? [ ]`) to avoid ambiguous aliasing.
  - add regressions for cross-type alias rejection and wildcard/relative-path safety.
- `evidence_after_fix`:
  - `internal/orchestrator/runtime_input_repair.go`
  - `internal/orchestrator/worker_runtime_test.go`:
    - `TestBestArtifactCandidateForMissingPathRejectsCrossTypeAlias`
    - `TestBestArtifactCandidateForMissingPathRejectsWildcardAlias`
    - `TestRepairMissingCommandInputPathsRejectsRelativeCrossTypeAlias`
    - `TestRepairMissingCommandInputPathsRejectsWildcardAlias`
  - validation: `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
- `task_map`: Sprint 37 `Critical 4`
- `status`: `resolved`

## S37-D025
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: current typed input-repair guard uses a finite artifact-kind set (`archive|hash|wordlist|log|report`), which risks becoming brittle as artifact diversity grows.
- `hypothesis`: the runtime needs a more generic family-based safety model (intent-driven + metadata-aware) rather than expanding per-kind heuristics.
- `evidence`:
  - operator concern: artifact variants are effectively unbounded, so fixed labels can drift into maintenance/hardcoding burden.
  - current implementation relies partly on filename classification for expected/candidate kind compatibility.
- `decision`: implement generalized repair-family model with unknown-family fail-closed behavior after plan approval.
- `planning_artifact`:
  - `docs/runbooks/input-repair-family-design.md`
- `soundness_validation`:
  - mapped to `docs/runbooks/workflow-state-contract.md` (no new state/status authority).
  - mapped to `docs/runbooks/execution-contract-matrix.md` (`execute` fail-closed on ambiguous substitutions).
  - mapped to `docs/runbooks/failure-taxonomy.md` (`contract_failure`/`strategy_failure` terminal honesty).
  - mapped to `docs/runbooks/runtime-mutation-stage-map.md` (stays within `repair_missing_inputs` stage).
  - mapped to `docs/runbooks/llm-freedom-guardrail-audit.md` (generic capability-level guardrails only).
- `task_map`: Sprint 37 `High — Input-repair type system generalization`
- `evidence_after_fix`:
  - implementation:
    - `internal/orchestrator/runtime_input_repair.go` (family-based classification + intent-first derivation + unknown-family fail-closed exact-basename rule).
    - `internal/orchestrator/runtime_input_repair_test.go` (unknown-family and intent-precedence acceptance tests).
  - validation:
    - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
- `status`: `resolved`

## S37-D026
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: terminal run status intentionally zeroed `active_workers`/`running_tasks`, but this hid unfinished execution context and made postmortem triage harder.
- `hypothesis`: status needs two layers: coherent headline counters for terminal state + preserved unfinished detail for diagnostics.
- `evidence`:
  - projection code in `internal/orchestrator/run.go` and `internal/orchestrator/event_cache.go` zeroed terminal headline counters without exposing raw pre-zero values.
- `decision`:
  - keep headline counters fail-safe at terminal states.
  - add dedicated terminal detail counters: `terminal_active_workers`, `terminal_queued_tasks`, `terminal_running_tasks`.
  - expose terminal detail fields in status command output when non-zero.
- `evidence_after_fix`:
  - `internal/orchestrator/engine.go`
  - `internal/orchestrator/run.go`
  - `internal/orchestrator/event_cache.go`
  - `cmd/birdhackbot-orchestrator/main_commands.go`
  - tests:
    - `internal/orchestrator/run_test.go:TestBuildRunStatusTerminalPreservesUnfinishedDetailCounters`
    - `internal/orchestrator/run_test.go:TestManagerStatusTerminalPreservesUnfinishedDetailCounters`
  - validation: `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
- `task_map`: Sprint 37 `High — State and reporting consistency`
- `status`: `resolved`

## S37-D027
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: ZIP/local-file recovery flows could still materialize critical expected artifacts from generic command stdout fallback when concrete files were missing, risking claim-like artifact synthesis.
- `hypothesis`: concrete-source gating covered password/proof names but did not include extraction/decryption status/file-claim artifacts, leaving residual synthetic-fallback exposure.
- `decision`:
  - expand local-workflow critical artifact detection beyond password/proof to include extraction/decryption status/file claim outputs.
  - enforce concrete-source requirement for those critical artifacts in command expected-artifact materialization and skip stdout fallback when source files are absent.
- `evidence_after_fix`:
  - `internal/orchestrator/runtime_archive_proof.go`
  - `internal/orchestrator/worker_runtime.go`
  - tests:
    - `internal/orchestrator/runtime_archive_proof_test.go:TestLocalWorkflowCriticalArtifactName`
    - `internal/orchestrator/runtime_archive_proof_test.go:TestExpectedArtifactRequiresConcreteSource_ArchiveWorkflow`
    - `internal/orchestrator/worker_runtime_test.go:TestWriteExpectedCommandArtifactsSkipsLocalWorkflowCriticalFallback`
  - validation: `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
- `task_map`: Sprint 37 carryover completion gate (`disable synthetic stdout fallback for critical goal artifacts`)
- `status`: `resolved`

## S37-D028
- `date_utc`: `2026-03-06`
- `area`: `cli`
- `symptom`: live qwen3.5 CLI acceptance reruns show mixed completion behavior: successful crack/extract outcomes can still be followed by repeated completion chatter under queued `continue` inputs, while some runs terminate at EOF before final objective confirmation.
- `hypothesis`: current gate-driving method (piped batch input with multiple `continue` lines) is biasing interaction flow and masking clean terminalization behavior; a bounded interactive harness is needed for reliable acceptance measurement.
- `evidence`:
  - `sessions/20260306-174840-a3dd` (partial: password recovered, no finalized objective before EOF, reasoning leak in final response).
  - `sessions/20260306-175138-db2f` (objective achieved; repeated completion confirmations while queued `continue` remained).
  - `sessions/20260306-175932-90ae` (long-running cracking churn; operator stop for budget control).
  - `sessions/20260306-181309-c84e` (router scenario entered repeat read/curl/report churn after initial evidence-backed reconnaissance; did not cleanly terminalize before operator stop).
- `decision`: keep acceptance gate open; add a bounded live-gate execution method that avoids queued-input overhang and records terminal outcome deterministically before evaluating `>=5/5`.
- `task_map`: Sprint 37 carryover (`live qwen3.5 CLI acceptance gate`)
- `status`: `open`

## S37-D029
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: post-implementation live checks (one ZIP, one router) still show objective-level completion gaps despite stable run terminalization/report generation.
- `evidence`:
  - `run-s37-zip2-20260306-192543`
    - run terminalized with report, but ZIP objective remained unmet (`assist_no_new_evidence`/`budget_exhausted` churn path); downstream extract/report tasks remained queued.
  - `run-s37-router-20260306-192140`
    - run terminalized with report; candidate CVE follow-up blocked on out-of-scope source lookup target (`nvd.nist.gov`) and retained as `UNVERIFIED`.
- `decision`:
  - keep Sprint 37 acceptance gate open.
  - prioritize follow-up on objective-proof terminal consistency for queued downstream tasks after bounded recovery exhaustion.
  - preserve scope policy for external lookup, but improve source-validation strategy to use in-scope/local evidence providers first when possible.
- `task_map`: Sprint 37 carryover (`live qwen3.5 acceptance gate`, objective-proof consistency)
- `status`: `open`

## S37-D030
- `date_utc`: `2026-03-06`
- `area`: `orchestrator`
- `symptom`: layered assist context still favored fixed recency (`tail(observations)`, latest artifact modtimes), so recovery turns over-consumed inspection churn and under-exposed failure anchors, attempt deltas, and semantic evidence files.
- `evidence`:
  - code path before fix:
    - `internal/orchestrator/worker_runtime_assist_context_layered.go` used `selectTail(..., "latest")` for observations and artifact mtime ordering for current task artifacts.
  - live run correlation:
    - `run-s37-zip-20260306-191300`
    - `run-s37-zip2-20260306-192543`
    - both showed repeated `read_file` / `list_dir` churn around the same hash/path evidence.
- `decision`:
  - replace fixed-window selection with anchor-aware ranking for observations and current-task artifacts.
  - add explicit `recovery_anchors` section built from recover hint, high-signal observations, task contract, and dependency artifacts.
  - de-prioritize generic inspection churn artifacts (`worker-*.log`) behind anchored semantic artifacts.
- `evidence_after_fix`:
  - implementation:
    - `internal/orchestrator/worker_runtime_assist_context_layered.go`
  - tests:
    - `internal/orchestrator/worker_runtime_assist_context_layered_test.go:TestBuildWorkerAssistLayeredContextPrioritizesFailureAnchorsOverInspectionChurn`
    - `internal/orchestrator/worker_runtime_assist_context_layered_test.go:TestBuildWorkerAssistLayeredContextPrioritizesAnchoredArtifacts`
  - validation:
    - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
- `task_map`: Sprint 37 `High — Context quality under long recovery`
- `status`: `resolved`

## S37-D031
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: raw planner traces for ZIP recovery runs still emitted bare tool labels (`zip2john`, `john`, `fcrackzip`, `unzip -t`) even when the planner request already contained goal, scope, working directory, hypotheses, and ZIP playbook examples with concrete args.
- `evidence`:
  - live run: `run-s37-trace2-zip-20260307-110408`
  - planner trace request: `sessions/planner-attempts/run-s37-trace2-zip-20260307-110408/attempt-01.request.json`
  - raw extracted planner output: `sessions/planner-attempts/run-s37-trace2-zip-20260307-110408/attempt-01.extracted.json`
  - normalized plan: `sessions/run-s37-trace2-zip-20260307-110408/orchestrator/plan/plan.json`
- `hypothesis`: planner JSON mode is collapsing action shape more than task context itself is failing; the immediate reliability fix should gate direct-command executability rather than add more scenario-specific runtime command hardcoding.
- `decision`:
  - treat this as a planner action-shape / executability problem, not primarily a context-window problem.
  - add a generic post-planner executability gate:
    - derive task-contract anchors from targets, file/path references, expected-artifact basenames, and dependency context,
    - if a direct command does not bind to available anchors and gives no concrete executable inputs, promote it to `assist`,
    - preserve the original tool name only as an optional hint.
  - keep the worker LLM responsible for expanding the concrete command during execution.
- `evidence_after_fix`:
  - planning/control docs:
    - `docs/runbooks/planner-action-executability.md`
    - `docs/runbooks/execution-contract-matrix.md`
    - `docs/runbooks/workflow-state-contract.md`
    - `TASKS.md`
  - implementation:
    - `cmd/birdhackbot-orchestrator/main_planner_actionshape.go`
    - `cmd/birdhackbot-orchestrator/main_planner.go`
  - tests:
    - `cmd/birdhackbot-orchestrator/main_planner_actionshape_test.go:TestNormalizePlannerCommandExecutabilityPromotesBareBoundCommand`
    - `cmd/birdhackbot-orchestrator/main_planner_actionshape_test.go:TestNormalizePlannerCommandExecutabilityPreservesConcreteBoundCommand`
    - `cmd/birdhackbot-orchestrator/main_planner_actionshape_test.go:TestNormalizePlannerCommandExecutabilityPreservesAnchorlessBenignCommand`
  - validation:
    - `go test ./cmd/birdhackbot-orchestrator ./internal/orchestrator`
- `task_map`: Sprint 37 `High — Planner action executability gate`
- `status`: `resolved`

## S37-D032
- `date_utc`: `2026-03-07`
- `area`: `scopeguard`
- `symptom`: local ZIP assist steps in `--scope-local` orchestrator runs were being blocked as `scope_denied` because quoted wrapper tokens like `FILE_NOT_FOUND:secret.zip` were still parsed as host-style literals (`secret.zip`).
- `evidence`:
  - live run before fix: `run-s37-zipexec-20260307-111755`
  - failure: `scope violation: out of scope target secret.zip`
- `decision`:
  - treat quoted local filesystem tokens as file-like before host extraction by trimming wrapper punctuation in `looksLikeFileToken(...)`.
  - keep fail-closed network validation unchanged for real host/IP/url tokens.
- `evidence_after_fix`:
  - implementation: `internal/scopeguard/policy.go`
  - regression: `internal/scopeguard/policy_test.go:TestValidateCommandWrapperIgnoresQuotedLocalArchivePathAsHostTarget`
  - validation: `go test ./internal/scopeguard ./internal/orchestrator`
  - live verification: `run-s37-zipexec2-20260307-112707` progressed past `T-01-scope-verify` and executed `zipinfo` in `T-02-archive-metadata`
- `task_map`: Sprint 37 exit gate (`secret.zip` orchestrator smoke)
- `status`: `resolved`

## S37-D033
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: after the local-path scope fix, ZIP smoke progressed into metadata extraction, but recovery still failed by reading `scope_inventory.txt` from repo root instead of the produced task artifact path.
- `evidence`:
  - live run: `run-s37-zipexec2-20260307-112707`
  - failed task: `T-02-archive-metadata`
  - failure: `open /home/johan/birdhackbot/CodeHackBot/scope_inventory.txt: no such file or directory`
- `hypothesis`: assist/recovery artifact retrieval is still favoring bare expected-artifact names over concrete produced artifact paths, despite the artifact existing under the task artifact directory.
- `task_map`: Sprint 37 carryover (`secret.zip` orchestrator smoke)
- `evidence_after_fix`:
  - implementation:
    - `internal/orchestrator/worker_runtime_assist_exec.go`
    - `internal/orchestrator/runtime_input_repair.go`
  - regression:
    - `internal/orchestrator/worker_runtime_assist_builtin_test.go:TestExecuteWorkerAssistCommandBuiltinReadFileRepairsDependencyArtifactPath`
  - validation:
    - `go test ./internal/orchestrator ./internal/scopeguard`
  - live verification:
    - `run-s37-zipexec2-20260307-112707` no longer fails on `read_file scope_inventory.txt`; run progressed into recovery past that exact missing-artifact-path fault
- `status`: `resolved`

## S37-D034
- `date_utc`: `2026-03-07`
- `area`: `process`
- `symptom`: repeated ZIP-focused fixes risk regressing into scenario-shaped behavior without enough live evidence from different task families.
- `decision`:
  - expand Sprint 37 exit discipline to require at least two materially different live smokes beyond ZIP before declaring the salvage stable.
  - use those runs as anti-hardcoding checks, not just feature demos.
- `task_map`: Sprint 37 exit criteria, `Cross-scenario anti-hardcoding gate`
- `status`: `open`

## S37-D035
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: after fixing planner executability, local-path scope, and dependency artifact-path preference, ZIP smoke still churns in metadata extraction because recovery keeps re-reading/re-writing `archive_metadata.txt` without converging to a usable “encrypted/password-protected” conclusion.
- `evidence`:
  - live run: `run-s37-zipexec3-20260307-113641`
  - repeated recover loop around:
    - `zipinfo -v secret.zip | tee archive_metadata.txt`
    - `read_file /home/johan/birdhackbot/CodeHackBot/archive_metadata.txt`
- `hypothesis`: recovery/evaluation is still weak at extracting semantic state from produced metadata artifacts, so it keeps looping on the same evidence instead of promoting the conclusion into the next step.
- `task_map`: Sprint 37 carryover (`secret.zip` orchestrator smoke), cross-scenario anti-hardcoding gate
- `status`: `open`

## S37-D036
- `date_utc`: `2026-03-07`
- `area`: `architecture`
- `symptom`: after multiple salvage fixes, failures are still surfacing as small boundary bugs in the same chain (`planner -> action shaping -> artifact truth -> recovery input`), suggesting ownership overlap more than missing capability.
- `evidence`:
  - planner structured-output collapse required executability post-processing,
  - local path scope false positive required token-interpretation fix,
  - dependency artifact recovery preferred basename placeholders over canonical produced paths,
  - metadata recovery still loops on rereading the same artifact instead of converting evidence into the next step.
- `decision`:
  - pause broad tactical hardening and perform a narrow architecture checkpoint on the failing chain.
  - use that checkpoint as a Sprint 37 gate before more ZIP/router tactical patches.
  - prioritize simplification and overlap reduction over new adapters.
- `evidence_after_fix`:
  - checkpoint doc: `docs/runbooks/agent-workflow-checkpoint-s37.md`
  - sprint control update: `TASKS.md`
- `task_map`: Sprint 37 `Critical checkpoint — Simplify agent workflow ownership before more tactical fixes`
- `status`: `resolved`

## S37-D037
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: live router validation still matched the checkpoint: host discovery completed, but service-enum recovery started rereading generated command logs instead of converting the scan evidence into a next action.
- `evidence`:
  - live run: `run-s37-routerchk-20260307-115603`
  - `T-01` completed with verified evidence.
  - `T-02` produced a real `nmap -sV` command log, then followed it with chained `read_file` calls on:
    - `worker-T-02-a1-a1-s1-t1.log`
    - `worker-T-02-a1-a1-s2-t2.log`
    - `worker-T-02-a1-a1-s3-t3.log`
- `hypothesis`: recovery was still consuming execution transcripts as fresh evidence because the recovery prompt did not foreground a smaller explicit state object.
- `decision`:
  - use Target 3 from `docs/runbooks/agent-workflow-checkpoint-s37.md` as the first overlap-reduction implementation slice.
  - expose explicit recovery state (`previous_command`, `previous_exit_code`, `primary_evidence_refs`, `contract_gap`) ahead of observation churn.
  - block recover-mode execution-log reread chains as low-value churn.
- `evidence_after_fix`:
  - code:
    - `internal/orchestrator/worker_runtime_assist_context_layered.go`
    - `internal/orchestrator/worker_runtime_assist_loop.go`
    - `internal/orchestrator/worker_runtime_assist_loop_helpers.go`
  - tests:
    - `TestBuildWorkerAssistLayeredContextIncludesExplicitRecoveryState`
    - `TestDetectRecoverExecutionLogInspection`
    - `TestRunWorkerTaskAssistRecoverExecutionLogLoopFailsFast`
- `task_map`: Sprint 37 `Critical checkpoint — Simplify agent workflow ownership before more tactical fixes`
- `status`: `resolved`

## S37-D038
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: after the recovery-state slice, the router flow no longer reproduced the earlier `read_file` command-log chain in service enumeration or vuln-mapping; workers pivoted to the canonical XML evidence path and completed those tasks, but report generation failed later with `assistant tool suggestion has no files`.
- `evidence`:
  - live run: `run-s37-routerdeep-20260307-123600`
  - `T-01-port-scan`:
    - produced `nmap_router_scan.xml`
    - recover step read `/home/johan/birdhackbot/CodeHackBot/nmap_router_scan.xml`
    - completed without rereading `worker-*.log` transcripts
  - `T-02-vuln-mapping`:
    - read dependency artifact `/home/johan/birdhackbot/CodeHackBot/sessions/run-s37-routerdeep-20260307-123600/orchestrator/artifact/T-01-port-scan/nmap_router_scan.xml`
    - completed after one evidence read, again without transcript churn
  - `T-03-report-generation` failed with:
    - `reason=assist_loop_detected`
    - `schema_error=assistant tool suggestion has no files`
- `decision`:
  - treat the recovery-input slice as validated for this router scenario: evidence-first recovery improved and the old transcript reread loop did not recur.
  - next overlap-reduction target is not more ZIP/router-specific repair; it is reducing action-shape/tool-schema ambiguity that still surfaces in reporting/recover mode.
- `task_map`: Sprint 37 `Critical checkpoint — Simplify agent workflow ownership before more tactical fixes`, Sprint 37 exit criteria
- `status`: `open`

## S37-D039
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: generic action/tool-schema normalization improved assistant parsing, but live validation still exposed two earlier-than-expected boundary failures: ZIP metadata extraction hit duplicated absolute-path injection inside `bash -c`, and router recover mode still selected a worker command log over the canonical evidence artifact.
- `evidence`:
  - ZIP live run: `run-s37-postfix-zip-20260307-124500`
    - `T-01-metadata-extract` first command was repaired into:
      - `ls -la /home/johan/birdhackbot/CodeHackBot//home/johan/birdhackbot/CodeHackBot/secret.zip`
    - run failed before reaching cracking/reporting stages.
  - Router live run: `run-s37-postfix-router-20260307-124700`
    - `T-01-port-scan` produced canonical XML evidence path:
      - `/home/johan/birdhackbot/CodeHackBot/nmap_scan_192.168.50.1.xml`
    - recover step still chose:
      - `read_file .../orchestrator/artifact/T-01-port-scan/worker-T-01-port-scan-a1-a1-s1-t1.log`
    - meaning the execution-log churn guard did not catch this case.
- `decision`:
  - keep the generic tool-schema normalization; it is still sound and covered by unit tests.
  - next fix should remain generic and ownership-reducing:
    - stop absolute-path injection inside shell-script runtime repair,
    - widen execution-log churn detection so recover-mode cannot pivot from canonical evidence back to worker command logs.
- `evidence_after_fix`:
  - code:
    - `internal/assist/assistant_normalize.go`
  - tests:
    - `internal/assist/assistant_test.go:TestNormalizeSuggestionConvertsToolWithoutFilesToCommand`
- `task_map`: Sprint 37 `Critical checkpoint — Simplify agent workflow ownership before more tactical fixes`, Sprint 37 exit criteria
- `status`: `open`

## S37-D040
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: after the latest generic runtime fixes, both live smokes moved forward, but both now fail at the same narrower seam: post-command evidence interpretation still degrades to low-value rereads instead of pivoting from the newest artifact or the most recent command result.
- `evidence`:
  - ZIP live run: `run-s37-fix2-zip-20260307-125600`
    - shell-wrapper path duplication is fixed and `T-02` completes with `zip.hash`.
    - `T-03` reaches real cracking and executes:
      - `john --wordlist=/home/johan/birdhackbot/CodeHackBot/rockyou.txt --rules=John --format=pkzip .../zip.hash`
    - after `john` output, recovery does not pivot from the command result and instead repeats:
      - `read_file .../T-02/zip.hash`
    - task terminates as:
      - `objective_not_met`
      - `bounded_fallback=no_new_evidence`
      - `failure_class=strategy_failure`
  - Router live run: `run-s37-fix2-router-20260307-123100`
    - `T-01-port-scan` completes cleanly from canonical XML evidence and emits a valid completion contract.
    - `T-02-vuln-mapping` launches bounded vuln-scan `nmap` and produces:
      - `T-02-vuln-mapping/nmap_vuln_scan_192.168.50.1.xml`
    - after the command returns, recovery falls back to repeated reads of the older dependency artifact:
      - `read_file .../T-01-port-scan/nmap_scan_192.168.50.1.xml`
    - task terminates as:
      - `no_progress`
      - `failure_class=context_loss`
      - repeated low-value action on the older XML artifact
    - `SIGINT` stop still produced a coherent partial report.
- `decision`:
  - keep the latest generic runtime fixes; they removed earlier boundary faults and exposed the current narrower seam.
  - next fix should stay generic and ownership-reducing:
    - prefer the newest primary output artifact from the immediately preceding command over dependency artifacts during recovery,
    - pass a compact command-result summary into recovery so the LLM can pivot from tool output instead of rereading source inputs,
    - keep static fallback from regressing to dependency rereads when a newer artifact exists.
- `evidence_after_fix`:
  - code:
    - `internal/orchestrator/runtime_input_repair.go`
    - `internal/orchestrator/worker_runtime_assist_loop_helpers.go`
  - tests:
    - `internal/orchestrator/worker_runtime_test.go:TestRepairMissingCommandInputPathsForShellWrapperAvoidsDuplicatingInsertedAbsolutePath`
    - `internal/orchestrator/worker_runtime_assist_loop_helpers_test.go:TestDetectRecoverExecutionLogInspection`
- `task_map`: Sprint 37 `Critical checkpoint — Simplify agent workflow ownership before more tactical fixes`, Sprint 37 `High — Context quality under long recovery`, Sprint 37 exit criteria
- `status`: `open`

## S37-D041
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: the latest recovery-state prioritization slice improved internal signal quality, but the live runs show that fallback and adaptive-replan paths still discard that signal and regress to low-value source/log rereads.
- `evidence`:
  - ZIP live run: `run-s37-fix3-zip-20260307-133800`
    - `T-01` makes more real progress than before:
      - `zipinfo -v secret.zip`
      - `unzip -l secret.zip`
      - Python `zipfile` inspection
      - generated `zip_metadata_extractor.py` tool
    - after that, recover-mode falls into static fallback:
      - `Read the referenced local file to continue.`
      - `read_file /home/johan/birdhackbot/CodeHackBot/secret.zip`
    - this repeats until:
      - `objective_not_met`
      - `bounded_fallback=no_new_evidence`
      - `last_repeated_action=read_file .../secret.zip`
    - meaning the new recovery-state hints are not surviving the fallback path.
  - Router live run: `run-s37-fix3-router-20260307-134300`
    - `T-01-port-scan` now hits execution-log churn blocking immediately after reading its own worker log.
    - the adaptive replan task then reopens the same worker log twice before pivoting.
    - after one bounded `nmap` retry, the fallback path again regresses to `read_file` of the original worker log, this time with a malformed trailing `)` in the repaired path.
    - run ends with:
      - `task-rp-c895703f64`
      - `reason=command_failed`
      - missing file on the malformed log path
    - meaning the replan/fallback path still does not preserve the newest actionable result as recovery truth.
- `decision`:
  - keep the generic recovery-state additions; they are not the wrong direction.
  - next fix must target the remaining ownership boundary directly:
    - make fallback/replan consume the same canonical latest-result object as normal recovery,
    - stop fallback from defaulting to `read_file` on whichever path happened to be in the hint text,
    - keep path repair from inheriting trailing punctuation when fallback suggestions cite prior log paths.
- `evidence_after_fix`:
  - code:
    - `internal/orchestrator/worker_runtime_assist_feedback.go`
    - `internal/orchestrator/worker_runtime_assist_context_layered.go`
    - `internal/orchestrator/worker_runtime_assist_loop_helpers.go`
    - `internal/orchestrator/worker_runtime_assist_loop_mode.go`
  - tests:
    - `internal/orchestrator/worker_runtime_assist_feedback_test.go:TestCaptureAssistExecutionFeedbackInfersOutputArtifactRefs`
    - `internal/orchestrator/worker_runtime_assist_feedback_test.go:TestCaptureAssistExecutionFeedbackInfersPrimaryArtifactRefs`
    - `internal/orchestrator/worker_runtime_assist_loop_helpers_test.go:TestDetectRecoverDependencyArtifactRegression`
    - `internal/orchestrator/worker_runtime_assist_context_layered_test.go:TestBuildWorkerAssistLayeredContextIncludesExplicitRecoveryState`
- `task_map`: Sprint 37 `Critical checkpoint — Simplify agent workflow ownership before more tactical fixes`, Sprint 37 `High — Context quality under long recovery`, Sprint 37 exit criteria
- `status`: `open`

## S37-D042
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: execution failures were still creating two competing recovery narratives: the real `execution_failure` path from `task_failed`, and a weaker generic `new_finding` path from the synthetic `task_execution_failure` finding. That split was why some adaptive-replan goals still used the old generic wording and lost the newest-result context.
- `evidence`:
  - ZIP live run before fix: `run-s37-fix4-zip-20260307-135200`
    - `task_failed` payload already carried `latest_result_summary` / `latest_log_path`
    - but the spawned recovery task goal still started with generic text:
      - `Use recent artifacts/logs first. Avoid re-running the same failing command unchanged.`
    - indicating the recovery task came from the `new_finding` path instead of the richer `execution_failure` path.
  - router live run before fix: `run-s37-fix4-router-20260307-135600`
    - same duplicated ownership existed around `task_execution_failure` findings and recovery mutation.
- `decision`:
  - keep `task_execution_failure` as reportable telemetry,
  - but stop routing that finding type through `new_finding` adaptive-replan creation,
  - let `task_failed -> execution_failure` own adaptive recovery so there is one recovery goal builder and one latest-result source of truth.
- `evidence_after_fix`:
  - code:
    - `internal/orchestrator/coordinator_replan.go`
    - `internal/orchestrator/coordinator_test.go:TestCoordinator_TaskExecutionFailureFindingDoesNotEmitNewFindingReplan`
  - live verification:
    - `run-s37-next-zip-20260307-140336`: replan event now uses `trigger=execution_failure` only for the failure path; the generic old `new_finding` recovery wording does not appear for `task_execution_failure`.
    - `run-s37-next-router-20260307-140336`: `T-02-map-vulnerabilities` failure creates `run_replan_requested trigger=execution_failure` with no duplicate weaker `new_finding` recovery event for the same task failure.
- `task_map`: Sprint 37 `Critical checkpoint — Simplify agent workflow ownership before more tactical fixes`
- `status`: `resolved`

## S37-D043
- `date_utc`: `2026-03-07`
- `area`: `orchestrator`
- `symptom`: fallback path selection is less wrong than before, but parse-failure/static-fallback still regresses into inspection-only churn on the last evidence file instead of pivoting to a new executable validation step.
- `evidence`:
  - ZIP live run: `run-s37-next-zip-20260307-140336`
    - improvement:
      - fallback no longer turns a directory into `read_file /home/johan/birdhackbot/CodeHackBot` and crash-loops on “is a directory”.
    - remaining failure:
      - after metadata trouble, adaptive recovery falls back to repeated `read_file` of:
        - `.../T-01/worker-T-01-a1-a1-s6-t6.log`
      - task then bounded-fails with:
        - `inspection-only churn without validation command pivot`
  - router live run: `run-s37-next-router-20260307-140336`
    - improvement:
      - `task_execution_failure` no longer spawns the weaker duplicate replan path,
      - recovery stays on canonical `execution_failure`.
    - remaining failure:
      - `T-02-map-vulnerabilities` reads the same XML evidence twice,
      - then adaptive recovery reads that same XML again and no-progress terminalizes.
  - ZIP same run also exposed a separate upstream blocker:
    - archive adaptation is still malformed in `T-01`:
      - `zipinfo` receives duplicated/broken args (`"-1 /home/.../secret.zip", "/home/.../secret.zip"`)
      - which triggers the first recovery cycle unnecessarily.
- `decision`:
  - keep the latest-result and concrete-path fallback changes,
  - next slice must target static-fallback/parse-failure churn directly:
    - if latest evidence was just read with identical result, fallback must not suggest rereading it,
    - fallback should pivot to a new executable validation step or fail fast for replan,
    - archive workflow adaptation needs its own contract cleanup so metadata commands stay executable before recovery starts.
- `evidence_after_fix`:
  - code:
    - `internal/assist/assistant_fallback.go`
    - `internal/assist/assistant_test.go`
    - `internal/orchestrator/command_adaptation.go`
    - `internal/orchestrator/command_adaptation_test.go`
  - tests:
    - `internal/assist/assistant_test.go:TestFallbackAssistantRecoverDirectoryEvidenceUsesListDir`
    - `internal/assist/assistant_test.go:TestFallbackAssistantRecoverNonexistentArtifactPathUsesReadFile`
    - `internal/orchestrator/command_adaptation_test.go:TestAdaptCommandForRuntimeKeepsExplicitAllPortsDuringServiceEnumeration`
- `task_map`: Sprint 37 `High — Context quality under long recovery`, Sprint 37 exit criteria
- `status`: `open`

## S37-D044
- `date_utc`: `2026-03-07`
- `area`: `architecture`
- `symptom`: repeated live-run fixes kept improving local symptoms while preserving or increasing the number of action/recovery owners. This means the project can appear to progress while still moving away from the intended LLM-owned closed loop.
- `evidence`:
  - user review after latest ZIP/router cycles correctly identified that proposed fixes like:
    - archive command shaping,
    - special-case reread prevention,
    were treating symptoms rather than reducing architecture overlap.
  - current live evidence still shows the same structural pattern:
    - ZIP: malformed assist-mode runtime shaping triggers recovery too early,
    - router: fallback/parse-failure still re-reads existing evidence instead of choosing a materially new action,
    - both are signs of multiple decision owners rather than missing model capability.
- `decision`:
  - pause patch-driven behavior work.
  - require architecture-reduction-first slices only.
  - adopt a stricter rule:
    - every implementation must remove an owner, reduce mutation, or improve feedback fidelity,
    - otherwise it should not land during Sprint 37.
- `evidence_after_fix`:
  - docs:
    - `docs/runbooks/closed-loop-reduction-plan.md`
    - `docs/runbooks/agent-workflow-checkpoint-s37.md`
    - `docs/runbooks/ownership-boundaries.md`
    - `docs/runbooks/runtime-mutation-stage-map.md`
    - `docs/runbooks/workflow-state-contract.md`
  - task control:
    - `TASKS.md` now includes `Critical 5 — Closed-loop architecture reduction`
- `task_map`: Sprint 37 `Critical checkpoint — Simplify agent workflow ownership before more tactical fixes`, Sprint 37 `Critical 5 — Closed-loop architecture reduction`
- `status`: `resolved`

## S37-D045
- `date_utc`: `2026-03-07`
- `area`: `architecture`
- `symptom`: fallback behavior is not sufficiently documented as a decision owner. The project had state/workflow docs, but no explicit contract for what fallback may or may not do, which allowed fallback to drift into executable command synthesis.
- `evidence`:
  - current fallback owners span:
    - `internal/assist/assistant_fallback.go`
    - `internal/orchestrator/worker_runtime_assist_llm.go`
    - `internal/cli/llm_guard.go`
    - `internal/orchestrator/worker_runtime_assist_loop_emit.go`
  - the existing inventory/workflow docs (`state-inventory`, `workflow-state-contract`) describe lifecycle and terminalization, but do not define fallback as an ownership boundary.
  - live failures match this gap:
    - fallback repeatedly invents `read_file` / `list_dir` style next actions after LLM parse/primary failure,
    - degraded mode therefore becomes a second planner in both CLI and orchestrator.
- `decision`:
  - create a dedicated fallback-ownership control doc and treat fallback reduction as the first architecture-reduction slice.
  - explicitly classify fallback behavior into:
    - allowed: availability signaling, parse-repair, bounded terminalization, interactive question where appropriate
    - forbidden: command synthesis, path-driven pseudo-planning, recovery-strategy takeover
- `evidence_after_fix`:
  - docs:
    - `docs/runbooks/fallback-ownership-reduction.md`
    - `docs/runbooks/closed-loop-reduction-plan.md`
    - `docs/runbooks/ownership-boundaries.md`
  - task control:
    - `TASKS.md` updated so the next implementation slice is removal of executable command/tool synthesis from `FallbackAssistant`
- `task_map`: Sprint 37 `Critical 5 — Closed-loop architecture reduction`
- `status`: `resolved`

## S37-D046
- `date_utc`: `2026-03-07`
- `area`: `fallback / orchestrator`
- `symptom`: after removing executable command synthesis from `FallbackAssistant`, live runs became more honest about ownership boundaries: direct assist turns stayed LLM-led, but non-interactive degraded recovery now exposes a separate gap where fallback questions can loop because recovery still expects an actionable step.
- `evidence`:
  - implementation:
    - `internal/assist/assistant_fallback.go`
    - `internal/assist/doc.go`
    - `internal/assist/assistant_test.go`
  - validation:
    - `go test ./internal/assist ./internal/cli ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
  - live ZIP run: `run-s37-fallbackless-zip-20260307-143338`
    - no fallback command/tool synthesis observed in the metadata/recover path.
    - observed actions remained `decision_source=llm_direct`.
    - run terminalized coherently (`state=stopped`, `report_ready=true`) but still ended `objective_not_met` after repeated evidence inspection (`read_file secret_zip_hexdump.txt`).
  - live router run: `run-s37-fallbackless-router-20260307-143338`
    - run terminalized coherently (`state=stopped`, `report_ready=true`, `run_report_generated` emitted).
    - degraded recovery emitted question-only fallback (`Recover-mode fallback will not invent a new command.`) and then failed with `assist_loop_detected` because the worker is non-interactive.
- `decision`:
  - keep the fallback reduction.
  - do not restore command synthesis in fallback.
  - next reduction slice should remove more assist-mode runtime action shaping and make non-interactive degraded recovery fail/replan from the canonical latest execution result instead of looping on fallback questions.
- `task_map`: Sprint 37 `Critical 5 — Closed-loop architecture reduction`
- `status`: `open`

## S37-D047
- `date_utc`: `2026-03-07`
- `area`: `runtime shaping`
- `symptom`: after fallback command synthesis removal, the next remaining action-owner overlap is assist-mode runtime shaping. Multiple runtime layers still inject or rewrite archive/vulnerability commands before execution, which keeps executable action shape partly system-owned even when the LLM is healthy.
- `evidence`:
  - current shaping owners:
    - `internal/orchestrator/runtime_command_pipeline.go`
    - `internal/orchestrator/command_adaptation.go`
    - `internal/orchestrator/runtime_archive_workflow.go`
    - `internal/orchestrator/runtime_vulnerability_evidence.go`
    - `internal/orchestrator/runtime_input_repair.go`
    - `internal/msf/runtime_command.go`
  - current-state map:
    - `docs/runbooks/runtime-mutation-stage-map.md`
  - live correlation:
    - ZIP run `run-s37-fallbackless-zip-20260307-143338` still showed `runtime_adapt` in the execution path before later evidence-inspection churn.
    - prior ZIP/router runs already showed archive arg corruption and weak-action rewrites as separate mutation sources.
- `decision`:
  - document a reduced runtime-shaping contract.
  - keep only safety/scope/minimal wrapping/exact path canonicalization/runtime env setup.
  - remove or demote capability-specific input injection and weak-command rewrites from assist-mode execution.
- `evidence_after_fix`:
  - doc:
    - `docs/runbooks/runtime-shaping-reduction.md`
  - task control:
    - `TASKS.md` updated so Sprint 37 second reduction slice is explicitly documented before implementation.
- `task_map`: Sprint 37 `Critical 5 — Closed-loop architecture reduction`
- `status`: `resolved`

## S37-D048
- `date_utc`: `2026-03-07`
- `area`: `runtime shaping / assist`
- `symptom`: assist-mode archive workflows still passed through `adapt_archive_workflow`, so runtime could inject ZIP inputs before execution. That kept archive command shape partly system-owned even after fallback reduction.
- `evidence`:
  - current shaping site before fix:
    - `internal/orchestrator/runtime_command_pipeline.go` always called `adaptArchiveWorkflowCommand(...)`, including assist-mode tasks.
  - live correlation before fix:
    - earlier ZIP runs exposed archive arg corruption and injected archive handling in assist-mode metadata steps.
- `decision`:
  - remove assist-mode archive input injection from the runtime mutation pipeline.
  - keep safe archive runtime env support for `john`, but do not let runtime choose missing ZIP inputs for assist-mode tasks.
- `evidence_after_fix`:
  - implementation:
    - `internal/orchestrator/runtime_command_pipeline.go`
    - `internal/orchestrator/worker_runtime_assist_exec.go`
  - tests:
    - `internal/orchestrator/runtime_command_pipeline_test.go:TestApplyRuntimeCommandPipelineDoesNotInjectArchiveInputForAssistTask`
    - `internal/orchestrator/worker_runtime_assist_builtin_test.go:TestApplyAssistExternalCommandEnvAddsArchiveRuntimeEnvForJohn`
  - validation:
    - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
    - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - live ZIP run: `run-s37-rtshape-zip-20260307-151500`
    - no `adapt_archive_workflow` stage was emitted.
    - remaining runtime mutation came from `repair_missing_inputs`, not archive injection.
    - run still regressed into evidence rereads (`read_file ...worker-T-01-a1-a1-s3-t3.log`, `read_file zipinfo_output.txt`) before operator stop.
  - live router run: `run-s37-rtshape-router-20260307-152000`
    - connectivity task completed cleanly with a worker-issued `complete` contract.
    - `T-02-port-enumeration` still emitted `runtime_adapt` via `prepare_runtime_command`, confirming runtime shaping overlap remains outside archive handling.
- `task_map`: Sprint 37 `Critical 5 — Closed-loop architecture reduction`
- `status`: `open`

## S37-D049
- `date_utc`: `2026-03-07`
- `area`: `runtime shaping / assist`
- `symptom`: after archive input injection removal, assist-mode still flowed through generic runtime shaping (`prepare_runtime_command`) plus weak vulnerability/report rewrites. That left runtime ownership over target injection and scan/report selection even when the LLM already emitted a concrete command.
- `evidence`:
  - shaping sites before fix:
    - `internal/orchestrator/runtime_command_pipeline.go`
    - `internal/orchestrator/runtime_command_prepare.go`
    - `internal/orchestrator/runtime_vulnerability_evidence.go`
    - `internal/orchestrator/worker_runtime_report.go`
  - pre-fix live correlation:
    - `run-s37-rtshape-router-20260307-152000` emitted `runtime_adapt` in `prepare_runtime_command` during `T-02-port-enumeration`
    - older assist tests depended on runtime target injection for `nmap`
- `decision`:
  - remove assist-mode generic command shaping from the runtime mutation pipeline.
  - allow assist-mode commands to fail closed under scope validation rather than being rewritten into an executable shape by runtime.
- `evidence_after_fix`:
  - implementation:
    - `internal/orchestrator/runtime_command_pipeline.go`
  - tests:
    - `internal/orchestrator/runtime_command_pipeline_test.go:TestApplyRuntimeCommandPipelineSkipsPrepareStageForAssistNmapTask`
    - `internal/orchestrator/runtime_command_pipeline_test.go:TestApplyRuntimeCommandPipelineSkipsWeakVulnerabilityRewriteForAssistTask`
    - `internal/orchestrator/runtime_command_pipeline_test.go:TestApplyRuntimeCommandPipelineSkipsWeakReportRewriteForAssistTask`
    - `internal/orchestrator/worker_runtime_assist_scope_sync_test.go:TestRunWorkerTaskAssistCommandScopeValidationDoesNotInjectTargetForAssistTask`
  - validation:
    - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
    - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - live ZIP run: `run-s37-rtshape2-zip-20260307-153200`
    - `T-01` executed the raw LLM-selected command with no `runtime_adapt` stage.
    - failure moved to adaptive replan / artifact truth (`read_file zip_metadata.txt` missing in recovery task), not runtime command shaping.
  - live router run: `run-s37-rtshape2-router-20260307-153600`
    - `T-01-port-scan` executed raw `bash -c "mkdir -p ... && nmap ..."` with no `runtime_adapt` stage.
    - remaining failure was degraded recovery after a primary assistant error, ending in `assist_loop_detected`.
- `task_map`: Sprint 37 `Critical 5 — Closed-loop architecture reduction`
- `status`: `open`

## S37-D050
- `date_utc`: `2026-03-07`
- `area`: `recovery / artifact truth`
- `symptom`: recovery and replan paths still lost exact artifact truth for shell-wrapper commands and could loop on fallback questions after a primary assistant failure. This made workers act on basename guesses (`zip_metadata.txt`) or repeated fallback questions instead of the canonical latest result.
- `evidence`:
  - pre-fix ZIP failure:
    - `run-s37-rtshape2-zip-20260307-153200`
    - adaptive replan task failed on `read_file zip_metadata.txt` from the wrong workspace path because recovery metadata only carried the basename.
  - pre-fix router failure:
    - `run-s37-rtshape2-router-20260307-153600`
    - degraded recovery looped on fallback questions after primary assistant failure and terminated with `assist_loop_detected`.
- `decision`:
  - make recover-mode fallback fail fast in non-interactive workers with canonical latest-result metadata.
  - infer shell-wrapper output refs (`>`, `>>`, `tee`) and shell input refs into `assistExecutionFeedback` so recovery/replans receive exact artifact truth without adding new planner/runtime owners.
- `evidence_after_fix`:
  - implementation:
    - `internal/orchestrator/worker_runtime_assist_loop.go`
    - `internal/orchestrator/worker_runtime_assist_loop_helpers.go`
    - `internal/orchestrator/worker_runtime_assist_feedback.go`
  - tests:
    - `internal/orchestrator/worker_runtime_assist_modes_test.go:TestRunWorkerTaskRecoverFallbackQuestionFailsFastWithLatestResult`
    - `internal/orchestrator/worker_runtime_assist_feedback_test.go:TestCaptureAssistExecutionFeedbackInfersShellOutputAndInputRefs`
    - `internal/orchestrator/coordinator_recovery_goal_test.go:TestBuildAdaptiveRecoveryGoalIncludesCommandArgsAndLog`
  - validation:
    - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
    - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - live ZIP run: `run-s37-recover-zip-20260307-154400`
    - bounded smoke stayed LLM-led during metadata recovery and showed no new regression from latest-result capture changes.
    - operator stop ended the run before a fresh adaptive replan failure could validate the new shell-output artifact truth end-to-end.
  - live router run: `run-s37-recover-router-20260307-154900`
    - bounded smoke stayed LLM-led during the initial port-scan step and showed no new regression from fail-fast recover fallback changes.
    - operator stop ended the run before a fresh primary-assistant recovery failure could validate the new fail-fast path end-to-end.
- `task_map`: Sprint 37 `Critical 5 — Closed-loop architecture reduction`
- `status`: `open`

## S37-D051
- `date_utc`: `2026-03-07`
- `area`: `adaptive replan / artifact truth`
- `symptom`: even after shell-wrapper output/input refs were captured, adaptive replan workers could still lose canonical artifact truth because the source-task evidence was only mentioned in prompt text and not exposed through the normal dependency artifact path. That left recovery free to act on basenames like `zip_metadata.txt` instead of the actual produced artifact path.
- `evidence`:
  - pre-fix ZIP run:
    - `run-s37-rtshape2-zip-20260307-153200`
    - adaptive recovery reopened `zip_metadata.txt` from the wrong workspace path because the replan task had no explicit dependency on the failed source task.
  - code-path evidence:
    - `buildReplanTask(...)` created adaptive recovery tasks without `DependsOn`
    - `runWorkerAssistTask(...)` only collects dependency artifact candidates from `task.DependsOn`
- `decision`:
  - make adaptive recovery tasks depend on the source task for evidence access,
  - treat those dependencies as evidence-ready when the source task is terminal non-success, so recovery can queue and consume canonical source artifacts without inventing a second handoff path.
- `evidence_after_fix`:
  - implementation:
    - `internal/orchestrator/coordinator_replan_helpers.go`
    - `internal/orchestrator/scheduler.go`
  - tests:
    - `internal/orchestrator/coordinator_replan_mutation_test.go:TestBuildReplanTask_AdaptiveRecoveryDependsOnSourceTask`
    - `internal/orchestrator/scheduler_test.go:TestScheduler_AdaptiveReplanEvidenceDependencyAllowsFailedSource`
  - validation:
    - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
- `live_followup`:
  - ZIP: `run-s37-arttruth-zipall-20260307-145040`
    - adaptive recovery tasks now explicitly depended on `T-01-metadata`, and second-order recovery depended on the first adaptive recovery task.
    - this confirms the source-task evidence dependency is flowing through the real scheduler/runtime path.
    - remaining blocker moved to malformed parse-repair / tool-install recovery (`type ✓`) plus execution-log churn inside adaptive recovery.
  - Router: `run-s37-arttruth-router-20260307-144314`
    - adaptive recovery task `task-rp-dcdb111cb1` explicitly depended on `TASK-001-DISCOVERY`.
    - the run report and plan now show that dependency directly, confirming the source evidence is no longer prompt-only.
    - remaining blocker stayed in LLM recovery behavior (`read_file` on worker log, then bounded stop during `TASK-002-PORTSCAN`), not dependency artifact handoff.
- `task_map`: Sprint 37 `Critical 5 — Closed-loop architecture reduction`
- `status`: `open`
