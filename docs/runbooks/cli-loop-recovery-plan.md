# CLI Closed-Loop Recovery Plan (Sprint 36 / PR-011)

Date: 2026-03-02  
Status: Draft (planning only)

## Scope

- In scope: interactive CLI assist loop (`internal/cli/*`).
- Out of scope: orchestrator worker runtime changes in this slice.

## Goal

Make CLI assist execution resilient by enforcing a strict per-step loop where the model learns from execution feedback before deciding the next move.

## Per-Step Contract

1. `execute`
2. `observe` (`exit_code`, key stdout/stderr, artifacts, evidence delta)
3. `interpret` (new facts, disproven assumptions, new leads)
4. `decide`:
   - `retry_modified`
   - `pivot_strategy`
   - `ask_user`
   - `step_complete`
5. `memory_update` (facts, failed approaches, open unknowns, retained anchors)

## Completion Contract

- `step_complete` is valid only with:
  - `objective_met` (`true|false`)
  - `evidence_refs` (one or more artifact/log refs)
  - `why_met` (short reason)
- If `objective_met=true` and evidence is missing, reject completion and continue loop/recovery.

## Command Contract

- Suggestion command must be executable shape (`command` token + `args[]`).
- Reject prose, list-item text, and combined unparsed command strings before `/run` dispatch.
- On rejection, force bounded repair prompt to produce executable command shape.

## Loop Guard

- Detect repeated low-value actions (`ls`/`list_dir` churn or equivalent) with no evidence delta.
- On threshold, force `pivot_strategy` or classify as `no_progress`.

## Validation (Required)

Deterministic tests:
- command-shape validator rejects prose-as-command.
- completion validator rejects `objective_met=true` without evidence refs.
- repeated low-value no-evidence streak triggers pivot/no-progress.

Live qwen3.5 tests:
- `secret.zip` scenario: pass `>=5/5`, zero prose-command exec failures.
- router `192.168.50.1` smoke: evidence-backed findings or explicit `objective_not_met`, never false-success report.

## Rollout Plan

1. Implement and validate in CLI only.
2. Confirm Sprint 36 Phase 8 gate items tied to PR-011 are green.
3. Prepare orchestrator worker adoption plan using same contract, after CLI gate passes.
