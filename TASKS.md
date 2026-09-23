# Tasks

Updated 2026-09-23. This file owns immediate implementation order and status.

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

Done means the worker and then the coordinator pass their explicit foundation gates in `docs/runbooks/acceptance-gates.md`. Preserve the useful existing coordinator; further knowledge, playbook, and source integration waits for these gates. Customer scope isolation and wider product acceptance remain separate explicit requirements; the web session layer now provides explicit persisted discovery and resume, while exact-once external-effect recovery remains deferred.

The dated [worker audit](docs/worker-foundation-audit-2026-09-20.md) records removed logic, replacement contracts and live-validation limits. The Qwen guided diagnostic split a requested single-worker task and omitted a line from its final content summary; it does not pass worker acceptance despite completing its assignments.

The [2026-09-23 code assessment](docs/code-assessment-2026-09-23.md) found five reproducible defects in model-call accounting, live conversation context, worker-question routing, finding revisions, and web-session persistence. All five are repaired with permanent regressions. Full deterministic CI, affected-package race tests, and a focused live browser path passed after the repairs. This closes those defects, not the broader foundation or customer deployment gates. Next, extract web lifecycle/model-request ownership along actual boundaries and reconcile the previously tracked final-status mismatch after a failed worker is recovered in a later round.

The 2026-09-23 Daybreak Blue browser assessment of the authorized router at `192.168.50.1` is an **incomplete baseline**, not a platform acceptance pass. From a high-level brief, the coordinator delegated eight tasks across four rounds, identified the ASUS GT-BE98, and preserved scan and research evidence. Three research workers hit the ten-decision limit, including both final advisory/CVE workers. The resulting report recorded only informational fingerprint disclosure; independent evidence from the same LAN vantage also supports HTTP-only administration and a UPnP description service that BirdHackBot missed. Investigate generic worker budget sizing, service-coverage recovery, and evidence-backed finding prioritization, then repeat the same brief and compare claims and omissions. Do not add router-specific workflow logic.

A fresh high-level Daybreak Blue browser run after generic planning guidance and a 16-decision worker default completed eight worker tasks in four rounds without blocked workers. It independently recorded a medium-severity HTTP-only administration finding, corroborated by closed TCP/8443 and disabled HTTPS redirect, and separated firmware, DNS, and TCP/7788 hypotheses from confirmed target behavior. The prior UPnP listener was absent in this point-in-time full TCP scan, so this run cannot establish whether the harness would now detect it when exposed. The completed session took 61 minutes and 105 model calls (about 1.67 million reported tokens), which is too slow for the intended interactive workflow. Further acceptance needs repeated runs and independently reviewed precision/recall on more targets, plus lower latency and cost without narrowing the generic assessment capability.

Completed web assessments now accept post-run coordinator questions and can generate OWASP WSTG- or PTES-aligned Markdown drafts from deterministic templates, saved and linked as separate artifacts. The report structures are implemented; formatting and evidence meaning still require professional review, and PDF/DOCX export remains a later slice.

A 2026-09-23 Daybreak browser smoke of an operator-authorized public site exposed excessive intake demands: the coordinator asked for a formal RoE, window, escalation contact, and record path before a low-impact inspection. Intake now accepts the operator's authorization statement and reviewed exact-target scope for that tier; the CLI uses the same rule. The live run produced bounded DNS, HTTP/HTTPS, and TLS evidence plus a cautious incomplete report. It also exposed a worker proposing an unselected sibling service check; that action was denied, and skipped task IDs/goals now enter the selected worker's context with a focused fixture regression. Repeat live validation on the updated worker build remains open. The runtime still lacks structural network-scope and deadline enforcement, so the smoke does not establish customer deployment readiness.

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
- [x] Require operator selection of proposed web test tasks before execution; retain approved/skipped choices in the assessment plan and report.
- [x] Preserve structured advisory/CVE references and observed software on findings, with transparent severity/confidence prioritization in a separate web analysis view.
- [x] Include selected test sequence, advisory references, evidence register, remediation, and assessment gaps in the formal Markdown draft report.
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

