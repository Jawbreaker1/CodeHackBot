# Rebuild Plan

This task list derives from `docs/architecture.md`.
The architecture document is the source of truth for the rebuild branch.

Planning rules:
- `TASKS.md` is the executable plan for the current phase and, optionally, the next phase.
- `ROADMAP.md` is directional only and must not contain detailed task lists.
- `DISCOVERIES.md` records lessons, risks, and future-phase notes; it is not a task list.
- If `TASKS.md` and `ROADMAP.md` differ, `TASKS.md` is authoritative for execution.

## Current Direction — 2026-09-19

Phase 4 is the active implementation priority: orchestrator-led assessments and subscription access. Phase 5 is the next product slice: source-assisted assessment. The standalone worker remains the shared execution foundation.

Phases 0-3 below preserve rebuild history and unfinished worker debt. Their unchecked items are not blanket prerequisites for orchestration; pull forward only the work required by Phase 4 contracts and acceptance. No new runtime capability is marked complete by this planning update.

## Phase 0 — Reset
- [x] Aggressively archive historical docs into `docs/archive/`
- [x] Reduce active docs to the minimal set
- [x] Move the pre-rebuild implementation into `legacy/` so old and new code cannot be confused
- [x] Write and freeze the v1 baseline architecture document with user review before adoption
- [x] Rewrite `README.md` to match the rebuild branch state

Exit criteria:
- architecture frozen at `docs/architecture.md`
- legacy/new separation is clear
- active planning/documentation structure is clean

## Phase 1 — Minimal Worker Loop

Status:
- complete

Goal:
- build the smallest interactive worker loop that follows `docs/architecture.md`

Tasks:
- [x] Create the new rebuild-root Go module and minimal package layout
- [x] Implement the minimal behavior-frame loader, including `AGENTS.md`
- [x] Implement minimal worker session foundation:
  - goal
  - reporting requirement
- [x] Implement the minimal worker context packet v1
- [x] Implement exact-action execution with:
  - minimal shell wrapping only when needed
  - full command logging
  - execution-result capture
- [x] Implement the minimal worker closed loop:
  - ask LLM for next action
  - validate approval/executability
  - execute exact action
  - feed result back
- [x] Implement the minimal approval model:
  - `this time`
  - `always allow` (session-scoped)
  - `no`
  - `--allow-all`
- [x] Implement context inspection for live diagnosis
- [x] Implement state-based session resume
- [x] Run live validation on:
  - one `secret.zip` workflow
  - one router workflow
- [x] Improve generic execution-result assessment:
  - add result `assessment`
  - add generic result `signals`
  - surface ambiguous/suspicious outcomes in active context
- [x] Improve generic completion judgment:
  - prefer `step_complete` when the goal is already satisfied by evidence
  - avoid rereading the same evidence indefinitely
- [x] Improve generic bounded-action judgment:
  - prefer actions that fit an interactive loop
  - avoid broad expensive commands when a smaller action can establish the next fact
- [x] Add minimal structured worker task context:
  - task state
  - current target
  - missing fact
- [x] Implement the minimal interactive worker CLI shell:
  - persistent prompt loop
  - session continuity across turns
  - reuse the same worker loop
  - keep it simple; no full TUI yet

Exit criteria:
- worker loop runs end-to-end with real LLM calls
- no fallback command synthesis exists in the new path
- exact commands and execution results are logged
- active context packet is inspectable during runs
- both live scenarios have been exercised with understandable behavior, even if not yet perfect
- router-style reconnaissance can complete cleanly on a bounded local target
- a minimal interactive worker CLI shell exists for direct user testing

## Phase 2 — Active Context Quality

Goal:
- make active context truth, target stability, and inspectability reliable before adding deeper memory mechanisms

Planned tasks:
- [x] Implement running summary as an explicit active section
- [x] Tighten truth ordering inside active context
- [ ] Improve target stability against noisy latest evidence without adding scenario-specific guardrails
- [x] Add visibility into included vs excluded context material and approximate size
- [x] Add a lightweight packet validation pass:
  - validate contradictions/redundancy after packet build
  - log validation results with the session
  - fail closed on fatally untrustworthy packets
