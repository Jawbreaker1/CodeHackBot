# State And Status Inventory

Date: 2026-02-27  
Owner: Orchestrator runtime

## Purpose

This document is the canonical inventory of state/status enums and their intended ownership.
Use it to avoid introducing new overlapping state concepts or free-form status strings when an existing domain already applies.

## Locked Target Model (Slice 7.1)

This section is the implementation contract for cleanup slices 7.2-7.8.

Authoritative write-model state machines:

- `TaskState` (task lifecycle and retry transitions)
- `RunPhase` (workflow progression only)
- `RunOutcome` (terminal truth; to be introduced in slice 7.3)
- `ApprovalStatus` (approval request lifecycle)
- `FindingState` (evidence lifecycle)

Derived read-model projections (not decision sources):

- `LeaseStatus` (persisted task lease projection derived from task transitions)
- `RunStatus.State` (UI/API projection from events)
- `WorkerStatus.State` (UI/API projection from worker events)

Rules:

- Terminal run truth must be decided by verification gates + `RunOutcome`, not by projection state.
- No domain may have two independent lifecycle authorities.
- Event handlers must map to one authoritative lifecycle owner per mutation.

## Semantic Glossary (Locked)

- `failed` (task): execution attempt completed but did not satisfy contract/evidence requirements.
- `blocked` (task): cannot continue until external prerequisite changes (approval/dependency/resource).
- `canceled` (task): intentionally terminated by operator/system stop intent.
- `stopped` (run/worker projection): runtime halted; not itself a success/failure truth verdict.
- approval `decision` (`allow|request|deny`): policy decision before/while creating approval flow.
- approval `status` (`pending|granted|denied|expired`): lifecycle state of a specific approval record.

## Migration Guardrails

- Any new state/status enum requires:
  - update to this inventory doc,
  - guard coverage in `internal/orchestrator/state_inventory_test.go`,
  - explicit owner declaration (authoritative vs derived).
- No free-form `reason` values when typed reason enums exist.

## Canonical Domains

### Task State (`internal/orchestrator/state_machine.go`)

- `queued`
- `leased`
- `running`
- `awaiting_approval`
- `completed`
- `failed`
- `blocked`
- `canceled`

Allowed transitions:

- `queued -> leased|canceled`
- `leased -> running|awaiting_approval|queued|failed|blocked|canceled`
- `running -> awaiting_approval|completed|failed|blocked|canceled`
- `awaiting_approval -> running|queued|blocked|canceled`
- `failed -> queued` (retry path)
- `blocked -> queued|canceled`
- `completed -> (terminal)`
- `canceled -> (terminal)`

### Lease Status (`internal/orchestrator/lease.go`)

- `leased`
- `queued`
- `awaiting_approval`
- `running`
- `completed`
- `failed`
- `blocked`
- `canceled`

Notes:

- Lease status is a persisted lease projection derived from `TaskState` (`TaskState <-> LeaseStatus` mapping).
- Coordinator now syncs lease status from scheduler task state instead of writing independent lifecycle decisions.
- `*.lease.json` remains operational metadata (worker assignment, deadlines, attempts) for reclaim/timeout flows.

### Run Phase (`internal/orchestrator/run_phase.go`)

- `planning`
- `review`
- `approved`
- `executing`
- `completed`

Notes:

- Stored in plan metadata (`plan.json`).
- Used for planning/execution gating (TUI + CLI).

### Run Outcome (`internal/orchestrator/run_outcome.go`)

- `success`
- `failed`
- `aborted`
- `partial`

Notes:

- Introduced as canonical terminal-truth enum in cleanup slice 7.3.
- Wired in slice 7.4:
  - persisted in `plan.metadata.run_outcome`,
  - emitted in terminal run events (`run_completed` / `run_stopped`) as `payload.run_outcome`.
- Legacy terminal detail/status fields remain for compatibility.

### Run Status State (`RunStatus.State`)

- `unknown`
- `running`
- `stopped`
- `completed`

Source:

- Derived from event stream cache (`internal/orchestrator/event_cache.go`, `internal/orchestrator/run.go`).

### Worker Status State (`WorkerStatus.State`)

- `seen`
- `active`
- `stopped`

Source:

- Derived from worker/task events (`internal/orchestrator/event_cache.go`, `internal/orchestrator/run.go`).

### Approval Domain (`internal/orchestrator/approval.go`)

Statuses:

- `pending`
- `granted`
- `denied`
- `expired`

Decisions:

- `allow`
- `request`
- `deny`

Scopes:

- `once`
- `task`
- `session`

### Permission Mode (`internal/orchestrator/approval.go`)

- `readonly`
- `default`
- `all`

### Risk Tier (`internal/orchestrator/approval.go`)

- `recon_readonly`
- `active_probe`
- `exploit_controlled`
- `priv_esc`
- `disruptive`