2026-09-23 tool clarity: the shared model behavior frame carries a phase-by-phase tool catalog. Intake proposes scoped worker tasks for commands or file changes instead of claiming the whole harness is read-only. The local Qwen profile requests JSON-schema object output for structured control decisions because a live prose-only answer failed the intake contract. After operator feedback, the worker uses the general `bash` execution decision for both direct commands and Bash scripts; the short-lived `delete_file` decision was removed. Approval, exact invocation logging, evidence, and per-item plan review remain on the shared execution path. The runtime does not structurally enforce a request to sequence several changes if a model ignores its planning instruction; plan review remains the operator checkpoint.

Follow-up tool-list and execution smoke: the user observed that a coordinator answer named Kali programs without listing the worker operations. Intake and active coordinator-chat instructions now require named harness operations when asked about tooling. In a fresh Qwen 3.8 GUI conversation, the coordinator listed `bash`, `load_strategy`, `update_plan`, `ask_user`, `step_complete`, and `blocked` separately from direct intake observations and installed Kali programs. A scoped one-file worker used `bash` for a read-only existence check and then presented the exact `rm` invocation as dangerous in the main chat; denying it left both fixture files untouched. A focused integration test covers an approved one-file removal. These are smoke checks of explanation and approval flow, not pentest effectiveness or completeness of installed-tool discovery.

The web model picker now uses named provider profiles when `config/model-profiles.local.json` is present. Daybreak and Qwen can be selected for separate sessions without changing command-line flags; the selected endpoint and request limits persist with each session. Browser validation confirmed both directions of the model switch and live replies from both configured endpoints. Comparison quality still needs an evaluation set with the same tasks and scoring rules.

2026-09-23 first slice: a small, versioned strategy catalog is now included in the coordinator and worker behavior frames. The coordinator can label a bounded task round `research` before proposing tests, and may revisit research after discoveries; the model decides when this is useful. Workers select individual guides through a structured `load_strategy` decision, retaining their source, checksum, and content in the worker packet across later turns. This does not establish good strategy selection, current CVE coverage, or agentic recovery performance; live end-user validation is still required. The archive baseline in `docs/experiments/codex-archive-baseline-2026-09-23.md` shows a missed standard John rule despite an otherwise capable tool environment.

2026-09-23 catalog expansion: twelve concise, distinct high-level guides are indexed for coordinator and worker use. The coordinator may include up to two optional guide paths in each task, passed to the worker and shown in the plan; the worker still chooses what to load through `load_strategy`. Add or revise guides from observed task failures and evaluation evidence, rather than adding a long generic checklist. Selection quality and effect on outcomes still need live validation across several unlike targets.

Daybreak GUI smoke on the updated web build: for a read-only review of `testdata/source-lab`, the coordinator suggested `source-review` plus `identity-access` for one worker and `source-review` plus `api-assessment` for another. The operator selected one worker. Its saved context and visible activity show it loaded `source-review`, then completed a scoped static source trace with three approved read-only commands. The coordinator proposed a separate source-level validation task, left at plan review. This demonstrates guide selection and handoff on one known synthetic fixture, not independent validation of the candidate or general selection quality.

2026-09-23 diagnostic slice: the loopback web app now exposes coordinator and worker context turns as ordered sections with byte sizes and full model messages for newly recorded worker decisions. Optional worker sections can be omitted from future model projections and restored without changing durable evidence. In an initial Daybreak GUI smoke, the worker selected and retained the credential guide, but a later model call exceeded the subscription client's 90-second deadline. The coordinator identified the infrastructure failure and replanned; the client deadline now exceeds the bridge's three-minute inference budget. A fresh GUI run on the updated build completed recovery and independent validation of the local archive without displaying the secret. The coordinator recognized an invalid compressed-wordlist attempt, ran corrected plain and focused-mutation searches in parallel, then selected a broader installed rule campaign after both missed. The successful campaign took 14.5 seconds, while the whole approved five-plan, 47-call session took about 36 minutes. This is one end-to-end success, not repeatability or comparative acceptance. Detailed trace: `docs/experiments/strategy-context-smoke-2026-09-23.md`.

- [ ] Reduce model-call and review overhead on simple recovery tasks using the recorded context/decision traces; preserve separate validation, meaningful approvals, and adaptive replanning. Measure improvements on unchanged prompts and additional targets rather than adding archive-specific rules.

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
- [x] Add an in-application model settings flow, guided `/resume` selection, and coordinator conversation while delegated workers are active. Resume preserves saved results and model-call budgets and does not replay unknown actions.
- [ ] Complete the usability acceptance check with an operator unfamiliar with the implementation.

