# Problem Register (Sprint 36 Salvage)

Date: 2026-02-26  
Status: Active

## Purpose

This register is the gate for salvage work.  
No behavior-changing fix should merge unless the problem has:
- a reproducible recipe,
- concrete evidence,
- component ownership,
- and a measurable acceptance signal.

## Severity Scale

- `S0` Critical: unsafe, out-of-scope risk, or false high-impact claim.
- `S1` High: repeated run failure or major trust break in outcomes.
- `S2` Medium: degraded autonomy/reliability without direct unsafe behavior.
- `S3` Low: operator friction or non-critical quality issue.

## Reproducibility Scale

- `deterministic`: reproduces reliably with the same seed/inputs.
- `intermittent`: reproduces in a meaningful fraction of runs.
- `unknown`: seen, but not yet reproducible on demand.

## Active Problems

| ID | Title | Sev | Repro | Status | Owner | Components |
|---|---|---|---|---|---|---|
| PR-001 | Blind repeat loops in assist execution | S1 | intermittent | investigating | orchestrator-runtime | assist-loop, execution-policy |
| PR-002 | LLM planner parse/truncation instability | S1 | deterministic | investigating | planner | llm-planner, planner-diagnostics |
| PR-003 | Relative local artifact path misclassified as out-of-scope | S1 | deterministic | open | scope-engine | scope-validation, command-parser |
| PR-004 | Run terminal `completed` can diverge from goal truth | S1 | deterministic | open | orchestrator-runtime | completion-contract, runtime-terminalization |
| PR-005 | Report can claim success without verifier-backed evidence | S0 | deterministic | open | reporter/verifier | report-synthesis, evidence-gating |
| PR-006 | Approval UX lacks actionable context and can spam approvals | S2 | deterministic | open | orchestrator-ux | approvals, tui/cli-notify |
| PR-007 | Context compression/attempt reset may cause memory loss across turns | S1 | deterministic (code-path) | accepted | orchestrator-runtime | assist-context, observation-memory |
| PR-008 | Parallel-agent shared memory ownership is under-specified | S2 | deterministic (design gap) | accepted | orchestrator-architecture | memory-bank, verifier-promotion, worker-contract |
| PR-009 | False `scope_denied` on adapted nmap command (target inference mismatch) | S1 | deterministic | in_progress | scope-engine + worker-runtime | scope-validation, command-adaptation, execution-path |
| PR-010 | `assist_no_new_evidence` can terminalize non-summary recon tasks as completed | S1 | deterministic | in_progress | orchestrator-runtime + completion-contract | assist-loop, completion-gate, scheduler-replan |
| PR-011 | CLI assist loop emits non-executable command shapes and can report completion without objective truth | S1 | deterministic | in_progress | cli-runtime + assist-loop | internal/cli, assist-suggestion-contract, report-gate |

## Problem Details

### PR-001 - Blind repeat loops in assist execution

- Symptom: worker repeats near-identical actions without new evidence and stalls.
- Evidence:
  - repeated helper-script execution in one task before loop block:
    - `sessions/benchmark-20260222-185350-recovery_stress_repeat_pressure-r02/orchestrator/event/event.jsonl:24`
    - `sessions/benchmark-20260222-185350-recovery_stress_repeat_pressure-r02/orchestrator/event/event.jsonl:27`
    - `sessions/benchmark-20260222-185350-recovery_stress_repeat_pressure-r02/orchestrator/event/event.jsonl:30`
    - `sessions/benchmark-20260222-185350-recovery_stress_repeat_pressure-r02/orchestrator/event/event.jsonl:34`
    - `sessions/benchmark-20260222-185350-recovery_stress_repeat_pressure-r02/orchestrator/event/event.jsonl:37`
  - operator-observed repeated `ls -la /tmp` behavior during zip task runs.
- Repro recipe:
  1. `./birdhackbot-orchestrator run --sessions-dir sessions --run run-zip-loop-repro --goal "Extract contents of secret.zip and identify the password needed to decrypt it. Produce a concise local report with method, result, and evidence paths." --scope-local --constraint local_only --planner auto --approval-timeout 2m --worker-cmd ./birdhackbot --worker-arg worker`
  2. Inspect event stream for repeated same-intent actions with no new artifacts.
- Acceptance signal:
  - same-intent streak must force a strategy-class pivot within bounded attempts.
  - no terminal success if only repeated/no-new-evidence steps occurred.

### PR-002 - LLM planner parse/truncation instability

- Symptom: planner exhausts retries with JSON parse failures.
- Evidence:
  - six failed planner attempts in one run:
    - `sessions/planner-attempts/run-zip-auto-20260224-221310/attempts.json`
  - repeated error: `unexpected end of JSON input`.
- Repro recipe:
  1. Run any zip goal with `--planner auto` against the same LLM endpoint.
  2. Check `sessions/planner-attempts/<run-id>/attempts.json`.
- Acceptance signal:
  - planner succeeds within bounded retries on salvage smoke matrix.
  - failures are classified (truncation vs schema vs transport) and surfaced clearly.

### PR-003 - Relative local artifact path misclassified as out-of-scope

- Symptom: local workflow task fails with `scope_denied` for relative file artifacts.
- Evidence:
  - `scope violation: out of scope target zip.hash`:
    - `sessions/run-zip-reg3-20260225-201808-2/orchestrator/event/event.jsonl:70`
    - `sessions/run-zip-reg3-20260225-201808-2/orchestrator/event/event.jsonl:78`
- Repro recipe:
  1. Re-run zip local workflow with `--scope-local`.
  2. Include command actions that output/read `zip.hash` as relative artifact path.
- Acceptance signal:
  - relative artifact paths generated within task working directory are treated as local artifacts, not network targets.

### PR-009 - False `scope_denied` on adapted nmap command

- Symptom: worker emits `scope_denied` (`could not infer target from network command "nmap"`) even though runtime logs show target auto-injection/adaptation to `127.0.0.1`.
- Evidence:
  - adaptation + auto-injection logged, then immediate scope-denied failure:
    - `sessions/benchmark-20260227-154821-host_discovery_inventory-r01/orchestrator/event/event.jsonl:10`
    - `sessions/benchmark-20260227-154821-host_discovery_inventory-r01/orchestrator/event/event.jsonl:11`
    - `sessions/benchmark-20260227-154821-host_discovery_inventory-r01/orchestrator/event/event.jsonl:13`
  - repeated across salvage smoke runs:
    - `benchmark-20260227-131322`
    - `benchmark-20260227-134820`
    - `benchmark-20260227-140459`
    - `benchmark-20260227-142312`
    - `benchmark-20260227-154821`
- Repro recipe:
  1. `./birdhackbot-orchestrator benchmark --sessions-dir sessions --worker-cmd ./birdhackbot --worker-arg worker --repeat 1 --seed 42 --scenario host_discovery_inventory --approval-timeout 2m`
  2. Inspect `event.jsonl` for `task_progress` showing target injection followed by `task_failed reason=scope_denied`.
- Acceptance signal:
  - `host_discovery_inventory` smoke no longer fails with `scope_denied` in the target-injection path.
  - scope validation and execution use the same final command/args tuple (no stale pre-adaptation args in failure payload).
  - fail-closed behavior remains intact for truly missing or out-of-scope targets.

### PR-010 - `assist_no_new_evidence` terminal success leakage on recon tasks

- Symptom: non-summary recon task reaches `task_completed` with `reason=assist_no_new_evidence` after repeated identical command results, instead of being forced into no-progress replan/failure handling.
- Evidence:
  - completion/finding generated from bounded fallback in recon seed task:
    - `sessions/benchmark-20260301-132014-host_discovery_inventory-r01/orchestrator/event/event.jsonl`
      - `task_progress` with `bounded_fallback:"no_new_evidence"`
      - `task_completed` with `reason:"assist_no_new_evidence"` for `task-recon-seed`
- Repro recipe:
  1. Run bounded smoke on `host_discovery_inventory` with current fallback behavior.
  2. Confirm non-summary recon task emits `task_completed reason=assist_no_new_evidence`.
- Acceptance signal:
  - non-summary recon/validation tasks never terminalize as `completed` via `assist_no_new_evidence`.
  - no-progress fallback routes to explicit no-progress classification and bounded replan/failure path.
  - completion-contract required findings cannot be satisfied by synthetic fallback-only findings on evidence-producing tasks.

### PR-011 - CLI closed-loop gap causes prose-command execution failures and weak truth gating

- Symptom: CLI assist emits prose/compound command strings that fail at exec boundary, then still synthesizes reports without objective-level success proof.
- Evidence:
  - `secret.zip` CLI run executes invalid prose/compound commands:
    - `sessions/20260302-181014-de1c/logs/cmd-20260302-181033.187880016.log`
    - `sessions/20260302-181014-de1c/logs/cmd-20260302-181042.702891651.log`
    - `sessions/20260302-181014-de1c/logs/cmd-20260302-181044.897014918.log`
  - router CLI run executes invalid command tokens (`nmap -sn` as one binary token, `"1."` prose command):
    - `sessions/20260302-181329-d8bb/logs/cmd-20260302-181333.955942200.log`
    - `sessions/20260302-181329-d8bb/logs/cmd-20260302-181338.873317869.log`
  - router CLI run generated report without meaningful execution evidence:
    - `sessions/20260302-181356-e510/report.md`
- Repro recipe:
  1. `./birdhackbot -config /tmp/bhb-qwen35.json -permissions all`
  2. Goal A: `Extract contents of ./secret.zip and identify the password...`
  3. Goal B: `Scan my authorized lab router at 192.168.50.1...`
  4. Inspect session logs/reports for prose-as-command exec failures and objective/report truth mismatch.
- Acceptance signal:
  - CLI uses explicit per-step loop contract: `execute -> observe -> interpret -> decide -> memory_update`.
  - decision outputs constrained to `retry_modified|pivot_strategy|ask_user|step_complete`.
  - `step_complete` requires `objective_met` + evidence refs; missing evidence blocks completion/report success.
  - live qwen3.5 validation gate passes:
    - `secret.zip` success `>=5/5` with zero prose-command exec failures.
    - router smoke reports evidence-backed findings or explicit `objective_not_met` (no false-success synthesis).

### PR-004 - Run terminal `completed` can diverge from goal truth

- Symptom: run can end as `completed` even when requested outcome is not proven.
- Evidence:
  - run marked completed:
    - `sessions/run-zip-regression-20260225-223809/orchestrator/report.md`
  - cracking evidence shows zero recovered hashes:
    - `sessions/run-zip-regression-20260225-223809/orchestrator/artifact/T-004/john_show.txt`
    - `sessions/run-zip-regression-20260225-223809/orchestrator/artifact/T-005/john_show.txt`
- Repro recipe:
  1. Execute zip goal run with standard flags and inspect final report state and verifier evidence.
- Acceptance signal:
  - `completed` requires success predicates for goal outcomes, not only task completion.

### PR-005 - Report claim truth gap

- Symptom: report claims password recovery although evidence says none recovered.
- Evidence:
  - claim text ("Password recovered..."):
    - `sessions/run-zip-regression-20260225-223809/orchestrator/artifact/T-008/report.md`
  - contradictory evidence (`0 password hashes cracked`):
    - `sessions/run-zip-regression-20260225-223809/orchestrator/artifact/T-004/john_show.txt`
    - `sessions/run-zip-regression-20260225-223809/orchestrator/artifact/T-005/john_show.txt`