- [x] Add curated active execution facts:
  - derive only from structured runtime truth such as task target, latest execution status, signals, log refs, and artifact refs
  - render facts as a small context-packet section with provenance
  - do not infer new facts from raw command output or task-specific recipes
- [x] Harden active execution fact schema without closing the fact taxonomy:
  - require kind, subject, status, and source/provenance on every fact
  - allow unknown fact kinds when provenance is structured and explicit
  - do not drop or reject facts solely because their kind is not pre-registered
- [x] Add generic recovery semantics:
  - derive recovery semantics from structured result state, failure class, and existing signals
  - surface recovery semantics as provenance-backed execution facts
  - do not add task-specific command repair, output scraping, or scenario-specific guardrails
- [ ] Only after validation is useful and inspectable, design any separate packet repair/rebuild step
- [x] Add interactive shell inspection commands for live multi-turn context testing:
  - `/stats`
  - `/packet`
  - `/lastlog`
- [x] Re-run repeated live validation on:
  - `secret.zip`
  - router/local recon
- [x] Confirm current packet validation stays clean across a broader live suite
- [ ] Phase boundary:
  - treat Phase 2 as good enough for now
  - move next work to minimal planning rather than more packet shaping

## Phase 3 — Minimal Planning

Goal:
- define the smallest useful planning model without recreating the old complexity

Planned tasks:
- [x] Document worker-vs-orchestrator planning boundaries explicitly
- [x] Document interaction modes:
  - worker:
    - conversation
    - direct execution
    - planned execution
  - orchestrator:
    - conversation
    - planned orchestration
- [x] Document top-level goal vs worker subgoal model
- [x] Define planning trigger rules so trivial requests do not create plans
- [x] Write concrete use cases for:
  - standalone trivial request
  - standalone multi-step task
  - orchestrated single-worker task
  - orchestrated parallel task
  - orchestrator conversational status / plan-change request
- [x] Keep initial plans sequential:
  - no explicit branch tree in the first planner
  - local alternatives stay in the closed loop
  - replan only on real blockage or material task change
- [x] Define worker-plan acceptance criteria
- [x] Define orchestrator-plan acceptance criteria
- [x] Keep plan validation separate from packet validation
- [x] Define planner output schemas/contracts for:
  - worker planner output
  - orchestrator planner output
- [x] Tie planner validation expectations directly to the planner output schemas
- [x] Implement the minimal worker planner:
  - planner trigger remains optional
  - trivial requests bypass planning
  - planned tasks emit short sequential semantic steps
  - `step_complete` can advance through plan steps before final completion
- [x] Add minimal worker-plan inspection to the interactive shell:
  - `/plan` prints the active plan state for live testing
- [x] Log planner attempts with the session:
  - accepted and failed planner outputs become inspectable artifacts
  - planner prompt, raw response, parsed plan, and validation result are preserved
- [x] Document generic worker step-execution semantics:
  - `in_progress`
  - `satisfied`
  - `blocked`
- [x] Document generic worker step-advance and step-blockage rules
- [ ] Carry worker planning boundary issues into the Phase 4 orchestrator contract design
- [x] Implement generic worker step satisfaction/blockage evaluation
- [x] Implement model-assisted generic step satisfaction evaluation:
  - evaluate active step from structured packet evidence
  - log step-evaluation attempts with the session
  - allow automatic step advancement when the active step is already satisfied
- [x] Implement model-assisted planned-step action review:
  - review proposed actions before execution during planned steps
  - log action-review attempts with the session
  - allow revise/block decisions without hardcoded command recipes
- [x] Normalize interrupted execution separately from ordinary command failure:
  - classify interrupted commands as `execution_interrupted` at the runtime layer
  - keep interrupted work represented as in-progress rather than ordinary failure in worker summaries
- [ ] Validate worker step advancement behavior with repeated live runs
- [ ] Implement generic handling for long-running but reasonable planned-step actions:
  - distinguish valid in-progress step work from overbuilt action selection
  - improve planned-step progress interpretation before adding more command-shape guidance