## Required UI surfaces: terminal and web

The CLI and browser are presentation layers over the same orchestrator and evidence contracts. Do not create a second worker or assessment implementation for the web path.

- [x] Show coordinator planning, queued workers, dependencies, phases, budgets, evidence counts, approval waits, and terminal worker status in the guided terminal output.
- [x] Keep provider input budgets explicit: local Qwen 3.8 retains the conservative 48 KiB default, while the guided Daybreak profile uses a larger 128 KiB client ceiling and persists the value in assessment state.
- [x] Keep the terminal as a live coordinator surface: free-form operator messages, `/workers`, `/status`, `/help`, `/stop`, approval/question routing, and bounded conversation excerpts share the assessment runtime.
- [x] Reopen unfinished coordinator sessions from durable assessment state while retaining completed evidence, operator notes, and consumed model-call budgets. Exactly-once recovery of unknown external effects remains out of scope.
- [ ] Give finalized assessments with blocked exploratory workers a distinct runtime status so the terminal label and coordinator summary cannot disagree.
- [ ] Extract the guided assessment lifecycle from `guided.App` into an application service with explicit commands, read models, approval requests, and progress events.
- [ ] Keep Bubble Tea as the terminal adapter; test the CLI through the service boundary and evaluate the upstream v1-to-v2 migration separately.
- [x] Add the initial Go HTTP API with assessment create/read/start/stop, approval decisions, live events, and report access. Keep loopback binding as the default until authentication and remote deployment controls exist.
- [x] Build the first browser workflow against that API: customer/session scope review, provider visibility, approval, progress, stop, and report review. Verify it with the same deterministic assessment fixture used by the CLI, including two sessions aggregated into one customer report.
- [x] Add the optional pinned Playwright worker helper, preprovisioned system-browser contract, task-local trace/screenshot capture, and workspace-bounded registered artifact route.
- [x] Add durable web session discovery/resume with per-session transcript/model metadata and an explicit interrupted-run resume action.
- [x] Add explicit confirmed deletion for draft and finalized web sessions, including their linked intake metadata and local evidence directories.
- [ ] Add authentication, origin/CSRF protection, and streaming events before remote web deployment.
- [ ] Treat the local subscription bridge as an internal model-provider service, never as a browser-facing execution endpoint.

Implement this alongside the first orchestrator flow. Keep advanced CLI access and reuse existing runtime contracts; do not build a second execution path.

## Deferred product work

`ROADMAP.md` owns future direction. Full target-scope enforcement, exactly-once external-effect recovery, independent finding verification, and scored comparisons remain required product work.

Earlier phase checklists are archived in `docs/archive/pre-core-cleanup-2026-09-19/TASKS.md`. They are historical records, not additional current work orders.

## Current slice: approvals and observable browser execution

- [x] Three explicit session approval modes shared by web and terminal: every execution, dangerous/uncertain executions, and all executions allowed. Model-authored action descriptions drive risk presentation; command-text parsing does not classify risk.
- [x] Make either automatic web approval mode start the coordinator's scoped plans without a separate “Run selected” gate. Keep task selection in the default mode and retain dangerous/uncertain command prompts in dangerous-only mode. Built-app lifecycle checks and the GUI setting text were verified.
- [x] Compact main-chat approval cards with expandable invocation details, session permission picker, and CLI `/permissions`.
- [x] Optional worker watch view showing live output and declared browser preview; named Playwright steps and local trace evidence.
- [x] Project live worker observations into coordinator chat before task completion; show final conclusions in chat and analysis; remove historical resolved gaps from current analysis.
- [x] First Daybreak GUI browser smoke completed with a real click, registered PNG and trace; files independently inspected. This is a local harness smoke, not pentest-effectiveness acceptance.
- [x] Complete the follow-up Daybreak GUI smoke for revised approvals, watch view, progress replies, and persisted context/evidence. The browser interaction and independent artifact check passed after recovery; the whole session is not a clean acceptance pass.
- [ ] Reconcile final assessment status with recovered worker failures: the current runtime retains `incomplete` if an earlier worker blocked, even after a successful recovery and final coordinator conclusion. Keep the failed attempt in history; define completion versus remaining gaps in the shared coordinator contract.