- Repro recipe:
  1. Run zip workflow where cracking strategies do not recover password.
  2. Compare synthesized report claim vs verifier-backed artifacts.
- Acceptance signal:
  - report findings only include verifier-backed claims.
  - contradictory evidence downgrades claim to `UNVERIFIED` or removes it.

### PR-006 - Approval UX ambiguity and approval spam

- Symptom: approval prompt does not clearly state what is being approved and can create noisy repeated approvals.
- Evidence:
  - generic approval reason only:
    - `sessions/benchmark-20260222-185350-host_discovery_inventory-r01/orchestrator/event/event.jsonl:63`
  - payload uses `reason:"requires approval"` and tier without action-level detail.
- Repro recipe:
  1. Start a run that triggers `active_probe` approvals.
  2. Observe approval messages and operator action burden.
- Acceptance signal:
  - each approval line includes actionable command summary, target, risk tier, and reason.
  - no duplicate spam for equivalent pending approvals.

### PR-007 - Context compression and attempt reset risk

- Symptom: assist context may forget relevant prior observations and repeat low-value actions.
- Evidence (code-path):
  - observation history capped to 12:
    - `internal/orchestrator/worker_runtime_assist.go:15`
  - observations initialized per assist loop attempt:
    - `internal/orchestrator/worker_runtime_assist_loop.go:45`
  - command output summary heavily truncated:
    - `internal/orchestrator/worker_runtime_assist_exec.go:360`
  - observation trimming to latest bounded tail:
    - `internal/orchestrator/worker_runtime_assist_exec.go:382`
- Repro recipe:
  1. Run tasks with high-volume directory or scan output.
  2. Check whether crucial file indicators disappear from subsequent decisions.
- Acceptance signal:
  - repeated attempts preserve critical prior facts (or emit explicit memory handoff).
  - no blind repeats caused by dropped high-signal observations.
  - assist input uses layered context (facts/actions/artifacts/unknowns) instead of a single rolling text blob.
  - context diagnostics artifact proves truncation/compaction behavior is bounded and observable.

### PR-008 - Parallel-agent shared memory ownership gap

- Symptom: unclear ownership of shared memory can lead to design drift, race-prone updates, and non-auditable truth promotion.
- Evidence:
  - current run memory is centralized, but worker read/write contract is not explicitly enforced as a non-negotiable architectural rule.
  - no strict requirement yet that promoted shared-memory facts carry verifier/event provenance.
- Repro recipe:
  1. Review orchestrator/worker context pathways for direct shared-memory mutation opportunities.
  2. Confirm whether all shared truth updates can be traced to event IDs + verifier outcomes.
- Acceptance signal:
  - orchestrator is the only shared-memory writer.
  - workers remain append-only proposers via artifacts/events/findings.
  - promoted facts include explicit provenance (event IDs + verifier decision state).

## Checkpoint Log

### 2026-03-02 - PR-011 slice 1: CLI command-shape contract + low-value anti-loop guard

- Scope:
  - added CLI command contract normalization/validation:
    - `internal/cli/assist_command_contract.go`
    - rejects non-executable prose commands before `/run` dispatch.
    - normalizes inline command strings (`"which zip2john"`) into `command + args`.
    - requires explicit `bash/sh -c` wrapper for shell operators.
  - wired command contract in assist execution path:
    - `internal/cli/cli.go` (`executeAssistSuggestion` command branch).
  - strengthened low-value progress tracking:
    - `internal/cli/assist_budget.go` now treats `list_dir`/`read_file` as non-progress when no new evidence delta exists.
  - added/updated deterministic tests:
    - `internal/cli/assist_command_contract_test.go`
    - `internal/cli/assist_budget_test.go`
- Validation:
  - deterministic:
    - `go test ./internal/cli`
    - `go build -buildvcs=false ./cmd/birdhackbot`
  - live qwen3.5 CLI smoke:
    - `sessions/20260302-213745-ba87` (`secret.zip`) confirms prose fallback command is rejected by contract (`non-executable prose command rejected`) instead of executing `exec: "1."`.
    - `sessions/20260302-213920-3ebf` (router) confirms executable command-shape behavior (`nmap -sn -v 192.168.50.1`, `nmap -sV ...`) without previous `exec: "nmap -sn"` failure mode.
    - `sessions/20260302-214344-5a5b` (`secret.zip`) confirms contract guard continues to block prose-command execution under longer assist run.

### 2026-03-02 - PR-011 slice 2: explicit decision contract + completion payload enforcement

- Scope:
  - extended assistant suggestion contract with `decision` field and prompt/schema requirements:
    - `internal/assist/assistant.go`
    - `internal/assist/assistant_parse.go`
    - `internal/assist/assistant_normalize.go`
    - `internal/assist/assistant_prompt.go`
    - `internal/assist/assistant_fallback.go`
  - added CLI runtime decision validator for action goals:
    - `internal/cli/assist_decision_contract.go`
    - enforced in `internal/cli/cli.go` before step execution (`command|tool|question|complete`).
  - updated deterministic fixtures/tests to include explicit decision-state outputs:
    - `internal/cli/assist_decision_contract_test.go`
    - `internal/cli/agent_loop_test.go`
- Validation:
  - deterministic:
    - `go test ./internal/assist ./internal/cli`
    - `go build -buildvcs=false ./cmd/birdhackbot`
  - live qwen3.5 CLI smoke:
    - `sessions/20260302-222928-1f73` (`secret.zip`, config `/tmp/config-qwen35.json`).
    - result: no prose-command execution regression, but run still failed objective truth gate (budget exhausted; no password/decrypt proof).
    - observed residual gaps:
      - mixed/invalid decision emissions from model responses still trigger repeated contract violations.
      - report path handling bug remains (`output=...` interpreted as literal path prefix).
      - completion/report quality still weak (contains reasoning text and no verified objective closure).

### 2026-03-02 - PR-011 slice 3: open-like CLI loop mode + immediate repair retry

- Scope:
  - added flag-gated open-like mode for CLI assist loop:
    - config field `agent.assist_loop_mode` (`strict|open_like`) in `internal/config/config.go` and `config/default.json` (default `strict`).
    - mode helper in `internal/cli/assist_loop_mode.go`.
  - runtime behavior in `open_like` mode:
    - relax per-step decision enforcement for `command|tool|question`.
    - keep `type=complete` decision/evidence contract strict.
    - before broad recovery, attempt one immediate LLM-guided repair action for failed `command|tool` steps.
  - implementation touchpoints:
    - `internal/cli/assist_decision_contract.go`
    - `internal/cli/cli.go`
    - `internal/cli/assist_recovery.go`
- Validation:
  - deterministic:
    - `go test ./internal/cli ./internal/assist ./internal/config`
    - `go build -buildvcs=false ./cmd/birdhackbot`
    - added regression: `TestAssistOpenLikeLoopUsesImmediateRepairBeforeBroadRecovery`.
  - live qwen3.5 CLI smoke (`open_like`):
    - session: `sessions/20260302-224005-0576` (`secret.zip`).
    - observed:
      - immediate repair path executed and corrected a failed step to a runnable command.
      - run still fails acceptance due to model quality issues (plain-text command leakage, reasoning-text leakage in report summary, objective not met).

### 2026-03-02 - PR-011 slice 4: open-like persistence hardening + report/output sanitation

- Scope:
  - strengthened open-like runtime persistence/repair behavior:
    - configurable `agent.assist_repair_attempts` (default `1`; open-like fallback default `3`) in `internal/config/config.go`.
    - bounded immediate-repair retries before broad recovery (`internal/cli/assist_recovery.go`).
    - open-like long-horizon budget extension until hard cap (`internal/cli/assist_budget.go`, `internal/cli/cli.go`).
  - objective truth enforcement:
    - open-like skips final report synthesis when action objective remains unmet (`internal/cli/reporting.go`).
    - completion path now marks objective state from contract payload (`internal/cli/cli.go`).
  - command/report/output hardening:
    - reject additional narrative prose command patterns (`internal/cli/assist_command_contract.go`).
    - reject interactive `write_file` command shape in assist mode (requires path+content arguments).
    - parse report output flags (`--output=...`, `--output ...`, `output=...`) via `parseReportArgs` (`internal/cli/reporting.go`).
    - strip reasoning-tail leakage (`Thinking Process`/`Reasoning`) from user-visible assistant text (`internal/cli/assist_text.go`).
    - tighten plain-text command fallback to reject `type:` prefixed prose (`internal/assist/assistant_json.go`).
- Validation:
  - deterministic:
    - `go test ./internal/assist ./internal/cli ./internal/config`
    - `go build -buildvcs=false ./cmd/birdhackbot`
  - live qwen3.5 CLI smoke (`open_like`):
    - `sessions/20260302-224934-af1c` (`secret.zip`) shows:
      - immediate repair retries correcting failing command variants (e.g., invalid `--show`/`--remove` combinations),
      - continued bounded exploration to cap (step `30/30`) with explicit `Objective not met yet...` status instead of false terminal success.
    - `sessions/20260302-225740-e38a` (`secret.zip`) shows residual model/output faults under open-like flow:
      - repeated malformed tool/script path generation and prose fallback churn still occur,
      - objective remains unmet; additional controller hardening still required for acceptance gate.

### 2026-03-03 - PR-011 slice 5: tool path canonicalization + run-path repair

- Scope:
  - canonicalized tool-forge file paths into session tools root and rejected unmapped absolute file paths:
    - `internal/cli/tool_forge.go`
    - `normalizeToolFilePath`, `mapToolPathToToolsRoot`
  - added run-path repair so absolute/session-prefixed script paths are remapped to actual files under `artifacts/tools` before execution:
    - `internal/cli/tool_forge.go`
    - `remapAbsoluteSessionPathToToolsRoot`, `normalizeToolRun` updates
  - strengthened tool recovery guidance to require paths under `artifacts/tools` (no `sessions/<id>/scripts/...` run targets).
- Validation:
  - deterministic:
    - `go test ./internal/assist ./internal/cli ./internal/config`
    - `go build -buildvcs=false ./cmd/birdhackbot`
    - added tests:
      - `TestToolForgeRepairsSessionPrefixedFileAndRunPaths`
      - `TestToolForgeRejectsAbsoluteUnmappedFilePath`
      - `TestMapToolPathToToolsRoot`
  - live qwen3.5 smoke (`open_like`):
    - `sessions/20260302-231623-57bb` (`secret.zip`) reached step 23 without prior `.../sessions/<id>/scripts/*.sh: No such file or directory` failure mode; remaining failures were strategy/model-quality related (wordlist/tool choice churn), not the previous path-canonicalization regression.

### 2026-02-27 - Phase 7 slice 7.1: canonical state contract lock (docs-only)

- Scope:
  - locked cleanup target model contract in:
    - `docs/runbooks/state-inventory.md`
  - added explicit authoritative-vs-derived ownership rules:
    - authoritative: `TaskState`, `RunPhase`, `RunOutcome` (planned), `ApprovalStatus`, `FindingState`
    - derived: `LeaseStatus`, `RunStatus.State`, `WorkerStatus.State`
  - added semantic glossary and migration guardrails to prevent state/status drift.
  - updated sprint tracking:
    - `TASKS.md` marks slice `7.1` complete.
