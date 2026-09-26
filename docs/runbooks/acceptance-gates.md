# Acceptance gates

Updated 2026-09-19. Gates distinguish implemented contract checks from future product acceptance.

## Current core cleanup gate

Required deterministic checks:

- Literal argv survives spaces/quotes/metacharacters; shell mode is explicit.
- Approval matches the executed invocation and cwd.
- Cancellation terminates the owned process group and finalizes an aborted session before UI exit.
- Output is available during execution; previews are bounded and full artifacts are retained.
- Unrelated output and evaluator failure cannot automatically establish completion.
- Prior-task failures do not become new-task execution truth.
- Private IPs and local paths do not automatically establish authorization scope.
- Missing behavior rules are rejected even when the goal is valid.
- Session persistence and the existing behavior suite continue to pass.

Run `./scripts/ci.sh` and `go test -race ./...`. Run repeated focused real-model checks and inspect their actual session JSON, context snapshots, command logs, and output artifacts. Record model ID, fixture, run directory, and limitations. A literal-output check is evidence for the execution contract, not pentest effectiveness.

## Recorded core validation — 2026-09-19

Deterministic CI and race checks passed. Local model `lmstudio-community/qwen3.5-27b` completed 3/3 explicit-shell and 3/3 direct-argv literal-output checks. Context, evaluator output, final state, full output artifacts, and clean fixture were inspected. Local evidence: `sessions/core-live-20260919-7eLNPJ/`. Broader lab/product gates below remain unvalidated by this slice.

## Subscription wrapper gate — first slice passed

Prove a live authenticated structured request through the selected backend and a harmless end-to-end worker task. Verify that the provider does not execute tools independently. Test cancellation and deterministic fixtures for expired authentication, exhausted limits, and unsupported models. Keep credentials out of evidence and never silently switch billing modes. Do not deliberately exhaust an account to test rate limits.

Recorded 2026-09-19: deterministic CI and affected-package race checks passed. After correcting the initial stream parser probe, 3/3 live worker trials through `gpt-daybreak-blue-latest` completed with exactly one local argv-mode printf invocation each. Final contexts, semantic evaluations, saved state, and full output were inspected; the fixture remained empty and credentials were absent from session artifacts. Local evidence: `sessions/subscription-live-20260919/`. The backend reported resolved model `gpt-5.6-sol`. Expiry, limits, and refresh behavior use deterministic fixtures; this is not a pentest acceptance result. See [setup and compatibility limits](subscription-bridge.md).

## Agentic worker foundation gate — current priority

Before further coordinator or knowledge expansion, demonstrate one shared worker carrying a meaningful bounded task through goal interpretation, planning when needed, approved actions, observation, adaptation, and evidence-backed completion. A recoverable obstacle must permit a model-directed change of approach within the original scope and remaining budget; a missing prerequisite, denied permission, exhausted budget, or unavailable evaluator must not be converted into success or permission to evade a boundary.

The 2026-09-20 rebuild implements one adaptive decision loop, explicit plan revisions with history, whole-goal evaluation, bounded context views, and persisted turn budgets. Ensure standalone diagnosis and delegated execution also expose the same explicit provider settings, including the user-confirmed Qwen profile. A coordinator supplies a bounded task to the shared loop; the worker chooses and revises its own plan.

Combine focused deterministic checks with three real-model runs per selected acceptance scenario and inspect actual commands, observations, context snapshots, state transitions, completion claims, and remaining budgets. Exercise operator questions, denial, stop, and provider errors through the application where relevant; reuse existing valid checks. Record Daybreak and Qwen results separately. A single-command smoke check, synthetic coordinator success, or deliberately interrupted run cannot close this gate. Strict customer scope isolation and whole-assessment resume remain additional product gates.

## Orchestrator gate — structure implemented; capability acceptance ongoing

Controlled worker-recovery evidence from 2026-09-20 is documented in [the worker audit](../worker-foundation-audit-2026-09-20.md): three consecutive, independently inspected Daybreak passes after correcting plan-history loss and ambiguous multiline evidence rendering. Earlier false-completion results are retained as failures. This is a focused recovery fixture, not generic pentest capability acceptance. The Qwen diagnostic remains unaccepted.

