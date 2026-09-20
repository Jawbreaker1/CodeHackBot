# Tasks

Updated 2026-09-20. This file owns immediate implementation order and status.

## Agreed sequence

1. Get the existing core into a dependable state.
2. Build subscription-backed OpenAI inference through a local API wrapper.
3. Make orchestration the primary product surface.
4. Add source-assisted assessment and comparative evaluation.

The product remains orchestrator-first. The first two steps establish its shared engine and model access. Keep each implementation small, runnable, and tied to its done condition. Priority clarification, 2026-09-19: complete and accept the agentic worker first, then complete and accept the orchestrator structure, before expanding knowledge/source capabilities. Completed cleanup and provider checks do not establish full worker acceptance.

## Current priority: foundation acceptance and capability proof

Objective: prove one shared worker can carry a bounded multi-step task from goal to evidence-backed completion, adapt when observations invalidate its plan, and return an honest blocked/aborted result when appropriate.

- [x] Audit and replace competing worker control paths. One decision loop now owns model-authored plan changes, approved actions, questions, explicit blockers, and whole-goal completion. Removed keyword mode selection, startup-only planner, action reviewer, separate step judge, target/prerequisite inference, synthetic facts, failure ranking, and ambiguous response aliases.
- [x] Bound model views without rewriting persisted evidence; retain repeated execution identities, plan history, multiline answers, protected instructions and explicit truncation. Persist consumed budgets, stop on recording failure, and reject uncertain action replay. Version 1 state cannot be resumed.
- [x] Cover recovery and plan revision through the built terminal application; nine deterministic user-path cases now pass, including generic orchestration. Race testing identified and removed duplicate worker/UI progress writers.
- [x] Pass three consecutive Daybreak controlled-recovery runs after independent artifact review. Corrected missing plan history and ambiguous multiline evidence rendering; retained four preceding runs as failures. This is focused fixture validation, not canonical worker acceptance.
- [ ] Validate the full worker cycle with generic, meaningful multi-step assessment tasks covering discovery, evidence-producing actions, recovery, and a genuinely blocked case. Use synthetic or authorized customer-like fixtures selected for the capability under test; do not optimize around a named legacy target.
- [ ] Use the confirmed provider settings consistently in standalone diagnosis and delegated work. Qwen's low reasoning and larger output allowance currently exist in guided setup but are not exposed by the standalone development CLI.
- [ ] Inspect repeated real-model runs, persisted state, and actual evidence. Daybreak and Qwen results remain separately attributed; neither interrupted runs nor single-command checks establish acceptance.
- [x] Implement the orchestrator contract around that same engine: bounded assignments, dependencies, result handoff, adaptation to failed/blocked work, shared budgets, cancellation, and evidence-backed synthesis. The guided application now exercises two independent tasks followed by dependent validation with separate workspaces and a shared model-call budget.
- [ ] Validate the orchestrator with repeated real-model, generic multi-capability assessments and independently reviewed findings. Keep scope isolation, whole-assessment resume, and report verification as explicit product gates.

Done means the worker and then the coordinator pass their explicit foundation gates in `docs/runbooks/acceptance-gates.md`. Preserve the useful existing coordinator; further knowledge, playbook, and source integration waits for these gates. Customer scope isolation, full assessment resume, and wider product acceptance remain separate explicit requirements.

The dated [worker audit](docs/worker-foundation-audit-2026-09-20.md) records removed logic, replacement contracts and live-validation limits. The Qwen guided diagnostic split a requested single-worker task and omitted a line from its final content summary; it does not pass worker acceptance despite completing its assignments.

## Completed: core cleanup

Checkpoint: `checkpoint/pre-core-rebuild-2026-09-19` (`95edae1`). Implementation branch: `codex/core-foundation`.

Objective: correct demonstrated execution/evidence failures without rebuilding every proposed subsystem.

