# Rebuild Plan

This task list derives from `docs/architecture.md`.
The architecture document is the source of truth for the rebuild branch.

Planning rules:
- `TASKS.md` is the executable plan for the current phase and, optionally, the next phase.
- `ROADMAP.md` is directional only and must not contain detailed task lists.
- `DISCOVERIES.md` records lessons, risks, and future-phase notes; it is not a task list.
- If `TASKS.md` and `ROADMAP.md` differ, `TASKS.md` is authoritative for execution.

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
- [ ] Only after worker planning boundaries are stable, design orchestrator planning in detail
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
- [ ] Implement robust worker input-mode classification before worker-loop execution:
  - classify each normal user turn as `conversation`, `direct_execution`, or `planned_execution`
  - validate classifier output against a small contract
  - fail safe to `conversation` on invalid or ambiguous classification
- [x] Define worker input-classifier contract and validator skeleton:
  - structured output limited to `mode` and `reason`
  - no commands, no plan steps, no essay output
- [ ] Prioritize the worker interactive UI for manual testing:
  - add a panel-based layout with:
    - primary chat/execution pane
    - persistent right-side status pane
    - persistent bottom input bar
  - make always-visible worker state available without opening logs:
    - active plan
    - active step
    - latest command
    - latest result summary
    - latest action review
    - latest step evaluation
    - scope / approval / model / context usage
  - keep the visible plan semantic and short
  - validate the worker UI manually against `secret.zip` and router runs

## Working Rules
- [ ] No patch-first behavior on this branch
- [ ] No new behavior that conflicts with the architecture document
- [ ] Every major implementation slice must be followed by real LLM validation
- [ ] Behavioral conclusions from live validation should use repeated runs, not single examples
- [ ] Prefer deletion over adaptation when both solve the same problem