- Validation:
  - `go test ./internal/orchestrator -run TestStateInventory -count=1`
  - smoke run (local static planner):
    - `./birdhackbot-orchestrator run --sessions-dir sessions --run run-s36-s7-1-docs-smoke --goal "Inspect local /tmp and produce a concise report." --scope-local --constraint local_only --planner static --approval-timeout 2m --worker-cmd ./birdhackbot --worker-arg worker`
    - outcome: run reached terminal state `stopped` and produced report artifact; failure remained on `task-h01` (`command_failed`) with no new regressions attributed to this docs-only slice.

### 2026-02-27 - Phase 7 slice 7.2: transition/invariant guard tests (tests-only)

- Scope:
  - expanded test-only guardrail coverage before runtime refactor slices:
    - `internal/orchestrator/state_inventory_test.go`
      - terminal task states must have no outgoing transitions.
      - `LeaseStatus` must mirror `TaskState` domain (projection contract).
      - approval decision and approval status domains must remain disjoint.
      - every worker failure reason must map to a valid failure class.
    - `internal/orchestrator/worker_runtime_assist_context_diag_test.go`
      - added `TestRunWorkerTaskAssistContextEnvelopeCapturesCarryoverAndDelta` to validate actual attempt-to-attempt context carryover content and attempt-delta event/artifact emission.
- Validation:
  - `go test ./internal/orchestrator -run TestStateInventory -count=1`
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssistContextEnvelopeCapturesCarryoverAndDelta|TestWorkerAssistContextEnvelope' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator`

### 2026-02-27 - Phase 7 slice 7.3: run outcome type introduction (no behavior switch)

- Scope:
  - introduced canonical run-outcome enum and parse/validate helpers:
    - `internal/orchestrator/run_outcome.go`
    - values: `success|failed|aborted|partial`
  - added type/unit coverage:
    - `internal/orchestrator/run_outcome_test.go`
  - extended inventory guards:
    - `internal/orchestrator/state_inventory_test.go` validates run-outcome domain.
  - updated inventory docs:
    - `docs/runbooks/state-inventory.md` now includes `Run Outcome` domain and deferred wiring note for slice 7.4.
- Validation:
  - `go test ./internal/orchestrator -run 'TestNormalizeRunOutcome|TestValidateAndParseRunOutcome|TestStateInventoryRunOutcomesValidate|TestStateInventory' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator`

### 2026-02-27 - Phase 7 slice 7.4: run-outcome wiring (compatible terminal payloads)

- Scope:
  - wired `RunOutcome` into runtime terminalization while keeping legacy semantics:
    - terminal events now include `payload.run_outcome` on `run_completed` / `run_stopped`.
    - run plan metadata now persists `metadata.run_outcome` via manager setter.
    - legacy `detail` payload and exit-status behavior remain unchanged.
  - added manager API + validation:
    - `internal/orchestrator/run.go` `SetRunOutcome`.
    - `internal/orchestrator/validate.go` validates optional `metadata.run_outcome`.
  - added/updated coverage:
    - `internal/orchestrator/run_test.go` (`TestManagerSetRunOutcome`)
    - `cmd/birdhackbot-orchestrator/main_test.go` terminal event + metadata assertions
    - `cmd/birdhackbot-orchestrator/main_runtime_truth_gate_test.go` updated to canonical outcome enum.
- Validation:
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator`
  - `go build -buildvcs=false -o birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - smoke run:
    - `run-s36-s7-4-smoke2` (static planner) produced terminal stop with report.
    - verified:
      - `sessions/run-s36-s7-4-smoke2/orchestrator/plan/plan.json` has `metadata.run_outcome=failed`
      - terminal `run_stopped` event payload includes `run_outcome=failed`.

### 2026-02-27 - Phase 7 slice 7.5: lease ownership collapse (TaskState authoritative)

- Scope:
  - implemented canonical lease/task mapping in:
    - `internal/orchestrator/lease.go`
      - `LeaseStatusFromTaskState`
      - `TaskStateFromLeaseStatus`
      - `UpdateLeaseFromTaskState`
  - tightened lease validation:
    - `internal/orchestrator/validate.go` now validates lease status via mapping contract.
  - removed independent coordinator lease lifecycle writes:
    - `internal/orchestrator/coordinator.go`
      - added `syncLeaseWithSchedulerState` and replaced direct `UpdateLeaseStatus` transitions with scheduler-derived sync.
      - dispatch/approval/timeout/budget/stop flows now persist lease status from `TaskState`.
    - `internal/orchestrator/coordinator_replan.go`
      - queued replan lease status now derived from `TaskStateQueued`.
      - blocked-task detection now uses scheduler task states as authoritative source.
  - added mapping/derivation regression tests:
    - `internal/orchestrator/lease_test.go`
      - `TestLeaseStatusTaskStateRoundTrip`
      - `TestUpdateLeaseFromTaskState`
- Validation:
  - `go test ./internal/orchestrator -run 'TestLeaseStatusTaskStateRoundTrip|TestUpdateLeaseFromTaskState|TestReclaim|TestCoordinator' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke run:
    - `run-s36-s7-5-smoke` (static planner) reached terminal stop with report artifact.
    - persisted leases reflected scheduler-derived terminal states (`task-recon-seed=completed`, `task-h01=failed`).

### 2026-02-27 - Phase 4 slice: telemetry completion + failure classification

- Scope:
  - completed remaining Phase 4 telemetry items:
    - observation append/eviction/token-compaction counters and compaction-summary detection in assist context envelopes.
    - retry carryover/reset instrumentation (`prior_attempt`, carryover counts, reset signals).
    - repeated action/result fingerprint telemetry in assist context envelopes.
  - added per-attempt change summaries:
    - assist runtime now emits `attempt_delta_summary` progress events when prior attempt context exists.
    - `context_envelope` now persists attempt-delta summaries.
  - added worker failure classification:
    - `task_failed` and `task_execution_failure` finding metadata now include `failure_class` (`context_loss|strategy_failure|contract_failure`).
  - added opt-in live LLM coverage:
    - `internal/assist/assistant_live_test.go` (`BIRDHACKBOT_LIVE_LLM_TEST=1`) for real LMStudio Suggest integration.
- Validation:
  - `go test ./internal/orchestrator -run 'TestWorkerAssistContextEnvelope|TestClassifyWorkerFailureReason|TestEmitWorkerFailureIncludesFailureClass' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator`
  - `go test ./internal/assist -run TestLLMAssistantSuggestLiveLMStudio -count=1 -v` (skipped without opt-in env)
  - `go build -buildvcs=false -o birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - live smoke with LLM planner:
    - `run-phase4-llm-smoke2` executed (`--planner auto`) and produced report; run failed on task `T-02`, verifying failure classification payloads (`reason=command_failed`, `failure_class=strategy_failure`) in event stream.

### 2026-02-27 - Phase 7 slice: state/status inventory guardrail

- Scope:
  - added canonical state/status inventory runbook:
    - `docs/runbooks/state-inventory.md`
  - added orchestrator enum/state guard tests:
    - `internal/orchestrator/state_inventory_test.go`
  - updated Sprint 36 Phase 7 tracking to include this cleanup slice:
    - `TASKS.md`
- Validation:
  - `go test ./internal/orchestrator -run TestStateInventory -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`

### 2026-02-26 - Phase 1 slice: diagnostic run mode

- Scope:
  - added orchestrator diagnostic flag plumbing:
    - `birdhackbot-orchestrator run --diagnostic`
    - `birdhackbot-orchestrator benchmark --diagnostic` (forwarded to each scenario run).
  - diagnostic worker contract:
    - `BIRDHACKBOT_ORCH_DIAGNOSTIC_MODE=true` propagated to workers.
    - adaptive runtime rewrites disabled in diagnostic mode (assist/non-assist target auto-injection, runtime command adaptation, runtime missing-input repair).
    - assist diagnostics default to strict + trace unless caller already set worker env:
      - `BIRDHACKBOT_WORKER_ASSIST_MODE=strict`
      - `BIRDHACKBOT_WORKER_ASSIST_TRACE_LLM=true`
- Validation:
  - `go test ./internal/orchestrator -run 'TestParseWorkerRunConfig|TestRunWorkerTaskAssistDiagnosticModeSkipsShellRewrite|TestRunWorkerTaskAssistTraceLLMResponsesWritesArtifacts|TestRunWorkerTaskAssistDegradedModeEmitsFallbackMetadata'`
  - `go test ./cmd/birdhackbot-orchestrator -run 'TestAppendEnvIfMissing|TestBenchmarkScenarioRunArgsIncludesDiagnosticFlag'`
  - `go test ./cmd/birdhackbot-orchestrator ./internal/orchestrator`

### 2026-02-26 - Phase 1 slice: run-id/artifact checklist + quota cadence

- Scope:
  - standardized salvage experiment run IDs and artifact checklist in:
    - `docs/runbooks/salvage-experiment-ops.md`
  - standardized quota-aware validation cadence:
    - per-change smoke, milestone quick gate, periodic/manual full gate.
  - added reusable command templates for:
    - focused smoke run,
    - diagnostic trace run,
    - quick benchmark slice.
- Validation:
  - documentation update only; no runtime behavior change.

### 2026-02-26 - Phase 1 slice: sprint freeze policy

- Scope:
  - codified Sprint 36 freeze policy in:
    - `docs/runbooks/salvage-experiment-ops.md`
  - policy explicitly constrains active work to Sprint 36 salvage phases; Sprint 37+ remains provisional until Sprint 36 exit criteria pass.
- Validation:
  - documentation update only; no runtime behavior change.

### 2026-02-26 - Phase 3 slice: deterministic repro harness + quick/full gate packs

- Scope:
  - added repro matrix for top failures:
    - `docs/runbooks/sprint36-repro-scenarios.json`
  - added deterministic approval-stall plan template:
    - `docs/runbooks/repro/approval-stall-plan.template.json`
  - added quick-gate artifacts:
    - scenario pack: `docs/runbooks/sprint36-quick-gate-scenarios.json`
    - runner: `scripts/run_sprint36_quick_gate.sh`
    - threshold checker: `scripts/check_benchmark_gate.py`
  - added full-gate artifacts (manual/periodic):
    - scenario pack: `docs/runbooks/sprint36-full-gate-scenarios.json`
    - runner: `scripts/run_sprint36_full_gate.sh`
  - updated benchmark program runbook with canonical quick/full gate references:
    - `docs/runbooks/autonomy-benchmark-program.md`
- Validation:
  - `python3 -m json.tool docs/runbooks/sprint36-repro-scenarios.json`
  - `python3 -m json.tool docs/runbooks/sprint36-quick-gate-scenarios.json`
  - `python3 -m json.tool docs/runbooks/sprint36-full-gate-scenarios.json`
  - `python3 -m py_compile scripts/check_benchmark_gate.py`

### 2026-02-26 - Phase 3 quick-gate execution snapshot