- [x] Replace implicit shell detection and command splitting with literal argv or explicit shell scripts.
- [x] Approve the prepared invocation and cwd; reject unknown approval decisions.
- [x] Stop owned Unix process groups on cancellation and wait for TUI worker finalization.
- [x] Stream tool output to local files, bound previews, and preserve execution metadata.
- [x] Require semantic completion evaluation; remove evaluator-error success fallback.
- [x] Remove severity-based replacement of current results and cross-task result carryover.
- [x] Remove automatic scope-step advancement based on private IPs/local paths; expose missing runtime scope enforcement honestly.
- [x] Validate required behavior fields independently of the goal.
- [x] Replace session snapshots atomically and record aborted state.
- [x] Repair repeat-run capture to use an explicit session directory per run.
- [x] Complete deterministic CI, race checks, and repeated local-model validation; inspect saved context and evidence.
- [x] Complete documentation/link/status review.

Done: focused regressions and CI pass, live checks use current artifacts and support only the claims made, and remaining product gaps are explicit. This does not require a new distributed runtime, complete policy engine, or broad plugin system.

## Completed: subscription API wrapper (first slice)

Objective: make one real subscription-backed structured model request usable by the existing worker while BirdHackBot retains tool execution.

- [x] Verify available authentication, backend request semantics, actual models, and inference-only behavior.
- [x] Implement the smallest local authenticated wrapper and worker adapter needed for that request.
- [x] Handle cancellation, expired authentication, exhausted limits, and clear billing mode without paid-API fallback.
- [x] Validate a harmless worker task end to end with the live subscription backend.
- [x] Document setup, supported models/backend limits, and credential handling from verified behavior.

Done: deterministic CI and affected-package race checks passed; 3/3 real subscription worker checks completed through `gpt-daybreak-blue-latest`, with local execution/context/evidence inspected. Setup, comparison sources, and compatibility limits are in `docs/runbooks/subscription-bridge.md`.

Defer native OAuth UI, broader REST surface area, and client streaming until needed. Orchestrator scheduling belongs to the next slice, not to the inference bridge.

## Implemented: first guided lab assessment

Objective: guide one scoped lab assessment from software discovery through advisory research and bounded validation to an evidence-backed result, using the existing worker engine.

The [competitive assessment](docs/competitive-assessment-2026-09-19.md) supports this bounded slice. Include traceable findings and visible assessment gaps, then evaluate targeted retesting and authenticated multi-role testing as following increments. Competitor feature lists are not an instruction to implement every feature now.

- [x] Introduce a coordinator with a visible plan that can change as evidence arrives; every delegated task uses the shared adaptive worker loop.
- [x] Delegate bounded tasks to two workers with inherited scope/permissions, separate workspaces, evidence references, shared call budgets, and explicit completion criteria. Workspace separation is not security isolation.
- [x] Support model-directed local advisory investigation and follow-up requests through ordinary approved actions. A dedicated corpus/research service and connected research validation remain deferred.
- [x] Route useful candidates to dependent validation and require recorded evidence for draft findings. Structural checks do not independently verify a finding.
- [x] Expose this flow through guided startup, visible progress, worker questions, per-action approvals, and broadcast stop.
- [x] Validate the complete flow on controlled fixtures through the built application: three corrected-build Daybreak runs passed, and the deterministic generic orchestration path now covers two independent tasks followed by dependent validation. Contexts, dependencies, actual output, reports, and cancellation were inspected. Local-model acceptance remains open below.
- [x] Freeze the first synthetic fixture's expected findings, negative controls, and research gap before running it.
- [ ] Validate Qwen 3.8 on the current task/report contract with low reasoning and the larger output budget; retain the failed pilot and require explicit validation dependencies and reference checks.
- [ ] Record a capable general-agent baseline, verified unique findings, misses, false claims, operator effort, target effects, and model usage.

Done for the first lab slice: three Daybreak assessments exercised discovery, local advisory lookup, delegation, dependent validation, and a reviewable result. Earlier failed diagnostics remain recorded. Qwen contract reliability, broader research integration, usability acceptance, and comparative scoring remain open; this is not customer or air-gap acceptance.

## In validation: source-assisted authenticated assessment

Objective: establish whether the existing orchestrator can correlate a deployed source revision, investigate an authorization issue using supplied synthetic accounts, and produce a target-backed finding without a scenario-specific runtime path.

- [x] Freeze a synthetic multi-tenant fixture with a vulnerable API, fixed counterpart, authentication controls, an old non-applicable advisory, and default source HEAD differing from deployment.
- [x] Add deterministic fixture checks to CI.
- [ ] Complete three unchanged-build guided Daybreak runs and inspect source provenance, dependent validation, actual responses, state, contexts, and report claims.