The generic orchestrator structure is implemented and exercised by the built application. Its deterministic orchestration scenario runs two independent tasks, preserves separate workspaces and inherited scope, accounts for one shared model-call budget, then schedules a dependent validation task and produces a completed report. This proves the coordination contracts and user path; it does not prove discovery quality or independent finding correctness.

Use a clean fixture with two independent investigation branches and dependent validation/reporting work. At least three repeated runs must preserve task/evidence ownership, demonstrate bounded concurrency, honor dependencies and budgets, stop workers cleanly, and produce a reviewable report. Scope enforcement and resumed external effects need explicit tests before customer use.

Include discovered software that triggers advisory research and a new delegated validation task. Inspect the source references, applicability reasoning, visible plan change, worker scope/budget, and final evidence. Include an advisory that does not apply and a failed lookup: neither may become a confirmed finding or a claim that the target is secure. Use deterministic advisory fixtures for repeatable matching checks. Validate permitted live research in connected mode and local snapshot research in air-gapped mode.

## Recorded guided fixture validation — Daybreak workflow passed

On 2026-09-19, after preserving and correcting failed diagnostic runs, three consecutive runs on the unchanged build completed through the actual guided terminal application using `gpt-daybreak-blue-latest`. They used 15, 21, and 17 model calls, each completing four workers and a report with the expected unique defect, fixed control, non-applicable advisory, and unavailable-reference result. The last run recovered from an ordinary shell syntax error without a runtime workaround.

Exact commands, full responses, saved contexts, inherited scope, registered citations, dependent validation, and final reports were inspected. Deterministic CI includes nine terminal scenarios, including the generic orchestration path; affected-package race checks passed. Temporary subscription credentials were removed on exit. Evidence: `sessions/guided-live-20260919-r5vjg4v3/daybreak-lab/`, with review results in the parent directory's `completed-daybreak-reviews.json`. Earlier failures remain available and are not counted as passes.

This passes the bounded synthetic workflow check with existing subscription sign-in. Customer scope enforcement, whole-assessment resume, unfamiliar-operator usability, connected live research, comparative discovery, and air-gapped acceptance remain open. Qwen is recorded separately below.

## Current local-model pilot — incomplete

This historical 2026-09-19 run used Qwen 3.8 27B Q6_K, reasoning low, server context 50,176, and two parallel slots. The user changed the server setting to approximately 70k tokens on 2026-09-26; this older result does not validate that setting. Guided local calls request 32,768 output tokens and allow ten minutes per request. Initial 90-second planning attempts timed out. With the larger allowance, four workers completed and captured the seeded admin issue and fixed control, but the coordinator omitted validation dependency links and the requested unavailable-reference GET; the final report was rejected. This run began before the explicit evidence catalog and accurate step countdown were added. It is not local-model acceptance and was not a refusal test. Retain it and repeat on the current contract before accepting the local configuration. Local review: `sessions/guided-live-20260919-r5vjg4v3/qwen38-02-review.json`.

## Usability gate — required for the primary product surface

From an installed application, an operator unfamiliar with its implementation must be able to launch `birdhackbot` without flags, complete guided provider setup, define a harmless scoped task, review its permissions, start it, stop it, and find its results. No source editing, hand-built flag list, manual token-file handling, or separately launched bridge should be needed.

Check that the operator can identify the active targets and permission mode and understand a proposed action before approving it. Exercise a missing sign-in or unavailable provider and verify that the application offers an understandable recovery step. A subsequent launch should reuse provider preferences while keeping the assessment scope and permissions explicit. Advanced CLI and guided operation must preserve the same execution and evidence guarantees.

Record where the operator needed outside help, misinterpreted scope or permissions, or could not stop/recover. Correct those observed problems before claiming this gate passed. This gate is currently unvalidated; passing transport or worker tests does not establish usability.

Application checks are a continuing development requirement. CI now runs nine terminal scenarios against the actual binary and a deterministic model server, covering first-use setup, saved-provider startup, action approval, denial without execution, provider-configuration recovery, cancellation before start, Ctrl-C during a child command, answering a worker question, recovery with a visible plan revision after a failed command, and generic orchestration with dependent validation. It verifies configured local reasoning/output settings on coordinator, worker, and evaluator requests. These checks complement real-model runs; they do not substitute for an unfamiliar operator's usability assessment.

## Source-assisted gate — partially exercised