- Commands:
  - `./scripts/run_sprint36_quick_gate.sh`
  - fallback per-scenario run for second case after first-case hard fail:
    - `./birdhackbot-orchestrator benchmark --sessions-dir sessions --scenario-pack docs/runbooks/sprint36-quick-gate-scenarios.json --scenario evidence_first_reporting_quality --benchmark-id benchmark-s36-quick-evidence_first_reporting_quality-20260226-175500-s17 --repeat 1 --seed 17 --planner auto --approval-timeout 2m --worker-cmd ./birdhackbot --worker-arg worker`
- Outcomes:
  - `benchmark-s36-quick-recovery_stress_repeat_pressure-20260226-174655-s11` -> **failed**
    - run: `benchmark-s36-quick-recovery_stress_repeat_pressure-20260226-174655-s11-recovery_stress_repeat_pressure-r01`
    - metrics: `task_success_rate=0.2857`, `loop_incident_rate=0.2857`, failures include `assist_loop_detected` and `command_failed`.
  - `benchmark-s36-quick-evidence_first_reporting_quality-20260226-175500-s17` -> **failed**
    - run: `benchmark-s36-quick-evidence_first_reporting_quality-20260226-175500-s17-evidence_first_reporting_quality-r01`
    - metrics: `task_success_rate=0.6667`, `loop_incident_rate=0`, but quick-gate checker failed due `task_completed.reason=assist_no_new_evidence`.
- Gate status:
  - quick-gate checker did not pass; blockers remain open for Sprint 36 Phase 4/5 contract hardening.

### 2026-02-26 - Phase 4 slice: layered assist context payload

- Scope:
  - added layered assist payload (`facts`, `recent_actions`, `recent_artifacts`, `open_unknowns`) for orchestrator assist turns.
  - kept existing behavior path as fallback when layered fields are empty.
- Validation:
  - targeted tests passed:
    - `go test ./internal/orchestrator -run 'TestBuildWorkerAssistLayeredContextReadsMemoryAndArtifacts|TestReadMarkdownBulletsIgnoresNonBullets|TestNormalizePathAnchorToken|TestSummarizeOutputWithMetaTracksTruncation|TestWriteWorkerAssistContextEnvelope|TestLoadPreviousWorkerAssistContextEnvelope'`
  - quick-gate smoke executed (LLM active):
    - `benchmark-20260226-104341` (`recovery_stress_repeat_pressure`, `repeat=1`, `seed=42`) -> failed
    - dominant failure remains `missing_required_artifacts=2` (existing issue), not a new loop-safety regression.
- Evidence:
  - layered/context diagnostics artifacts present:
    - `sessions/benchmark-20260226-104341-recovery_stress_repeat_pressure-r01/orchestrator/artifact/task-recon-seed/context_envelope.a1.json`
    - `sessions/benchmark-20260226-104341-recovery_stress_repeat_pressure-r01/orchestrator/artifact/task-recon-seed/context_envelope.a2.json`

### 2026-02-26 - Phase 4 slice: oversized-context smoke

- Scope:
  - added deterministic stress regression for oversized worker context compaction:
    - `internal/orchestrator/worker_runtime_assist_context_layered_test.go` (`TestBuildWorkerAssistLayeredContextStressCompaction`).
- Validation:
  - `go test ./internal/orchestrator -run 'TestBuildWorkerAssistLayeredContextStressCompaction' -v`
  - observed bounded context after extreme inputs:
    - `knownFacts=20` (retained earliest, dropped tail like `fact-199`)
    - `openUnknowns=12` (retained earliest, dropped tail like `unknown-119`)
    - `recentActions=8` (retained latest tail `obs-292..obs-299`, dropped oldest)
    - `inventoryLines=10` (artifact list capped)
    - payload sizes remained bounded (`chatBytes=4620`, `recentBytes=1442`)

### 2026-02-26 - Phase 4 slice: compaction retention policy fix

- Scope:
  - changed compaction retention policy to preserve freshness while pinning critical anchors:
    - always retain known-fact anchors (`Goal`, `Planner decision`).
    - retain newest dynamic known facts, open unknowns, recent actions, and recent artifacts within caps.
  - added explicit prompt-visible compaction notes:
    - append `[compaction_summary]` section to assist context with retained/total/dropped counts and policy.
    - include memory-bank compaction notes from `memory/context.json` so historical drops remain visible after persisted compaction.
- Validation:
  - `go test ./internal/orchestrator -run 'TestCompactKnownFactsEntriesPreservesAnchorsAndLatestDynamic|TestCompactTailEntriesKeepsNewest|TestBuildWorkerAssistLayeredContextIncludesMemoryCompactionSummary|TestBuildWorkerAssistLayeredContextStressCompaction|TestRefreshMemoryBankCompactsLargeFindingSet'`

### 2026-02-26 - Phase 4 slice: token-aware observation compaction

- Scope:
  - increased retained observation floor from `12` to `20` entries.
  - added token-budgeted observation compaction (`workerAssistObsTokenBudget=1800`) with signal-priority retention:
    - prioritize entries carrying file paths, targets, and error signals.
    - preserve latest observation while compacting.
  - added compaction carry-note for dropped high-signal observations:
    - synthetic `compaction_summary` observation captures retained anchors from dropped lines (`paths/targets/errors`).
- Validation:
  - `go test ./internal/orchestrator -run 'TestAppendObservationCompactsWithSignalPriority|TestAppendObservationAddsCompactionSummaryWhenSignalsDropped|TestNormalizeObservationEntryCompactsLongStrings|TestBuildWorkerAssistLayeredContextStressCompaction|TestBuildWorkerAssistLayeredContextIncludesMemoryCompactionSummary' -v`
  - quick-gate smoke executed:
    - `benchmark-20260226-115457` (`recovery_stress_repeat_pressure`, `repeat=1`, `seed=42`) -> failed
    - dominant failure remains `missing_required_artifacts=2` (existing terminal-contract gap), no loop incident regression observed.

### 2026-02-26 - Phase 4 slice: low-value listing churn -> no_progress

- Scope:
  - enforced explicit `no_progress` failure for repeated low-value listing churn (`list_dir`/`ls`) so the worker does not silently complete via `assist_no_new_evidence`.
  - added worker failure reason:
    - `no_progress` (`WorkerFailureNoProgress`) and wired it to execution-failure replan trigger classification.
  - updated semantic-repeat regression to assert:
    - no `task_completed` event,
    - `task_failed.reason=no_progress`.
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssistLoopGuardDetectsSemanticRepeatsAsNoProgress|TestRunWorkerTaskAssistConsecutiveToolChurnFallsBackToNoNewEvidence|TestRunWorkerTaskAssistRecoverToolCallCapFallsBackToNoNewEvidenceCompletion|TestRunWorkerTaskAssistRecoverCommandKeepsRecoverModeForToolCapFallback|TestRunWorkerTaskAssistAdaptiveReplanActionStepCapFallsBackToNoNewEvidence|TestRunWorkerTaskAssistAdaptiveReplanToolCapFallsBackToNoNewEvidence' -v`
  - quick-gate smoke executed:
    - `benchmark-20260226-131520` (`recovery_stress_repeat_pressure`, `repeat=1`, `seed=42`) -> failed
    - dominant failure remains `missing_required_artifacts=2`; loop-incident rate remained `0`.

### 2026-02-26 - Phase 4/5 bridge: assist completion-contract artifact aliasing

- Root cause:
  - static synthesized seed task requires concrete artifact `recon-seed.log`.
  - assist completion previously produced only auto-named logs (`worker-...-complete.log` + step logs), so completion-contract verification failed as `missing_required_artifacts`.
- Scope:
  - assist completion now materializes concrete expected-artifact aliases for concrete requirements and includes them in completion contract + evidence:
    - `internal/orchestrator/worker_runtime_assist_loop.go`
  - added regression:
    - `TestRunWorkerTaskAssistMaterializesConcreteExpectedArtifact`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssistMaterializesConcreteExpectedArtifact|TestRunWorkerTaskAssistLoopGuardDetectsSemanticRepeatsAsNoProgress|TestRunWorkerTaskAssistConsecutiveToolChurnFallsBackToNoNewEvidence|TestRunWorkerTaskAssist' -v`
  - quick-gate rerun (`benchmark-20260226-133828`) no longer surfaced `missing_required_artifacts`; dominant failure shifted to `assist_loop_detected` during repeated recover nmap command churn.

### 2026-02-26 - Phase 4/5 bridge: repeated recover command churn fallback

- Scope:
  - added bounded fallback for repeated recover-mode command streaks on no-new-evidence candidate commands:
    - emit `assist_no_new_evidence` completion instead of escalating to `assist_loop_detected`.
  - added regression:
    - `TestRunWorkerTaskAssistRecoverRepeatedCommandFallsBackToNoNewEvidence`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssistRecoverRepeatedCommandFallsBackToNoNewEvidence|TestRunWorkerTaskAssistRecoverCommandKeepsRecoverModeForToolCapFallback|TestRunWorkerTaskAssistLoopGuardDetectsSemanticRepeatsAsNoProgress|TestRunWorkerTaskAssistMaterializesConcreteExpectedArtifact|TestRunWorkerTaskAssist' -v`
  - quick-gate rerun:
    - `benchmark-20260226-152107` (`recovery_stress_repeat_pressure`, `repeat=1`, `seed=42`) -> **passed**
    - metrics: `task_success_rate=1.0`, `loop_incident_rate=0`, `recovery_success_rate=1.0`, `terminal_reason=completed`.

### 2026-02-26 - Phase 4 slice: shared-memory contract + fact provenance

- Scope:
  - enforced orchestrator-owned shared-memory contract in memory bank refresh:
    - `memory/shared_memory_contract.json` seals hashes for orchestrator-managed memory files.
    - drift now emits `run_warning` (`reason=shared_memory_contract_violation`) before orchestrator reconciliation.
  - added provenance ledger for promoted shared-memory facts:
    - `memory/known_facts_provenance.json` includes `source_event_ids`, source task/worker IDs, and state-transition lineage for verified/rejected findings.
  - memory context now records writer/policy/provenance counters and violation indicators.
- Validation:
  - targeted tests passed:
    - `go test ./internal/orchestrator -run 'TestInitializeMemoryBankCreatesScaffold|TestRefreshMemoryBankWritesFindingProvenance|TestRefreshMemoryBankWarnsOnSharedMemoryDrift|TestRefreshMemoryBankFoldsFindings|TestRefreshMemoryBankExcludesUnverifiedFindingsFromKnownFacts|TestRefreshMemoryBankCompactsLargeFindingSet' -v`
    - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
    - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - quick-gate execution (LLM auto planner):
    - `benchmark-s36-quick-recovery_stress_repeat_pressure-20260226-232020-s11` -> **failed**
      - run: `benchmark-s36-quick-recovery_stress_repeat_pressure-20260226-232020-s11-recovery_stress_repeat_pressure-r01`
      - checker failure: `exit_code=1` and `task_completed.reason=assist_no_new_evidence`.
    - `benchmark-s36-quick-evidence_first_reporting_quality-20260226-232333-s17` -> checker returned **passed**, but run was manually stopped during prolonged `nmap 127.0.0.0/8` execution.
      - run: `benchmark-s36-quick-evidence_first_reporting_quality-20260226-232333-s17-evidence_first_reporting_quality-r01`
      - scorecard terminal reason: `stopped:stop`; treated as non-clean gate signal.
