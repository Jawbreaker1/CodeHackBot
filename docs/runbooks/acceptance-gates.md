# Acceptance Gates

Date: 2026-03-06  
Status: Active (Sprint 37 control doc)

## Purpose

Keep sprint reliability decisions tied to measurable gates instead of subjective impressions.

## Metrics

- `answer_success`: user-visible objective achieved.
- `contract_success`: terminal completion contract emitted and evidence-backed.
- `avg_steps_to_complete`: mean execution steps for successful runs.
- `interrupt_integrity`: interrupted/stopped runs still emit coherent terminal report/status.

## Sprint 37 Active Gates

1. ZIP reliability gate
- Scenario: `secret.zip` recovery in local scope.
- Pass: `answer_success >= 5/5` and `contract_success >= 5/5`.
- Required evidence: logs + final artifact references under `sessions/<id>/`.

2. Router smoke gate (lab only)
- Scenario: `192.168.50.1` scoped recon/validation.
- Pass: evidence-backed findings or explicit `objective_not_met`.
- Required evidence: scan logs + report artifact with validated claims only.

3. Interrupt terminalization gate
- Scenario: Ctrl-C / stop event during active run.
- Pass: terminal report exists; run headline counters coherent (`active_workers=0`, `running_tasks=0`).

4. Cross-scenario anti-hardcoding gate
- Scenario: at least two live smokes with materially different task shapes than `secret.zip` (for example local file workflow vs scoped network workflow vs reporting-heavy workflow).
- Pass: runs make forward progress using the same generic planning/runtime contracts; no new scenario-specific rewrite path is introduced to satisfy only one scenario.
- Required evidence: run ids, reports, and rationale in `docs/sprints/salvage2_discoveries.md`.

## Evidence Checklist

- Run/session id
- Terminal status snapshot
- Completion contract fields
- Artifact/log paths used as proof
- Final report path

## Change Control

When any gate threshold or metric definition changes, update:
- this file
- `TASKS.md` sprint exit criteria
- `docs/sprints/salvage2_discoveries.md` with rationale
