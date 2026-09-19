# Acceptance Gates

Date: 2026-09-19

Status: Active (rebuild control doc)

## Purpose

Keep rebuild-phase reliability decisions tied to measurable gates instead of subjective impressions.

## Metrics

- `answer_success`: user-visible objective achieved.
- `contract_success`: terminal completion contract emitted and evidence-backed.
- `avg_steps_to_complete`: mean execution steps for successful runs.
- `interrupt_integrity`: interrupted/stopped runs still emit coherent terminal report/status.
- `validated_findings`: findings confirmed against independent fixture truth with reproducible target evidence.
- `unsupported_claims`: promoted findings or access claims that lack required evidence or contradict fixture truth.
- `operator_interventions`: human corrections needed beyond initial setup and required approvals; record approvals separately.
- `wall_time` and `model_usage`: total assessment time and aggregate usage across all workers, including unsuccessful runs and validation.

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

## Orchestrator Product Gates (Not Yet Validated)

5. Orchestrated assessment gate — Phase 4
- Scenario: clean lab fixture with two independent investigation branches and a dependent validation/reporting task, using the orchestrator operator path.
- Pass: at least 3/3 runs preserve task boundaries, demonstrate actual concurrent work, honor dependencies/conflicts and the configured budget, and produce evidence-backed reports. A zero-exit command alone must not satisfy a task objective.
- Stop/resume check: stop during active work leaves no live worker child processes, persists an interrupted report, and resumes without repeating completed actions. Exercise at least one LLM wait and one child-process execution.
- Required evidence: run/task events, worker contexts and state, execution/artifact references, validation decisions, and final report.

6. Subscription REST bridge gate — Phase 4
- Scenario: authenticated local client uses the selected subscription backend for BirdHackBot's structured inference requests, without independent provider-side tool execution.
- Pass: live login, request, streaming, and cancellation work; worker contexts remain isolated; credentials are absent from evidence. Compatibility checks cover unsupported models, expired authentication, exhausted limits, and concurrent requests. These failure cases may use deterministic provider fixtures and must not deliberately exhaust an account.
- Required behavior: limit/auth failures are visible and coherent; no silent API-key fallback or credit purchase. BirdHackBot retains task and execution ownership. A separate delegated-agent integration alone does not pass the inference bridge gate.
- Required evidence: redacted integration results, backend/version/capability record, and explicit billing mode. This gate does not assert unlimited use or universal account eligibility.

7. Source-to-target validation gate — Phase 5
- Scenario: independently specified vulnerable fixture, fixed counterpart, mismatched source revision, and unavailable-source case.
- Pass: at least 3 runs per case. The vulnerable fixture yields a reproducible target-validated finding with pinned source references; other cases produce no unsupported validated finding. Version uncertainty and source availability are reported honestly.
- Required evidence: observed identity, repository origin/revision, code references, preconditions, separate local/target validation results, and report. A candidate found in source is not sufficient proof.

8. Comparative value gate — Phase 5
- Baseline: Codex with its normal tooling and delegation available; match model access, target snapshots, source access, credentials, tools, and aggregate time/usage budgets where possible. Disclose unavoidable differences.
- Before running: freeze the fixture set, prompts, versions, scoring rules, repetition count, and the minimum improvement being tested. Use at least 3 trials per configuration/fixture; increase repeats when variance prevents a conclusion.
- Pass: demonstrate the preregistered improvement in confirmed assessment outcomes or operator effort without increased unsupported claims or exceeding matched budgets. Otherwise record the result as unproven, including regressions and failed trials.
- Required evidence: independently scored outcomes, complete run identifiers, aggregate usage, timings, interventions, and protocol. Do not claim superiority from a selected successful run, a model upgrade alone, or a larger agent count.

## Evidence Checklist

- Run/session id
- Terminal status snapshot
- Context packet snapshots inspected for the run
- Persisted session state inspected for the run
- Completion contract fields
- Artifact/log paths used as proof
- Final report path
- Source revision and target-match evidence when applicable
- Provider/backend version, model settings, and aggregate usage when comparing configurations

## Validation Discipline

- The canonical live scenarios for current acceptance work are:
  - `secret.zip`
  - `192.168.50.1`
- Use repeated live runs for behavioral conclusions because model behavior is non-deterministic.
- Default to 3 runs per scenario unless the check is explicitly labeled as smoke-only.
- A gate-specific repetition requirement overrides that default (the existing ZIP gate still requires 5 runs).
- Smoke/debug runs are useful for development, but they do not satisfy acceptance evidence for the canonical scenarios.
- A run does not count as validated if context snapshots or persisted session state are misleading, stale, or incomplete.

## Change Control

When any gate threshold or metric definition changes, update:
- this file
- `TASKS.md` phase exit criteria
- `DISCOVERIES.md` with rationale