Current evidence: two completed Daybreak runs passed inspection. The third run and the current Qwen retest were deliberately stopped during conversation troubleshooting, preserving aborted reports; neither establishes an application failure or a passing assessment. The remaining Daybreak repeat and local-model acceptance are still open.

Fixture: `testdata/source-lab/`. Done means the single expected defect is reproduced and controls/source mismatch are handled correctly. This does not complete unavailable-source, external repository acquisition, full source correlation, or competitive acceptance.

## After worker and orchestrator acceptance: local assessment knowledge

Objective: expose installed Kali capabilities and attributable local research/playbook resources to the coordinator and workers without replacing their reasoning with a fixed tool chain.

Start with the existing Exploit-DB index and a reviewed active web/network playbook. Show resource paths, revision/freshness, and coverage limits; distinguish exploit references from comprehensive CVE coverage. Keep lookup on the same approved execution path and retain source references in evidence. Validate lookup/applicability through the guided application before bounded read-only capability fixtures. Do not add a new database service, external updater, or legacy planner.

## Required capabilities: Kali, reusable knowledge, and air-gapped operation

These are product requirements, not completed features or a request to build every subsystem at once. The architecture owns the contracts; the acceptance gates own proof.

- [ ] Start from the installed Kali tools and local corpora recorded in `DISCOVERIES.md`; verify availability, versions, and knowledge freshness on the actual assessment environment.
- [ ] Bring relevant playbook guidance into worker context and preserve its revision; avoid importing the legacy heuristic planner.
- [ ] Support creating, validating, and reusing a small versioned local helper with recorded dependencies and evidence. Begin with one demonstrated use case, not a plugin marketplace.
- [ ] Make the same orchestrator workflow usable with every model role local and research based on local snapshots. Add guided offline readiness and deliberate resource import; no cloud fallback.
- [ ] Pass the air-gapped gate under enforced network isolation before claiming air-gapped support.
- [ ] Compare discovery quality with a relevant specialist competitor, expand to held-out fixtures, and measure the contribution of playbooks, research, and reusable tools. Report connected and offline results separately.

## Required in the primary product surface: guided operation

User requirement, 2026-09-19: usability and application assistance must reduce setup mistakes and unsafe operation. The contract is in `docs/architecture.md`; the first guided lab flow implements part of it.

- [x] Open a guided application with `birdhackbot` and no mandatory flags; support provider setup and reuse of preferences from the checkout.
- [x] Manage bridge startup and temporary local credentials through the application for an existing sign-in.
- [ ] Complete first-time provider sign-in and packaged operation outside the checkout.
- [x] Guide goal/scope entry, review per-action permissions before starting, and show progress, stop, report location, and actionable errors.
- [ ] Complete the usability acceptance check with an operator unfamiliar with the implementation.

## Required UI surfaces: terminal and web

The CLI and browser are presentation layers over the same orchestrator and evidence contracts. Do not create a second worker or assessment implementation for the web path.

- [x] Show coordinator planning, queued workers, dependencies, phases, budgets, evidence counts, approval waits, and terminal worker status in the guided terminal output.
- [ ] Extract the guided assessment lifecycle from `guided.App` into an application service with explicit commands, read models, approval requests, and progress events.
- [ ] Keep Bubble Tea as the terminal adapter; test the CLI through the service boundary and evaluate the upstream v1-to-v2 migration separately.
- [ ] Add an authenticated Go HTTP API with assessment create/read/start/stop, approval decisions, live events, and report/evidence access. Keep loopback binding as the default until remote deployment controls exist.
- [ ] Build the first browser workflow against that API: scope review, provider/permission visibility, approval, progress, stop, and report review. Verify it with the same deterministic assessment fixture used by the CLI.
- [ ] Treat the local subscription bridge as an internal model-provider service, never as a browser-facing execution endpoint.

Implement this alongside the first orchestrator flow. Keep advanced CLI access and reuse existing runtime contracts; do not build a second execution path.

## Deferred product work

`ROADMAP.md` owns future direction. Full target-scope enforcement, assessment resume, independent finding verification, and scored comparisons remain required product work.

Earlier phase checklists are archived in `docs/archive/pre-core-cleanup-2026-09-19/TASKS.md`. They are historical records, not additional current work orders.