- Gate status:
  - checkpoint protocol executed and logged.
  - quick-gate remains non-green for clean unattended pass due recovery scenario fail and long-running command behavior in evidence-first scenario.

### 2026-02-27 - Quick-gate scan-stall mitigation (loopback CIDR cap beyond discovery mode)

- Scope:
  - expanded nmap loopback broad-range capping from discovery-only (`-sn`) to all nmap target modes:
    - broad loopback CIDRs (for example `127.0.0.0/8`) are rewritten to host targets (`127.0.0.1`) to keep bounded runtime in quick/smoke scenarios.
  - preserved normal non-loopback behavior (no blanket narrowing of internal lab CIDRs).
- Code:
  - `internal/orchestrator/command_adaptation.go`
    - added generic loopback target rewrite (`capBroadLoopbackTarget`, `replaceNmapTargetArg`).
    - wired adaptation for all nmap invocations, not only `-sn`.
  - tests:
    - `internal/orchestrator/command_adaptation_test.go`
      - updated `TestAdaptCommandForRuntimeCapsLoopbackDiscoveryRange`
      - added `TestAdaptCommandForRuntimeCapsLoopbackRangeForNonDiscoveryScan`
- Validation:
  - `go test ./internal/orchestrator -run 'TestAdaptCommandForRuntimeCapsLoopback|TestAdaptCommandForRuntimeDownshiftsBroadNmap|TestEnsureNmapRuntimeGuardrailsProfiles' -v`
  - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - evidence-first quick-gate rerun:
    - `benchmark-s36-quick-evidence_first_reporting_quality-20260227-074653-s17`
    - event trace shows nmap target auto-injected as `127.0.0.1` and completed quickly (no long `/8` stall).
- Outcome:
  - long nmap `/8` stall symptom addressed for loopback scope.
  - quick-gate still fails for existing `assist_no_new_evidence` completion semantics (tracked separately).

### 2026-02-27 - Phase 5 slice: pseudo-command missing-tool installs blocked (tool-forge pivot)

- Root cause:
  - assist turns that emitted workflow directives like `report` as shell commands were treated as missing executables.
  - missing-tool remediation then requested package-install approvals (`apt-get install report`), creating noisy/stalling approval loops.
- Scope:
  - added pseudo-workflow token classifier in missing-tool remediation (`report`/`summary`/`plan` family).
  - these tokens are now treated as non-installable directives:
    - skip install approval/install attempt,
    - emit recover hint to pivot using available tools or helper generation via `type=tool` (tool-forge path),
    - keep repeated missing-tool retry fast-fail contract intact.
- Code:
  - `internal/orchestrator/worker_runtime_assist_install.go`
  - `internal/orchestrator/worker_runtime_assist_install_test.go`
  - `internal/orchestrator/worker_runtime_assist_modes_test.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestShouldSkipMissingToolInstall|TestRunWorkerTaskAssistPseudoWorkflowCommandSkipsInstallApproval|TestRunWorkerTaskAssistMissingToolInRecoverModeCanPivotToAvailableTool|TestRunWorkerTaskAssistMissingToolRepeatedRetryFailsFast' -v`
  - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`

### 2026-02-27 - Phase 5 slice: internal `report` command runtime + fallback path sanitization

- Root cause:
  - assist prompt/fallback allowed internal command `report`, but worker runtime did not implement it as a builtin command.
  - this mismatch caused churn: repeated `report` missing-tool/pseudo-command loops and brittle recover behavior.
  - recover goals also emitted `read_file <path>.` with trailing punctuation, causing path drift (`.`, `..`, `...`) across retries.
- Scope:
  - implemented worker builtin `report` execution path (local report synthesis over run artifacts) including optional output file arg handling.
  - kept pseudo-workflow install-skip policy for non-executable directives (`summary`/`plan` family), while allowing builtin `report`.
  - hardened fallback token parsing to trim trailing sentence punctuation from extracted local paths.
  - fallback assistant now returns `complete` for report goals in `recover` mode to avoid rerunning identical report steps.
- Code:
  - `internal/orchestrator/worker_runtime_assist_builtin.go`
  - `internal/orchestrator/worker_runtime_assist_exec.go`
  - `internal/orchestrator/worker_runtime_assist_install.go`
  - `internal/assist/assistant_fallback.go`
  - tests:
    - `internal/orchestrator/worker_runtime_assist_builtin_test.go`
    - `internal/orchestrator/worker_runtime_assist_install_test.go`
    - `internal/orchestrator/worker_runtime_assist_modes_test.go`
    - `internal/assist/assistant_fallback_test.go`
    - `internal/assist/assistant_test.go`
- Validation:
  - `go test ./internal/assist -v`
  - `go test ./internal/orchestrator ./cmd/birdhackbot-orchestrator ./internal/assist`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - live quick checks (`--scenario evidence_first_reporting_quality --repeat 1 --tool-install-policy never`):
    - `benchmark-pseudotoolcheck5-20260227-093138`:
      - `task-recon-seed` completed via `report` command without approval spam.
      - prior `read_file path.log....` drift not observed.
    - `benchmark-pseudotoolcheck6-20260227-093255`:
      - recover path for `task-h01` consistently used stable `read_file .../worker-task-h01-a1-a1-s2-t2.log` (no punctuation growth).
      - residual blocker remains `task-h01` `nmap -sV -v 127.0.0.1` `command_failed` on both attempts (tracked under PR-001 execution reliability).

### 2026-02-27 - Phase 7 slice 7.6: typed task-failure reason model

- Scope:
  - introduced canonical task-failure reason registry helpers:
    - `NormalizeTaskFailureReason` (strict known-enum normalization)
    - `CanonicalTaskFailureReason` (known-enum normalization with safe fallback)
  - migrated task-failure reason emit/read paths to typed constants + canonicalization across:
    - coordinator failure emitters and replan reason classifiers,
    - lease reclaim failure events,
    - worker failure emission/classification,
    - report task narrative failure-reason ingestion.
  - aligned scheduler blocked-reason mapping with typed constants (`approval_timeout`, `approval_denied`, `missing_prereq`, `scope_denied`) instead of free-form literals.
- Code:
  - `internal/orchestrator/reason_registry.go`
  - `internal/orchestrator/coordinator.go`
  - `internal/orchestrator/coordinator_replan.go`
  - `internal/orchestrator/coordinator_replan_helpers.go`
  - `internal/orchestrator/lease.go`
  - `internal/orchestrator/worker_runtime.go`
  - `internal/orchestrator/state_machine.go`
  - `internal/orchestrator/report.go`
  - tests:
    - `internal/orchestrator/state_inventory_test.go`
    - `internal/orchestrator/state_machine_test.go`
    - `internal/orchestrator/scheduler_test.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestStateInventory|TestClassifyWorkerFailureReason|TestMapFailureReasonToState|TestSchedulerMarkFailedBlocked|TestCoordinator|TestAssembleRunReport|TestLeaseStatusTaskStateRoundTrip|TestUpdateLeaseFromTaskState' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-131322` (`host_discovery_inventory`, `repeat=1`) -> **failed (known blocker)** with immediate `scope_denied` on `task-recon-seed` (`nmap` target inference path), run terminal `stopped:run_terminal_failure`.

### 2026-02-27 - Phase 7 slice 7.7: central event->state mutation ownership

- Scope:
  - introduced central event mutation ownership table in:
    - `internal/orchestrator/event_mutation_ownership.go`
  - table now defines canonical event-to-domain ownership for all event types and mutation domains:
    - `task_lifecycle`, `approval_status`, `replan_graph`, `run_projection`, `task_projection`, `worker_projection`.
  - runtime integration:
    - `runEventCache.applyEvent` now gates run/task/worker projection mutations through `EventMutatesDomain(...)`.
    - coordinator now asserts event authorization before event-driven mutations via `RequireEventMutationDomain(...)` for:
      - external approval decision ingestion (`approval_status`)
      - operator stop request task cancellation (`task_lifecycle`)
      - event-driven replan trigger synthesis (`replan_graph`)
- Code:
  - `internal/orchestrator/event_mutation_ownership.go`
  - `internal/orchestrator/event_cache.go`
  - `internal/orchestrator/coordinator.go`
  - `internal/orchestrator/coordinator_replan.go`
  - tests:
    - `internal/orchestrator/event_mutation_ownership_test.go`
    - `internal/orchestrator/state_inventory_test.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestValidateEventMutationTable|TestEventMutatesDomainCoreExpectations|TestRequireEventMutationDomain|TestRunEventCacheApplyEventRespectsMutationDomains|TestStateInventory|TestCoordinator' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-134820` (`host_discovery_inventory`, `repeat=1`) -> **failed (known blocker)** with `scope_denied=1`, terminal reason `stopped:run_terminal_failure`.

### 2026-02-27 - Phase 7 slice 7.8a: projection-overlap cleanup (no behavior change)

- Scope:
  - removed duplicated projection switch logic between:
    - `internal/orchestrator/run.go` (`BuildRunStatus`, `BuildWorkerStatus`)
    - `internal/orchestrator/event_cache.go` (`runEventCache.applyEvent`)
  - extracted shared helpers in:
    - `internal/orchestrator/event_projection.go`
      - `applyEventToRunTaskWorkerProjection`
      - `applyEventToWorkerStatusMap`
  - normalized projection-state string literals into shared constants:
    - run: `unknown|running|stopped|completed`
    - task: `queued|running|done`
    - worker: `seen|active|stopped`
  - objective: reduce accidental drift while keeping behavior unchanged.
- Code:
  - `internal/orchestrator/event_projection.go`
  - `internal/orchestrator/run.go`
  - `internal/orchestrator/event_cache.go`
  - tests:
    - `internal/orchestrator/run_test.go`
      - `TestBuildRunStatusMatchesCacheProjection`
      - `TestBuildWorkerStatusMatchesCacheProjection`
- Validation:
  - `go test ./internal/orchestrator -run 'TestBuildRunStatus|TestBuildWorkerStatusMatchesCacheProjection|TestBuildRunStatusMatchesCacheProjection|TestStateInventoryEventMutationTableCoverage|TestValidateEventMutationTable|TestRunEventCacheApplyEventRespectsMutationDomains|TestManagerStatus' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-140459` (`host_discovery_inventory`, `repeat=1`) -> **failed (known blocker)** with `scope_denied=1`, terminal reason `stopped:run_terminal_failure`.

### 2026-02-27 - Phase 7 slice 7.8b: coordinator lease-write overlap cleanup (no behavior change)

- Scope:
  - removed duplicated inline lease construction in coordinator dispatch branches by extracting shared helpers:
    - `writeLeaseForState(taskID, attempt, workerID, state, now)`
    - `writeLeaseFromSchedulerState(taskID, attempt, workerID, now)`
  - replaced repeated lease-write blocks for:
    - scope-denied path
    - approval-denied path
    - awaiting-approval path
    - initial leased path