- [ ] Replace heuristic planner-step text matching with typed step metadata:
  - planner output should eventually carry explicit step kind/category information
  - runtime and validation logic should stop inferring semantics from step label text where possible
- [x] Implement robust worker input-mode classification before worker-loop execution:
  - classify each normal user turn as `conversation`, `direct_execution`, or `planned_execution`
  - validate classifier output against a small contract
  - fail safe to `conversation` on invalid or ambiguous classification
- [x] Add direct-execution completion evaluation in the worker loop:
  - after a successful simple action, evaluate whether the request is already satisfied
  - complete immediately on supported evidence instead of waiting for another free-form worker turn
  - keep this generic and evidence-driven; no task-specific rules or raw output parsing
- [x] Define worker input-classifier contract and validator skeleton:
  - structured output limited to `mode` and `reason`
  - no commands, no plan steps, no essay output
- [x] Replace the broken custom worker TUI renderer with a Bubble Tea foundation:
  - use Bubble Tea + Bubbles + Lip Gloss for the interactive worker CLI
  - retire the old snapshot/ANSI renderer path for real terminal usage
  - keep a separate scripted non-terminal path for tests and piped input
- [ ] Continue refining the worker interactive UI for manual testing:
  - keep the panel-based layout:
    - primary chat/execution pane
    - persistent right-side status pane
    - persistent bottom input bar
  - keep the separation between:
    - UI state
    - UI events/messages
    - renderer/view
    - input/controller loop
  - improve operator polish:
    - active plan visibility
    - active step visibility
    - latest command and result visibility
    - latest action review / step evaluation visibility
    - smoother manual diagnostics during long-running work
    - scope / approval / model / context usage
  - keep the visible plan semantic and short
  - validate the worker UI manually against `secret.zip` and router runs
- [x] Refactor the worker CLI run lifecycle around explicit progress events:
  - keep `WorkerPacket` as the authoritative current-task state
  - add a small structured worker progress event model for live runtime transitions
  - do not use events as a second source of truth
  - support multiple tasks inside one interactive session:
    - transcript/session persists
    - active task packet resets per new task
- [x] Remove black-box run behavior from the Worker CLI:
  - do not wait until the entire `Runner.Run(...)` returns before updating the TUI
  - surface in-flight progress during planning, execution, and post-execution evaluation
  - persist meaningful in-flight session state during long-running runs
- [x] Make the interactive worker logic observable and reusable without Bubble Tea rendering:
  - add an explicit headless interactive entrypoint
  - write append-only `events.ndjson` runtime artifacts during live sessions
  - write append-only `transcript.ndjson` conversation artifacts during live sessions
  - validate that a non-TTY run exercises the same classification / task rollover / worker execution logic
- [ ] Make post-execution terminal semantics execution-truth-first:
  - if execution already clearly implies `blocked`, surface it promptly
  - if execution already clearly implies `completed`, surface it promptly
  - use post-execution LLM judgment only when execution truth is genuinely ambiguous
- [ ] Add per-phase time budgets to worker LLM phases:
  - classification
  - planner
  - action review
  - direct evaluation
  - step evaluation
  - keep execution timeout policy separate

## Phase 4 — Orchestrator And Subscription Access (Active)

Goal:
- make the orchestrator the primary assessment experience while retaining the worker as the shared execution engine
- make subscription-backed OpenAI access available through a local REST bridge

Implementation order:

1. Establish trustworthy evaluation capture and resolve the subscription backend boundary.
2. Implement run/task/evidence contracts and the first complete orchestrated assessment.
3. Add bounded concurrency, validation, reporting, and operator control to that same path.

Tasks:
- [ ] Repair repeated-run capture to use explicit per-run session directories and the interactive/headless product path
- [ ] Define and record a Codex comparison protocol with matched model access, fixtures, tools, source access, credentials, and aggregate budgets
- [ ] Prototype subscription access without target execution:
  - compare documented Codex app-server embedding with an OpenCode-style OAuth inference adapter
  - verify account authentication, available models, streaming, structured results, cancellation, and limit/error handling
  - prove inference-only requests do not independently execute tools; an agent backend alone does not fulfill the LLM bridge requirement
  - select the backend from evidence and record compatibility/maintenance constraints
