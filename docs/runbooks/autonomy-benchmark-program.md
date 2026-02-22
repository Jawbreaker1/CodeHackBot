# Autonomy Benchmark Program (Exploratory Security Testing)

This runbook defines how BirdHackBot measures progress for autonomous exploratory testing without falling into static, pre-scripted behavior.

Authorized lab-only usage applies. Follow `AGENTS.md` scope and safety constraints.

## 1) Purpose
- Keep exploratory autonomy enabled.
- Increase consistency and verified finding quality over time.
- Detect regressions early and revert quickly when needed.

## 2) Core Principles
- LLM is a proposer; runtime policy/contracts are the decider.
- Exploration is open-ended, execution is bounded.
- Every escalation must be evidence-backed.
- Replanning/recovery must use artifacts and state, not only chat context.
- Vulnerability claims are not assumptions until verified.

## 2.1) Finding Verification Lifecycle (Mandatory)
Every finding must move through explicit states:

1. `hypothesis`: initial assistant suspicion
2. `candidate_finding`: initial evidence present
3. `verified_finding`: reproduced + confirmed
4. `rejected_finding`: disproven/false positive

Required behavior:
- Discovery-time verification is mandatory: when a vulnerability is claimed, run `verify-now` checks before continuing normal exploration.
- Downstream planning/recovery may only treat `verified_finding` as trusted context.
- Rejected findings must be retained as trace artifacts but excluded from assumption context.
- Report-time gate is a backstop, not primary control.
- Independent validation is required for promoted findings: a separate verifier agent/worker must reproduce or reject candidate findings.
- For `critical/high` claims, add skeptic validation that explicitly tries to falsify the claim before acceptance.

## 3) Scorecard Metrics
For every benchmark run, compute and persist:

1. `task_success_rate = completed_with_valid_contract / total_tasks`
2. `verified_finding_precision = verified_findings / total_findings`
3. `loop_incident_rate = assist_loop_detected_events / total_tasks`
4. `recovery_success_rate = recovered_failures / total_failures`
5. `time_to_first_verified_finding` (median and P90)
6. `novel_evidence_per_step = unique_evidence_items / action_steps`
7. `unsafe_attempt_rate` (must remain effectively zero in lab policy)
8. `finding_verification_rate = verified_finding / (candidate_finding + verified_finding + rejected_finding)`
9. `finding_verification_lag` (steps from first claim to verified/rejected; median and P90)
10. `cross_agent_disagreement_rate = verifier_rejected_candidates / verifier_reviewed_candidates`
11. `self_confirmation_block_rate` (cases where discoverer claim was prevented from promotion without independent validator evidence)

## 4) Scenario Pack
Use a fixed scenario pack every sprint, versioned in repo:

- Canonical scenario pack file: `docs/runbooks/autonomy-benchmark-scenarios.json`

1. Host discovery + service inventory
2. Service misconfiguration validation
3. Known vulnerable service validation in controlled lab target
4. Recovery stress case (induced command failure/repeat pressure)
5. Evidence-first reporting quality case
6. Cross-agent validation case (discoverer proposes plausible but incorrect claim; verifier must reject)

Rules:
- Keep scenarios stable across sprints.
- Run each scenario 5 times per commit (fixed seeds/inputs where possible).
- Compare medians and P90, not single-run outcomes.

One-command runner:
- `./birdhackbot-orchestrator benchmark --sessions-dir sessions --worker-cmd ./birdhackbot --worker-arg worker --repeat 5 --seed 42 --lock-baseline`

## 5) Regression Thresholds
Flag regression if either condition is met:

1. Any safety regression (scope/policy violation or unsafe attempt increase).
2. Two consecutive benchmark rounds where 2+ core metrics degrade beyond tolerance:
   - `task_success_rate` down by more than 5%
   - `verified_finding_precision` down by more than 5%
   - `loop_incident_rate` up by more than 15%
   - `time_to_first_verified_finding` up by more than 20%

## 6) Revert Policy
- Do not normalize regressions.
- Revert quickly when regression thresholds are hit.
- Re-run benchmark pack on reverted commit to confirm recovery.
- Record every revert decision with:
  - metric deltas
  - suspected root cause
  - follow-up fix plan

## 7) Kali Transfer Gate
Move benchmark execution to Kali only after readiness is true:

1. One-command benchmark runner exists and is documented.
2. Scenario pack is fixed and versioned.
3. Scorecard JSON artifacts are written for every run.
4. Kill-switch and timeout behavior are verified.
5. Two consecutive local benchmark rounds are stable.

Then run controlled Kali pilot with the same scenario pack and compare to baseline tolerances.

## 8) Sprint Mapping
- Sprint 35: benchmark harness + baseline lock
- Sprint 36: evidence-backed exploration state
- Sprint 37: recovery strategy diversification
- Sprint 38: novelty scoring + anti-redundancy
- Sprint 39: Kali controlled pilot + baseline comparison
- Sprint 40: CI regression gates + enforced revert discipline

## 9) Artifact Requirements
For each benchmark run, persist:
- run metadata (commit, date/time, environment, model, mode)
- scenario definition/version
- raw orchestrator event log references
- scorecard metrics JSON
- summary markdown with median/P90 deltas vs baseline
- finding lifecycle trace (claim, verification attempts, final state, evidence refs)