- Code:
  - `internal/orchestrator/coordinator.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestCoordinator|TestBuildRunStatus|TestBuildWorkerStatusMatchesCacheProjection|TestValidateEventMutationTable|TestStateInventory' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-142312` (`host_discovery_inventory`, `repeat=1`) -> **failed (known blocker)** with `scope_denied=1`, terminal reason `stopped:run_terminal_failure`.

### 2026-02-27 - Phase 7 slice 7.8c: guard-failure flow dedupe (no behavior change)

- Scope:
  - extracted shared coordinator helper:
    - `failTaskFromGuard(taskID, workerID, reason, eventPayload, replanContext)`
  - replaced duplicated non-retryable guard-failure blocks in:
    - execution-timeout handler
    - budget-exhaustion handler
  - helper preserves existing sequence:
    - stop worker (best-effort) -> emit `task_failed` -> `markFailedWithReplan(..., retryable=false)` -> lease sync from scheduler state.
- Code:
  - `internal/orchestrator/coordinator.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestCoordinator|TestHandleExecutionTimeout|TestHandleBudgetGuards|TestStateInventory|TestBuildRunStatus' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - started `benchmark-20260227-142710` (`host_discovery_inventory`, `repeat=1`) but run stalled in long nmap execution (`task-recon-seed` heartbeat-only progress).
    - operator stop issued: `birdhackbot-orchestrator stop --sessions-dir sessions --run benchmark-20260227-142710-host_discovery_inventory-r01`.
    - benchmark wrapper reported `status=ok` with terminal `stopped:stop`; treat as **non-clean smoke signal** (known long-scan behavior, not attributed regression).

### 2026-02-27 - Phase 7 slice 7.8d: canonical event-enum dedupe (no behavior change)

- Scope:
  - introduced single canonical event-type list function:
    - `internal/orchestrator/types.go` -> `CanonicalEventTypes()`.
  - removed duplicated event-type list definitions in:
    - mutation ownership validation (`internal/orchestrator/event_mutation_ownership.go`)
    - state inventory uniqueness test (`internal/orchestrator/state_inventory_test.go`)
  - objective: prevent enum-drift from multiple manually maintained lists.
- Code:
  - `internal/orchestrator/types.go`
  - `internal/orchestrator/event_mutation_ownership.go`
  - `internal/orchestrator/state_inventory_test.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestStateInventoryEventTypesUnique|TestValidateEventMutationTable|TestStateInventoryEventMutationTableCoverage|TestCoordinator|TestBuildRunStatus' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-154821` (`host_discovery_inventory`, `repeat=1`) -> **failed (known blocker)** with `scope_denied=1`, terminal reason `stopped:run_terminal_failure`.

### 2026-02-27 - PR-009 scope-denied fix (assist command adaptation/validation arg sync)

- Root cause:
  - in assist command path, adapted/injected args were modified inside an inner `if !cfg.Diagnostic` scope using short variable declarations.
  - this shadowed outer `args`, so scope validation used stale pre-adaptation args (`-iL .../targets.txt`) and could fail with false `scope_denied`.
- Scope:
  - fixed assist command loop to update outer args (no shadowing) before scope validation.
  - added regression test proving adapted target injection survives to executed command args and does not emit `scope_denied` in this path.
- Code:
  - `internal/orchestrator/worker_runtime_assist_loop.go`
  - `internal/orchestrator/worker_runtime_assist_scope_sync_test.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskAssistMissingInputListFallbackInjectsTargetAfterAdaptation|TestRunWorkerTaskAssist' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-160133` started with no `scope_denied`/`task_failed` events observed on the recon seed path after adaptation.
    - run was operator-stopped due prolonged nmap execution (`stopped:stop`); treat as partial validation, not full closure signal for PR-009.

### 2026-02-27 - Phase 7 slice 7.8f: runtime command prep dedupe (no behavior change)

- Scope:
  - extracted a shared helper for runtime command preparation:
    - `applyCommandTargetFallback -> adaptCommandForRuntime -> applyCommandTargetFallback`.
  - replaced duplicated inline prep blocks in:
    - direct worker action runtime path.
    - assist loop `type=command` path.
    - assist tool execution path.
  - objective: remove overlap that previously enabled subtle drift/shadowing bugs between adaptation and scope-validation paths.
- Code:
  - `internal/orchestrator/runtime_command_prepare.go`
  - `internal/orchestrator/worker_runtime.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
  - `internal/orchestrator/worker_runtime_assist_exec.go`
  - `internal/orchestrator/runtime_command_prepare_test.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestPrepareRuntimeCommand|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskAssistMissingInputListFallbackInjectsTargetAfterAdaptation' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-161125` (`host_discovery_inventory`, `repeat=1`) progressed with adapted args (`... 127.0.0.1`) and no immediate `scope_denied`.
    - operator stop issued during prolonged recon worker runtime; terminal reason `stopped:stop` (partial smoke signal, not clean unattended closure).

### 2026-02-27 - Phase 7 slice 7.8g: command-repair prep dedupe (no behavior change target)

- Scope:
  - replaced duplicated command-repair runtime prep (`fallback -> adapt`) with shared `prepareRuntimeCommand` helper so repair retries follow the same target-injection/adaptation flow as primary execution.
- Code:
  - `internal/orchestrator/worker_runtime_command_repair.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestPrepareRuntimeCommand|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant|TestInferRequiredTargetOptionFromOutput' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-161948` (`host_discovery_inventory`, `repeat=1`) -> completed, no `scope_denied`, terminal reason `completed`.

### 2026-03-01 - Phase 7 slice 7.8h: runtime prep-note dedupe (no behavior change target)

- Scope:
  - extracted shared runtime prep message builder:
    - `runtimePreparationMessages(prepared runtimeCommandPreparation)`.
  - removed duplicated prep-note event branches in:
    - `internal/orchestrator/worker_runtime.go`
    - `internal/orchestrator/worker_runtime_assist_loop.go`
  - objective: keep adaptation/injection progress messaging consistent across worker command paths.
- Code:
  - `internal/orchestrator/runtime_command_prepare.go`
  - `internal/orchestrator/runtime_command_prepare_test.go`
  - `internal/orchestrator/worker_runtime.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestPrepareRuntimeCommand|TestRuntimePreparationMessagesOrder|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260227-161948` (`host_discovery_inventory`, `repeat=1`) -> completed, no `scope_denied`.
    - `benchmark-20260228-230447` (`host_discovery_inventory`, `repeat=1`) -> no `scope_denied`; terminal failure `execution_timeout` after prolonged recon runtime.

### 2026-03-01 - Phase 7 slice 7.8i: runtime prep-event emit dedupe (no behavior change target)

- Scope:
  - extracted shared runtime preparation progress emitter:
    - `emitRuntimePreparationProgress(...)`.
  - removed duplicated prep progress payload assembly in:
    - `internal/orchestrator/worker_runtime.go`
    - `internal/orchestrator/worker_runtime_assist_loop.go`
  - objective: keep event payload structure consistent while reducing overlap in prep instrumentation paths.