Remaining foundation and comparative acceptance gates above still apply. The
watch view is observational, not interactive remote control of the target browser.

## Current slice: visible plans and active context

- [x] Project coordinator planning rounds and task outcomes into the right inspector; show worker step progress, model-authored step purposes, and previous plan revisions without making the UI a second planner.
- [x] Offload older worker execution bodies and superseded plan details from model requests before the input ceiling is reached. Keep full session records and log references locally; compact coordinator result cards while preserving the registered evidence catalog.
- [x] Verify the revised inspector and context snapshots in a new Astra browser session, including the coordinator's concluding plan revision and switching away from and back to the completed session. A separate broader source-trace diagnostic exhausted two workers' decision budgets and was stopped after its consolidation worker repeated reads; this remains a worker-quality failure to investigate, not an acceptance pass.
- [ ] Add relevance-based retrieval and independently checked long-run summaries only after a fixture demonstrates which lost fact the current projection fails to carry. Compare prompt growth and decision quality over multiple rounds; do not mistake a smaller byte count for reliable memory.

## Current slice: worker observation and context handoff

- [x] Correct the worker loop so each executed action returns its observed result to the deciding worker before completion is proposed. Run whole-goal evaluation for an explicit completion proposal, or as a final-budget fallback; never use an action's intended summary as the observed outcome.
- [x] Remove exact duplicate task-goal copies from the model view and shorten older execution bodies while retaining log references and lossless durable records. Give each worker detailed handoff cards for declared dependencies and a compact index for other prior tasks.
- [x] Pass the full CI suite, including guided terminal/TUI and web-app lifecycle checks, after updating fixtures for the revised decision flow.
- [x] Review two fresh Astra browser assessments against saved context and execution evidence. The first stopped after four completed search partitions with no password; a 2 KiB head-only triage conclusion hid an observed alternate source from later planning. The second used the packaged RockYou source directly, but its completion evaluator spent remaining decisions on an impossible hard aggregate-storage enforcement condition invented by the coordinator. Neither run extracted the archive. The second was then stopped at plan review to preserve the diagnostic state.
- [x] Independently check the earlier successful archive evidence without printing its secret: one archived candidate opens the current archive, but that candidate is absent from both packaged John and RockYou lists. John and fcrackzip recognize it when given as a single candidate. The completed RockYou search was a real negative for that source, not a broken converter. Do not use the old recovered value to seed a fresh harness run.
- [x] Retain worker conclusions long enough for late observed leads, compact repeated coordinator plan/log references, clarify public local corpus use, and keep negative tool results method-limited. Tell the coordinator not to assign unavailable hard filesystem/memory controls as worker done conditions; tell workers not to resubmit completion against unchanged evidence. Full CI passes.
- [x] Stop a third unsupported completion proposal when no new action or operator answer has changed the evidence after two evaluator rejections. Preserve the rejected feedback and return control to the coordinator instead of spending the worker's remaining calls in an evaluator loop.
- [x] Revalidate the revised worker completion and feasible resource-limit prompts in a fresh Astra browser run. The first worker finished a bounded negative search in 6/10 decisions; the coordinator used its result to propose a distinct RockYou prefix worker, which finished in 7/10 decisions. Both preserved the original archive. The live assessment is paused for operator review of a third proposed partition; access and extraction remain unproven.
- [x] Add a bounded coordinator planning projection: recent worker outcomes and finding-cited evidence stay visible, older workers become a task index, and the application checks the full role-specific request against its byte ceiling before inference. Keep durable results untouched.
- [ ] Validate the coordinator projection and worker packet under the configured Qwen 3.8 50k context/low-reasoning profile across multiple rounds; inspect real provider token usage, output/reasoning headroom, retained facts, and decision quality. A bounded synthetic inference reached the configured model with low reasoning and returned usage (58 prompt tokens, 47 completion tokens, including 35 reasoning tokens), but it is not an assessment-flow acceptance test. The 48 KiB input-byte ceiling is not a token count. Audit how resource bounds are actually enforced.