Run at least three trials each against a vulnerable fixture, fixed counterpart, mismatched source revision, and unavailable-source case. Require attributable pinned source and actual target evidence before promoting a finding. Record hypotheses and inconclusive outcomes separately.

Recorded 2026-09-19: two guided Daybreak runs against `testdata/source-lab/` passed inspection of the seeded tenant defect, fixed API control, authentication controls, deployed revision versus default HEAD, actual responses, contexts, dependencies, and reports. The third run was deliberately stopped during conversation troubleshooting and remains incomplete. The fixture and its expectations were frozen before inference; no scenario-specific runtime changes were added. Evidence and reviews are under `sessions/assessment-next-drlavi6_/`. Unavailable-source and broader acquisition cases remain open, so this gate has not passed. The concurrent Qwen retest was also deliberately stopped and does not establish local-model acceptance.

## Comparative value gate — planned

Freeze fixtures, prompts, versions, scoring, budgets, and the minimum useful improvement before measuring. Start with the first working assessment fixture, then add known-CVE, configuration, authorization/business-logic, and source-assisted scenarios, including fixed controls and held-out variants not used to tune the harness. Fixture truth must be maintained independently of the assessing agent; on open-ended targets, report verified findings without pretending total vulnerability recall is known.

Compare against Codex with its normal capabilities available and a relevant specialist competitor from the [competitive assessment](../competitive-assessment-2026-09-19.md). Match model, Kali/tool access, credentials, source access, initial knowledge, and aggregate budgets where supported; disclose unavoidable differences. Separate harness comparisons using the same model from complete-product comparisons using each product's supported configuration.

Count unique, reproducible, independently reviewed vulnerabilities and missed known defects. Deduplicate repeated symptoms of the same root cause; CVE matches and unvalidated hypotheses do not count. Record false claims, finding impact, target side effects, analyst interventions, elapsed time, and aggregate model usage. Blind reviewers to the producing system where practical. An apparent gain from more false claims, larger budgets, or repeated reports is not sufficient evidence of better discovery.

Use at least three trials per configuration/fixture for initial comparisons, retain failed runs, and show variation. A small pilot supports only a narrow claim; broader superiority needs broader held-out evidence. Measure connected and air-gapped configurations separately, with local-model competitors where available. Remove playbook retrieval, local vulnerability research, or reusable helpers one at a time in otherwise matched runs to test which capabilities contribute. Keep reusable-tool history controlled and disclose it; do not leak held-out answers through the tool catalog.

## Air-gapped gate — required, unvalidated

From a clean application profile and an explicitly provisioned offline environment, complete setup, discovery, local advisory research, planning/delegation, bounded validation, reporting, stop, and resume with external connectivity blocked throughout. All model roles must use local inference. Include creation of one small helper with locally available dependencies and its successful reuse in a second suitable fixture.

Observe application and child-process network attempts, including DNS, alongside the enforced network boundary. Require no attempted connections outside approved targets and local support services, and no successful external egress. A denied cloud request still fails the no-fallback behavior requirement. Record environment/tool/model versions, supplied dependencies, corpus revisions/dates, and network evidence.

Exercise unavailable local inference, a missing dependency, and an absent advisory/source: show actionable local errors or coverage gaps without contacting a cloud service, fetching packages, or declaring the target secure. Confirm that the report exposes snapshot freshness. This gate does not require equal findings across different local and cloud models; discovery quality is measured separately by the comparative gate.

## Generic capability gates

Use a rotating set of authorized synthetic or customer-like fixtures. Do not make a named target or legacy ZIP/router task a product benchmark. Each capability trial must declare scope, allowed actions, a done condition, evidence requirements, and out-of-scope behavior. Cover, as applicable, software discovery, service enumeration, authentication and authorization, configuration, application behavior, source-assisted investigation, advisory applicability, recovery from failed assumptions, blocked prerequisites, cancellation, and report reproducibility. Compare against matched baselines and record missed defects, false claims, operator effort, target effects, time, and model usage.

The previous detailed gate definitions and results remain in `docs/archive/pre-core-cleanup-2026-09-19/acceptance-gates.md`. Focused smoke checks do not pass these broader gates.

## Evidence record

For each claimed result, retain the run/session IDs, model/backend, fixture/start state, prompt, exit/status, actual invocation and cwd, context snapshots, output artifacts, and any report. Record scope, limitations, and failed runs. Keep raw session evidence local in ignored directories.