- Code:
  - `internal/orchestrator/runtime_command_prepare_emit.go`
  - `internal/orchestrator/runtime_command_prepare_test.go`
  - `internal/orchestrator/worker_runtime.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestPrepareRuntimeCommand|TestRuntimePreparationMessagesOrder|TestEmitRuntimePreparationProgressPayloadFields|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-001344` (`host_discovery_inventory`, `repeat=1`) operator-stopped during prolonged execution (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8j: shared runtime command util extraction (no behavior change target)

- Scope:
  - moved shared runtime command helpers out of `worker_runtime_assist_exec.go` into dedicated utility module:
    - `applyCommandTargetFallback`
    - `nmapHasInputListArg`
    - `firstTaskTarget`
    - `normalizeArgs`
  - objective: reduce cross-file coupling and keep runtime command contracts in one location.
- Code:
  - `internal/orchestrator/runtime_command_utils.go`
  - `internal/orchestrator/worker_runtime_assist_exec.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestApplyCommandTargetFallback|TestPrepareRuntimeCommand|TestRuntimePreparationMessagesOrder|TestEmitRuntimePreparationProgressPayloadFields|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-001703` (`host_discovery_inventory`, `repeat=1`) operator-stopped for budget control (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8k: assist exec utility split (no behavior change target)

- Scope:
  - moved large helper blocks out of `worker_runtime_assist_exec.go` into focused modules:
    - observation compaction/anchor helpers -> `worker_runtime_assist_observation.go`
    - assist action fingerprint normalization -> `worker_runtime_assist_action_key.go`
    - shared byte cap helper -> `runtime_bytes.go`
  - objective: reduce file size and isolate utility responsibilities without changing behavior.
- Code:
  - `internal/orchestrator/worker_runtime_assist_exec.go`
  - `internal/orchestrator/worker_runtime_assist_observation.go`
  - `internal/orchestrator/worker_runtime_assist_action_key.go`
  - `internal/orchestrator/runtime_bytes.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestApplyCommandTargetFallback|TestPrepareRuntimeCommand|TestRuntimePreparationMessagesOrder|TestEmitRuntimePreparationProgressPayloadFields|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant|TestAssistActionKey' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-002246` (`host_discovery_inventory`, `repeat=1`) operator-stopped for budget control (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8l: runtime helper extraction (no behavior change target)

- Scope:
  - moved shared runtime helpers out of `worker_runtime.go` into dedicated modules:
    - command scope-validation + shell token parsing -> `runtime_scope_validation.go`
    - worker signal/path-id helpers -> `runtime_worker_ids.go`
  - objective: reduce `worker_runtime.go` overlap and make cross-runtime helper ownership explicit.
- Code:
  - `internal/orchestrator/runtime_scope_validation.go`
  - `internal/orchestrator/runtime_worker_ids.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRequiresCommandScopeValidation|TestRunWorkerTaskAssist|TestAssistActionKey|TestApplyCommandTargetFallback|TestRuntimePreparationMessagesOrder|TestEmitRuntimePreparationProgressPayloadFields|TestWorkerFailureClassification' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-002620` (`host_discovery_inventory`, `repeat=1`) operator-stopped for budget control (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8m: failure helper extraction (no behavior change target)

- Scope:
  - moved worker failure support helpers out of `worker_runtime.go` into dedicated module:
    - `emitWorkerFailure`
    - `workerFailureClass*` constants
    - `classifyWorkerFailureReason`
    - `runErrString`
  - objective: isolate failure contract logic and reduce `worker_runtime.go` density.
- Code:
  - `internal/orchestrator/runtime_failure.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestWorkerFailureClassification|TestWorkerFailureEventsIncludeFailureClass|TestStateInventoryWorkerFailureClassificationsCovered|TestRequiresCommandScopeValidation|TestRunWorkerTaskAssist|TestAssistActionKey|TestApplyCommandTargetFallback' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-003431` (`host_discovery_inventory`, `repeat=1`) operator-stopped for budget control (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8n: runtime command exec extraction (no behavior change target)

- Scope:
  - moved worker command execution/output-cap helpers out of `worker_runtime.go`:
    - `runWorkerCommand`
    - `cappedOutputBuffer` + helpers
  - new home: `internal/orchestrator/runtime_command_exec.go`.
  - objective: isolate command execution mechanics and reduce worker runtime file density.
- Code:
  - `internal/orchestrator/runtime_command_exec.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant|TestWorkerFailureClassification|TestRequiresCommandScopeValidation|TestRunWorkerCommandContextCancellation|TestWorkerCommand' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-003802` (`host_discovery_inventory`, `repeat=1`) operator-stopped for budget control (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8o: runtime worker-task helper extraction (no behavior change target)

- Scope:
  - moved worker task lifecycle helper functions out of `worker_runtime.go`:
    - `workerAttemptAlreadyCompleted`
    - `workerFailureAlreadyRecorded`
    - `primaryTaskTarget`
  - new home: `internal/orchestrator/runtime_worker_task_helpers.go`.
  - objective: isolate worker task-state bookkeeping helpers and further reduce `worker_runtime.go` size.
- Code:
  - `internal/orchestrator/runtime_worker_task_helpers.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTask|TestRunWorkerTaskAssist|TestWorkerFailureClassification|TestWorkerFailureAlreadyRecorded|TestWorkerAttemptAlreadyCompleted|TestWorkerRuntime' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-103930` (`host_discovery_inventory`, `repeat=1`) operator-stopped for budget control (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8p: assist-loop helper extraction (no behavior change target)

- Scope:
  - moved non-loop helper functions out of `worker_runtime_assist_loop.go` into:
    - `internal/orchestrator/worker_runtime_assist_loop_helpers.go`
  - extracted helpers include:
    - action/result streak tracking and result key generation
    - no-new-evidence candidate and low-value listing checks
    - recover pivot basis parsing helpers
    - assist prompt scope builder
    - expected artifact materialization helper
    - autonomous question answer helper
    - shell tool script path resolution helper
  - objective: reduce assist-loop file density and isolate helper logic without changing runtime behavior.
- Code:
  - `internal/orchestrator/worker_runtime_assist_loop.go`
  - `internal/orchestrator/worker_runtime_assist_loop_helpers.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskAssistContextEnvelopeCapturesCarryoverAndDelta|TestAssistActionKey|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-110051` (`host_discovery_inventory`, `repeat=1`) operator-stopped for budget control (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8q: archive workflow helper extraction (no behavior change target)

- Scope:
  - moved archive/local-file workflow adaptation helpers out of `worker_runtime.go` into:
    - `internal/orchestrator/runtime_archive_workflow.go`
  - extracted set includes:
    - archive runtime env guardrail and command adaptation
    - zip/hash/wordlist input discovery and adaptation helpers
    - john/fcrackzip/unzip adaptation and supplemental artifact synthesis
    - recovered password parsing and option/flag helpers
  - objective: isolate archive workflow logic and further reduce `worker_runtime.go` complexity.
- Code:
  - `internal/orchestrator/runtime_archive_workflow.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant|TestArchive|TestRunWorkerTask|TestWorkerRuntime' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-110614` (`host_discovery_inventory`, `repeat=1`) operator-stopped for budget control (`stopped:stop`) with no `scope_denied`/`task_failed` observed before stop.

### 2026-03-01 - Phase 7 slice 7.8r: vulnerability evidence helper extraction (no behavior change target)

- Scope:
  - moved vulnerability-evidence helper logic out of `worker_runtime.go` into:
    - `internal/orchestrator/runtime_vulnerability_evidence.go`
  - extracted set includes:
    - nmap actionable-evidence detectors and timeout/output checks
    - weak vulnerability-action rewrite helpers
    - wrapped-shell nmap vuln-profile enforcement helpers
    - vulnerability evidence authenticity/placeholder guards
  - objective: isolate vulnerability-evidence policy/rewrites and reduce `worker_runtime.go` density without behavior intent changes.
- Code:
  - `internal/orchestrator/runtime_vulnerability_evidence.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'Vulnerability|Nmap|nmap|CommandAdaptation' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - smoke benchmark:
    - `benchmark-20260301-111652` (`host_discovery_inventory`, `repeat=1`) failed in sandboxed execution path due `nmap` netlink permission (`route_dst_netlink: cannot create AF_NETLINK socket: Operation not permitted`), terminal `run_terminal_failure`.
    - rerun outside sandbox restriction succeeded for nmap execution path but remained LLM-timeout bound and was operator-stopped for budget control (`benchmark-20260301-113124`, terminal `stopped:stop`).

### 2026-03-01 - Phase 7 slice 7.8s: nmap retry/evidence helper extraction (no behavior change target)

- Scope:
  - moved nmap retry/evidence helper logic out of `worker_runtime.go` into:
    - `internal/orchestrator/runtime_nmap_evidence_retry.go`
  - extracted set includes:
    - host-timeout retry decision + retry-arg synthesis
    - remaining-context timeout fitting helpers
    - nmap option upsert helper and retry-output merge helper
    - command-output evidence validation for nmap and archive-tool evidence checks
  - objective: isolate nmap retry/evidence policy mechanics and further reduce `worker_runtime.go` complexity without behavior changes.
- Code:
  - `internal/orchestrator/runtime_nmap_evidence_retry.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'Vulnerability|Nmap|nmap|WorkerRuntime|CommandAdaptation' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false ./cmd/birdhackbot`
  - `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-114011` (`host_discovery_inventory`, `repeat=1`) executed with `timeout 120s`; partial summary written on interrupt (expected for bounded smoke) with no immediate regression signals before stop.
  - follow-up discovery:
    - bounded benchmark probes (`benchmark-20260301-114511`, `benchmark-20260301-114724`) still emitted `llm_timeout_seconds=90` in worker task-progress events even when timeout override was attempted, so fast-smoke timeout override remains unreliable end-to-end and is tracked in `TASKS.md` Phase 8.

### 2026-03-01 - Phase 7 slice 7.8t: assist timeout-cap wiring + verification

- Scope:
  - wired worker assist call-timeout cap to configured env timeout:
    - `newAssistCallContext` now uses `workerAssistCallTimeoutCap()` instead of fixed `workerAssistLLMCallMax`.
  - added focused tests:
    - `TestWorkerAssistCallTimeoutCapFromEnv`
    - `TestNewAssistCallContextUsesConfiguredTimeoutCap`
  - objective: make bounded smoke/benchmark timeout controls effective for faster iteration loops.
- Code:
  - `internal/orchestrator/worker_runtime_assist_llm.go`
  - `internal/orchestrator/worker_runtime_assist_llm_test.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestWorkerAssistCallTimeoutCapFromEnv|TestNewAssistCallContextUsesConfiguredTimeoutCap|Vulnerability|Nmap|nmap|WorkerRuntime|CommandAdaptation' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-115719` run with `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15` now emits `llm_timeout_seconds=15` in task-progress events (override verified).

### 2026-03-01 - Phase 7 slice 7.8u: worker policy helper extraction (no behavior change target)

- Scope:
  - moved worker policy/bootstrap helpers out of `worker_runtime.go` into:
    - `internal/orchestrator/runtime_worker_policy.go`
  - extracted helpers:
    - `enforceWorkerRiskPolicy`
    - `EmitWorkerBootstrapFailure`
  - objective: keep worker runtime lifecycle code focused while preserving policy/bootstrap behavior.
- Code:
  - `internal/orchestrator/runtime_worker_policy.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestWorkerAssistCallTimeoutCapFromEnv|TestNewAssistCallContextUsesConfiguredTimeoutCap|TestEmitWorkerBootstrapFailure|WorkerFailure|RiskPolicy|Vulnerability|Nmap|nmap|WorkerRuntime|CommandAdaptation' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-115954` (`host_discovery_inventory`, `repeat=1`) executed with `timeout 45s` and `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`; partial summary expected from timeout and worker events confirm `llm_timeout_seconds=15`.

### 2026-03-01 - Phase 7 slice 7.8v: input-repair helper extraction (no behavior change target)

- Scope:
  - moved local input-repair helper cluster out of `worker_runtime.go` into:
    - `internal/orchestrator/runtime_input_repair.go`
  - extracted set includes:
    - command/shell input-path repair flow
    - workspace/wordlist candidate discovery and decompression helpers
    - dependency artifact path-reference extraction helpers
    - token/similarity scoring helpers for candidate selection
  - objective: isolate large local-input-repair logic and further reduce `worker_runtime.go` size while preserving behavior.
- Code:
  - `internal/orchestrator/runtime_input_repair.go`
  - `internal/orchestrator/worker_runtime.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTask|TestRepair|TestArchive|TestCommandAdaptation|TestWorkerRuntime|TestEmitWorkerBootstrapFailure|TestWorkerAssistCallTimeoutCapFromEnv|TestNewAssistCallContextUsesConfiguredTimeoutCap' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-121930` (`host_discovery_inventory`, `repeat=1`) executed with `timeout 45s` and `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`; partial summary expected from timeout and worker events confirm `llm_timeout_seconds=15` with no immediate `task_failed`/`scope_denied` in sampled events.

### 2026-03-01 - Phase 7 slice 7.8w: assist emit-path extraction (no behavior change target)

- Scope:
  - moved assist-loop emit helper logic out of `worker_runtime_assist_loop.go` into:
    - `internal/orchestrator/worker_runtime_assist_loop_emit.go`
  - extracted helpers:
    - `emitAssistCompletion`
    - `emitAssistNoProgressFailure`
    - `emitAssistNoNewEvidenceCompletion`
    - `emitAssistSummaryFallback`
    - `emitAssistAdaptiveBudgetFallback`
  - objective: reduce assist-loop body density and centralize repetitive event/failure emit paths without behavior changes.
- Code:
  - `internal/orchestrator/worker_runtime_assist_loop.go`
  - `internal/orchestrator/worker_runtime_assist_loop_emit.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestRunWorkerTaskAssistContextEnvelopeCapturesCarryoverAndDelta|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant|TestWorkerAssistCallTimeoutCapFromEnv|TestNewAssistCallContextUsesConfiguredTimeoutCap|TestWorkerFailureClassification' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-123456` (`host_discovery_inventory`, `repeat=1`) executed with `timeout 45s` and `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`; partial summary expected from timeout and sampled events confirm `llm_timeout_seconds=15` with no immediate `task_failed`/`scope_denied`.

### 2026-03-01 - Phase 7 slice 7.8x: planner archive normalization extraction (no behavior change target)

- Scope:
  - moved archive workflow task-dependency normalization helpers out of `main_planner.go` into:
    - `cmd/birdhackbot-orchestrator/main_planner_archive.go`
  - extracted helpers:
    - `normalizeArchiveWorkflowTaskDependencies`
    - `goalLikelyArchiveWorkflow`
    - `taskLooksLikeArchiveCrackStrategy`
    - `taskLooksLikeArchiveHashMaterial`
    - `taskLooksLikeArchiveExtraction`
    - `containsAny`
    - `containsString`
  - objective: reduce planner file density and isolate archive workflow planning normalization logic with no behavior change.
- Code:
  - `cmd/birdhackbot-orchestrator/main_planner.go`
  - `cmd/birdhackbot-orchestrator/main_planner_archive.go`
- Validation:
  - `go test ./cmd/birdhackbot-orchestrator -run 'Test.*Planner|Test.*Zip|TestAppendEnvIfMissing|TestBenchmark' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-124225` (`host_discovery_inventory`, `repeat=1`) executed with `timeout 45s` and `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`; partial summary expected from timeout and sampled events confirm `llm_timeout_seconds=15` with no immediate `task_failed`/`scope_denied`.

### 2026-03-01 - Phase 7 slice 7.8y: planner helper extraction (no behavior change target)

- Scope:
  - moved planner helper/util functions out of `main_planner.go` into:
    - `cmd/birdhackbot-orchestrator/main_planner_helpers.go`
  - extracted helpers:
    - `plannerPlaybookHints`
    - `plannerPlaybookQuery`
    - `plannerPlaybookBounds`
    - `plannerPlaybookDir`
    - `compactStrings`
    - `detectWorkerConfigPath`
  - objective: reduce planner file density and centralize shared planner utility helpers without behavior changes.
- Code:
  - `cmd/birdhackbot-orchestrator/main_planner.go`
  - `cmd/birdhackbot-orchestrator/main_planner_helpers.go`
- Validation:
  - `go test ./cmd/birdhackbot-orchestrator -run 'Test.*Planner|Test.*Zip|TestAppendEnvIfMissing|TestBenchmark' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-124907` (`host_discovery_inventory`, `repeat=1`) executed with `timeout 45s` and `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`; partial summary expected from timeout and sampled events confirm `llm_timeout_seconds=15` with no immediate `task_failed`/`scope_denied`.

### 2026-03-01 - Phase 7 slice 7.8z: planner review/prompt utility extraction (no behavior change target)

- Scope:
  - moved planner review/prompt utility functions out of `main_planner.go` into:
    - `cmd/birdhackbot-orchestrator/main_planner_review.go`
  - extracted helpers:
    - `plannerPromptHash`
    - `printPlanSummary`
    - `persistPlanReview`
    - `minInt`
    - `maxInt`
    - plus local pointer helpers (`float32Ptr`, `intPtrPositive`) retained with the same behavior surface.
  - objective: further reduce planner core-file density and isolate review/prompt hashing helpers without behavior changes.
- Code:
  - `cmd/birdhackbot-orchestrator/main_planner.go`
  - `cmd/birdhackbot-orchestrator/main_planner_review.go`
- Validation:
  - `go test ./cmd/birdhackbot-orchestrator -run 'Test.*Planner|Test.*Zip|TestAppendEnvIfMissing|TestBenchmark' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-125441` (`host_discovery_inventory`, `repeat=1`) executed with `timeout 45s` and `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`; partial summary expected from timeout and sampled events confirm `llm_timeout_seconds=15` with no immediate `task_failed`/`scope_denied`.

### 2026-03-01 - Phase 7 slice 7.8aa: planner retry/attempt extraction (no behavior change target)

- Scope:
  - moved planner retry/attempt helpers and diagnostics type out of `main_planner.go` into:
    - `cmd/birdhackbot-orchestrator/main_planner_attempts.go`
  - extracted items:
    - `plannerAttemptDiagnostic` type
    - `adaptivePlannerHypothesisLimit`
    - `adaptivePlannerBackoff`
    - `adaptivePlannerMaxTokens`
    - `persistPlannerAttemptDiagnostics`
  - objective: keep planner orchestration code focused and isolate retry/attempt diagnostics mechanics without behavior changes.
- Code:
  - `cmd/birdhackbot-orchestrator/main_planner.go`
  - `cmd/birdhackbot-orchestrator/main_planner_attempts.go`
- Validation:
  - `go test ./cmd/birdhackbot-orchestrator -run 'Test.*Planner|Test.*Zip|TestAppendEnvIfMissing|TestBenchmark' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-130001` (`host_discovery_inventory`, `repeat=1`) executed with `timeout 45s` and `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`; partial summary expected from timeout and sampled events confirm `llm_timeout_seconds=15` with no immediate `task_failed`/`scope_denied`.

### 2026-03-01 - Phase 7 slice 7.8ab: assist result-path dedupe (no behavior change target)

- Scope:
  - extracted repeated assist runtime-result handling (result streak tracking + no-new-evidence completion gates) into:
    - `internal/orchestrator/worker_runtime_assist_loop_result.go`
  - replaced duplicated command/tool success-path blocks in:
    - `internal/orchestrator/worker_runtime_assist_loop.go`
  - objective: reduce assist-loop duplication and keep no-new-evidence completion gating consistent across `type=tool` and `type=command` execution paths.
- Code:
  - `internal/orchestrator/worker_runtime_assist_loop_result.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestRunWorkerTaskAssistContextEnvelopeCapturesCarryoverAndDelta|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestWorkerFailureClassification|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-130613` (`host_discovery_inventory`, `repeat=1`) reached terminal `run_terminal_failure` due sandboxed `nmap` runtime error (`route_dst_netlink: cannot create AF_NETLINK socket: Operation not permitted`) in worker command logs; no slice-specific regression signature observed.

### 2026-03-01 - Phase 7 slice 7.8ac: assist missing-tool recovery dedupe (no behavior change target)

- Scope:
  - extracted duplicated assist missing-tool remediation/retry-contract handling into:
    - `internal/orchestrator/worker_runtime_assist_loop_missing_tool.go`
  - replaced duplicated command/tool failure-path blocks in:
    - `internal/orchestrator/worker_runtime_assist_loop.go`
  - objective: keep missing-tool retry policy and recover-mode transitions consistent between `type=tool` and `type=command` execution paths.
- Code:
  - `internal/orchestrator/worker_runtime_assist_loop_missing_tool.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant|TestWorkerFailureClassification' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark:
    - `benchmark-20260301-131617` (`host_discovery_inventory`, `repeat=1`) reached terminal `run_terminal_failure` with worker `nmap` netlink error (`route_dst_netlink: cannot create AF_NETLINK socket: Operation not permitted`); no slice-specific regression signature observed.
    - escalated rerun outside sandbox netlink restrictions:
      - `benchmark-20260301-131710` (`timeout 45s`, `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`) reached bounded interruption with partial summary; sampled events show no `scope_denied` and no netlink permission failure in artifacts.

### 2026-03-01 - Phase 7 slice 7.8ad: assist post-success mode dedupe (no behavior change target)

- Scope:
  - extracted duplicated post-success mode settlement into:
    - `internal/orchestrator/worker_runtime_assist_loop_mode.go`
  - replaced duplicated `wasRecover` mode-reset blocks in:
    - `internal/orchestrator/worker_runtime_assist_loop.go`
  - objective: keep success-path mode transitions centralized (`recover` continuation vs `execute-step` reset) across command/tool execution branches.
- Code:
  - `internal/orchestrator/worker_runtime_assist_loop_mode.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant|TestWorkerFailureClassification' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark (outside sandbox netlink restrictions):
    - `benchmark-20260301-132014` (`timeout 45s`, `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`) reached bounded interruption with partial summary; sampled events show no `scope_denied` and no netlink permission failures.

### 2026-03-01 - Phase 7 slice 7.8ae: assist execution-failure dedupe (no behavior change target)

- Scope:
  - extracted duplicated command/tool execution-failure handling into:
    - `internal/orchestrator/worker_runtime_assist_loop_failure.go`
  - replaced duplicated failure-path blocks in:
    - `internal/orchestrator/worker_runtime_assist_loop.go`
  - shared helper now centralizes:
    - missing-tool remediation path invocation
    - recover-mode terminal failure emission (`command_failed` / `command_timeout`)
    - transition-to-recover handling for non-recover failures
    - optional scope-denied terminal handling for tool path
  - objective: reduce assist-loop duplication while preserving failure semantics across command/tool branches.
- Code:
  - `internal/orchestrator/worker_runtime_assist_loop_failure.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssist|TestRunWorkerTaskAssistCommandScopeValidationUsesAdaptedArgs|TestRunWorkerTaskCommandActionRepairsRequiredHostOptionWithoutAssistant|TestWorkerFailureClassification' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark (outside sandbox netlink restrictions):
    - `benchmark-20260301-133706` (`timeout 45s`, `--worker-env BIRDHACKBOT_LLM_TIMEOUT_SECONDS=15`) reached bounded interruption with partial summary.
    - sampled events show no `scope_denied` and no netlink permission failures.
    - known open behavior remains: non-summary recon task can still complete via `assist_no_new_evidence` (tracked by `PR-010`).

### 2026-03-01 - PR-010 tightening: no-new-evidence terminalization + completion-contract hardening

- Scope:
  - enforced no-new-evidence policy for non-summary recon/validation tasks:
    - `assist_no_new_evidence` fallback now terminalizes as `no_progress` failure (`fallback_rejected`) instead of `task_completed`.
  - hardened assist completion contracts for no-new-evidence fallbacks:
    - no synthetic `task_execution_result` finding emission for `status_reason=assist_no_new_evidence`.
    - completion contract now marks `allow_fallback_without_findings=true` with `verification_status=fallback_no_new_evidence` for fallback completions.
  - updated quick-gate checker to assert policy on non-summary recon/validation tasks only:
    - `scripts/check_benchmark_gate.py` now evaluates `task_completed reason=assist_no_new_evidence` against task strategy from run plan and excludes summary strategies.
- Code:
  - `internal/orchestrator/worker_runtime_assist_loop_helpers.go`
  - `internal/orchestrator/worker_runtime_assist_loop_emit.go`
  - `internal/orchestrator/worker_runtime_assist_loop.go`
  - `internal/orchestrator/coordinator_completion_contract.go`
  - `internal/orchestrator/completion_gate.go`
  - `internal/orchestrator/worker_runtime_assist_modes_test.go`
  - `scripts/check_benchmark_gate.py`
- Validation:
  - `go test ./internal/orchestrator -run 'TestRunWorkerTaskAssistNoNewEvidenceFailsReconValidationTask|TestRunWorkerTaskAssistConsecutiveToolChurnFallsBackToNoNewEvidence|TestRunWorkerTaskAssistRecoverRepeatedCommandFallsBackToNoNewEvidence|TestRunWorkerTaskAssistAdaptiveReplanToolCapFallsBackToNoNewEvidence|TestWorkerFailureClassification|TestEvaluateCompletionVerificationGate' -count=1`
  - `go test ./internal/orchestrator ./internal/assist ./cmd/birdhackbot-orchestrator -count=1`
  - `go build -buildvcs=false -o ./birdhackbot ./cmd/birdhackbot`
  - `go build -buildvcs=false -o ./birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator`
  - bounded smoke benchmark (outside sandbox netlink restrictions):
    - `benchmark-20260301-135622` shows recon-seed no-new-evidence path now emits `task_failed reason=no_progress` with enforcement metadata (`completion_reason_candidate=assist_no_new_evidence`, `completion_reason_enforcement=fallback_rejected`) and no recon `task_completed reason=assist_no_new_evidence`.
  - single-scenario smoke benchmark:
    - `benchmark-20260301-135739` completed with adaptive fallback completion present but no non-summary recon/validation `assist_no_new_evidence` completion.
  - quick-gate check:
    - `python3 scripts/check_benchmark_gate.py --summary sessions/benchmarks/benchmark-20260301-135739/summary.json --sessions-dir sessions` => `quick-gate PASSED`.


## Triage Policy

- Every new production-facing failure gets a new `PR-xxx` entry before code changes.
- If a fix lands without a linked problem ID and acceptance signal, reopen as process defect.
- Closed problems must keep repro, evidence, and validation links for regression audits.