### Replan Outcome (`internal/orchestrator/coordinator_replan_helpers.go`)

- `refine_task`
- `split_task`
- `terminate_run`

### Finding State (`internal/orchestrator/types.go`)

- `hypothesis`
- `candidate_finding`
- `verified_finding`
- `rejected_finding`

### Worker Failure Reason (`internal/orchestrator/worker_runtime.go`)

- `invalid_task_action`
- `workspace_create_failed`
- `invalid_working_dir`
- `working_dir_create_failed`
- `artifact_write_failed`
- `command_failed`
- `command_timeout`
- `command_interrupted`
- `scope_denied`
- `policy_denied`
- `policy_invalid`
- `worker_bootstrap_failed`
- `insufficient_evidence`
- `assist_unavailable`
- `assist_parse_failure`
- `assist_timeout`
- `assist_needs_input`
- `assist_no_action`
- `assist_loop_detected`
- `assist_budget_exhausted`
- `no_progress`

### Worker Failure Class (`internal/orchestrator/worker_runtime.go`)

- `context_loss`
- `strategy_failure`
- `contract_failure`

Notes:

- `reason` remains the detailed canonical failure enum.
- `failure_class` is the coarse triage bucket for telemetry/reporting.

### Task Failure Reason Registry (`internal/orchestrator/reason_registry.go`)

Coordinator/runtime reasons:

- `worker_exit`
- `worker_reconcile_stale`
- `missing_required_artifacts`
- `missing_prereq`
- `startup_sla_missed`
- `stale_lease`
- `launch_failed`
- `approval_timeout`
- `approval_denied`
- `execution_timeout`
- `budget_exhausted`
- `repeated_step_loop`

Notes:

- Worker failure reasons are also part of this registry.
- Emitters should write canonical constants; readers should normalize through the registry before classification/reporting.

## Event Type Inventory

Canonical event types (`internal/orchestrator/types.go`):

- `run_started`
- `task_leased`
- `task_started`
- `task_progress`
- `task_artifact`
- `task_finding`
- `task_failed`
- `task_completed`
- `worker_started`
- `worker_heartbeat`
- `worker_stopped`
- `run_stopped`
- `run_completed`
- `run_report_generated`
- `run_warning`
- `run_state_updated`
- `run_replan_requested`
- `approval_requested`
- `approval_granted`
- `approval_denied`
- `approval_expired`
- `worker_stop_requested`
- `operator_instruction`

## Event Mutation Ownership (Slice 7.7)

Central table: `internal/orchestrator/event_mutation_ownership.go`

Mutation domains:

- `task_lifecycle`
- `approval_status`
- `replan_graph`
- `run_projection`
- `task_projection`
- `worker_projection`

Runtime usage:

- `runEventCache.applyEvent` now gates projection mutations through the central table (`run_projection`, `task_projection`, `worker_projection`).
- shared projection helper (`internal/orchestrator/event_projection.go`) is now the single mapping source for projection-state updates used by both cache and event-list builders.
- coordinator event handlers assert mutation authorization before applying event-driven state changes:
  - external approval decision ingestion (`approval_status`)
  - operator stop request task cancellation (`task_lifecycle`)
  - event-driven replan trigger synthesis (`replan_graph`)

Rule:

- Any new event type or event-driven mutation must update the central mutation table and related guard tests.

## Field Conventions

- `reason`: machine-classification field for status/failure reason. Prefer canonical enum values where available.
- `fallback_reason`: supplemental context for bounded fallback paths. Not a replacement for `reason`.
- domain-specific detail keys (for example `pseudo_command_reason`, `recover_pivot_contract`) are additive diagnostics.

Rule:

- Do not overload `reason` with a free-form sentence when a canonical reason enum exists.

## Current Complexity Risks

- Task lifecycle is represented in two domains (`TaskState` and `LeaseStatus`) with no single shared transition validator.
- Run lifecycle uses both plan `run_phase` and derived `RunStatus.State`.
- Failure diagnostics mix canonical and free-form reasons in some paths.

## Simplification Follow-Up (Sprint 36/37)

- Collapse duplicate lifecycle ownership:
  - `TaskState` is authoritative task lifecycle.
  - `LeaseStatus` becomes lease persistence/projection (derived, not independent lifecycle authority).
- Separate workflow progress from terminal truth:
  - keep `RunPhase` for planning/execution progression.
  - add `RunOutcome` for terminal truth (`success|failed|aborted|partial`).
- Define a small typed reason registry for `task_failed` and run-terminal payloads (coordinator + worker + reporter).
- Keep one authoritative mapping table: `event type -> state domain mutation`.
- Add explicit semantic glossary enforcement:
  - `failed` vs `blocked`
  - `canceled` vs `stopped`
  - approval decision (`allow|request|deny`) vs approval request status (`pending|granted|denied|expired`)
