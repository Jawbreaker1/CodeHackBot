# Acceptance Gates

Date: 2026-03-20  
Status: Active (rebuild control doc)

## Purpose

Keep rebuild-phase reliability decisions tied to measurable gates instead of subjective impressions.

## Metrics

- `answer_success`: user-visible objective achieved.
- `contract_success`: terminal completion contract emitted and evidence-backed.
- `avg_steps_to_complete`: mean execution steps for successful runs.
- `interrupt_integrity`: interrupted/stopped runs still emit coherent terminal report/status.

## Active Gates

1. ZIP reliability gate
- Scenario: `secret.zip` recovery in the local rebuild fixture.
- Pass: `answer_success >= 5/5` and `contract_success >= 5/5`.
- Required evidence: logs + final artifact references under `sessions/<id>/`.

2. Router smoke gate (lab only)
- Scenario: `192.168.50.1` lab recon/validation.
- Pass: evidence-backed findings or explicit `objective_not_met`.
- Required evidence: scan logs + report artifact with validated claims only.

3. Interrupt terminalization gate
- Scenario: Ctrl-C / stop event during active run.
- Pass: terminal report exists; run headline counters coherent (`active_workers=0`, `running_tasks=0`).

4. Cross-scenario anti-hardcoding gate
- Scenario: at least two live smokes with materially different task shapes than `secret.zip` (for example local file workflow vs scoped network workflow vs reporting-heavy workflow).
- Pass: runs make forward progress using the same generic planning/runtime contracts; no new scenario-specific rewrite path is introduced to satisfy only one scenario.
- Required evidence: run ids, reports, and rationale in `DISCOVERIES.md`.

## Evidence Checklist

- Run/session id
- Terminal status snapshot
- Context packet snapshots inspected for the run
- Persisted session state inspected for the run
- Completion contract fields
- Artifact/log paths used as proof
- Final report path

## Validation Discipline

- The canonical live scenarios for current acceptance work are:
  - `secret.zip`
  - `192.168.50.1`
- Use repeated live runs for behavioral conclusions because model behavior is non-deterministic.
- Default to 3 runs per scenario unless the check is explicitly labeled as smoke-only.
- Smoke/debug runs are useful for development, but they do not satisfy acceptance evidence for the canonical scenarios.
- A run does not count as validated if context snapshots or persisted session state are misleading, stale, or incomplete.

## Change Control

When any gate threshold or metric definition changes, update:
- this file
- `TASKS.md` phase exit criteria
- `DISCOVERIES.md` with rationale