- [ ] Implement the local REST bridge around the selected adapter:
  - versioned requests, streaming events, cancellation, capability/model discovery, auth status, and limit visibility
  - local binding, caller authentication, isolated worker contexts, and protected credentials
  - no implicit switch from subscription access to paid API billing
  - retain local-model and explicitly selected API backends
- [ ] Define small run/task/result contracts with scope, dependencies, budgets, worker role, artifact ownership, and validation state
- [ ] Adapt the existing worker to those contracts; repair execution/completion/evidence gaps required by orchestration
- [ ] Implement orchestrator goal intake, semantic planning, task dispatch, durable state, and event observation
- [ ] Implement configurable bounded concurrency with separate task workspaces, shared read-only references, and exclusion for conflicting target mutations
- [ ] Enforce explicit target boundaries and aggregate run budgets at execution boundaries
- [ ] Implement a shared evidence ledger with observations, hypotheses, claims, and validated outcomes
- [ ] Implement independent claim validation before promoting findings or access claims
- [ ] Produce one OWASP-style assessment report with evidence, reproduction, impact, remediation, and coverage gaps, including interrupted/inconclusive runs
- [ ] Make the orchestrator CLI show worker activity, dependencies, approvals, blockers, model/backend usage, and report status
- [ ] Implement stop broadcast, child-process cleanup, and state-based resume without duplicate completed actions
- [ ] Validate the new path against the Phase 4 gates in `docs/runbooks/acceptance-gates.md`

Exit criteria:
- the primary orchestrator path completes a bounded lab assessment with a reviewable report
- independent tasks run concurrently and dependent/conflicting work is ordered correctly
- important claims require supporting validation; stop/resume and evidence attribution remain coherent
- subscription login and REST access work on the selected supported backend, with honest limits and no surprise API billing
- repeated live evidence and a matched Codex baseline are recorded; no superiority claim is inferred from agent count

## Phase 5 — Source-Assisted Assessment (Next)

Goal:
- connect deployed software identification to matching source, candidate weaknesses, and validated outcomes on the authorized target

Tasks:
- [ ] Record software identity, version/build/configuration evidence, and uncertainty
- [ ] Discover authoritative public or operator-authorized source and acquire a pinned revision with origin and license metadata
- [ ] Track source-to-deployment matching explicitly, including mismatched or unknown versions
- [ ] Delegate bounded source-analysis questions through the same task/worker contracts
- [ ] Emit hypotheses with code references, preconditions, target linkage, and missing validation evidence
- [ ] Support isolated local reproduction and approved target validation; distinguish their conclusions
- [ ] Handle unavailable source and false candidates without blocking unrelated investigation or inventing findings
- [ ] Test untrusted repository instructions and deceptive target output as evidence-handling cases
- [ ] Compare repeated runs against the frozen Codex baseline and record where orchestration/source correlation helps or hurts

Exit criteria:
- a clean vulnerable fixture produces a reproducible, source-linked, target-validated finding
- fixed and mismatched fixtures do not produce unsupported validated findings
- missing source degrades honestly to other assessment paths
- a preregistered comparison shows a repeatable improvement in assessment outcomes or operator effort within matched budgets, without increased unsupported claims

## Working Rules
- [ ] No patch-first behavior on this branch
- [ ] No new behavior that conflicts with the architecture document
- [ ] Every major implementation slice must be followed by real LLM validation
- [ ] Behavioral conclusions from live validation should use repeated runs, not single examples
- [ ] Prefer deletion over adaptation when both solve the same problem
- [ ] Add a recurring architecture drift review at phase boundaries:
  - catch hardcoded task behavior
  - catch brittle natural-language trigger matching
  - catch raw stdout/stderr parsing in behavior logic
  - catch machine state leaking into human-facing chat text
