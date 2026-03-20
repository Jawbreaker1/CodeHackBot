# Architecture

Status: v1 baseline  
Decided at: 2026-03-14
Last updated: 2026-03-14

## 1. Purpose

BirdHackBot is a generic, LLM-led security testing system for authorized lab environments.

The system must:
- let the LLM reason, execute, inspect results, and self-correct
- preserve strict scope and safety boundaries
- log all executed actions and produced evidence
- generate human-readable, reproducible reports

The system must not depend on hardcoded pentest recipes to function.

## 2. Design Rules

These rules are intended to prevent the previous failure mode.

1. Keep the core small.
2. Prefer deletion over patching.
3. Let the LLM own problem solving.
4. Let the runtime own only safety, scope, execution, and evidence capture.
5. Keep context generous and controlled, not artificially small.
6. Treat capabilities as optional and removable.
7. Avoid deep state complexity until it is clearly needed.
8. Validate every major slice with real LLM runs.

## 3. Core vs Capabilities

### 3.1 Core

The core is the smallest working closed loop:
- goal intake
- step definition
- context packet assembly
- LLM action selection
- execution
- execution result capture
- self-correction loop
- report generation from evidence

The core must work without any extra capability enabled.

### 3.2 Capabilities

Capabilities are optional modules that plug into the core without taking control of it.

Examples:
- runbook hints
- CVE candidate lookup
- vulnerability validation helpers
- product fingerprinting
- report enrichment
- tool discovery/install guidance

Capabilities may:
- contribute context
- contribute observations
- propose candidates
- validate or enrich evidence
- add report sections

Capabilities must not:
- become a second planner
- inject hidden command sequences
- mutate runtime behavior globally
- become required for the core loop to function

If a capability is disabled or broken, the core loop must still run honestly.

## 4. Component Responsibilities

### 4.1 Planner

Owns:
- turning a goal into a small semantic step or task
- defining done conditions
- defining failure conditions
- defining expected evidence

Does not own:
- exact command shaping
- recovery strategy
- fallback behavior

### 4.2 Context Manager

Owns:
- building the active context packet for each LLM turn
- preserving the current task thread
- surfacing canonical truth from the latest execution result
- retrieving older relevant memory only when useful

Does not own:
- decision making
- command mutation
- hidden heuristics for recovery

### 4.3 Worker Loop

Owns:
- asking the LLM for the next action
- passing exact execution results back to the LLM
- driving `retry_modified`, `pivot_strategy`, `ask_user`, or `step_complete`

Does not own:
- tool-specific hacks
- fallback command invention

### 4.4 Executor

Owns:
- executing the exact selected action
- minimal shell wrapping only when needed
- capturing stdout/stderr
- capturing artifacts
- capturing timestamps, exit status, cwd, and env delta

Does not own:
- improving commands
- inventing arguments
- replacing weak actions with stronger ones

### 4.5 Evidence Store

Owns:
- command logs
- execution result records
- artifact storage
- report inputs
- event history

This is the truth source for replay, reporting, and audit.

### 4.6 Reporter

Owns:
- building a human-readable report from structured evidence

Does not own:
- execution flow
- vulnerability truth
- recovery behavior

### 4.7 Orchestrator

Owns:
- run-level planning into worker tasks
- worker lifecycle
- approvals
- scheduling
- claim validation and promotion of trusted outcomes
- aggregation of worker outputs into a run result

Does not own:
- a separate intelligence loop
- command shaping inside worker execution
- fallback planning on behalf of workers
- blind trust in worker conclusions

## 5. Canonical Data Contracts

These contracts are intentionally small. They should stay small until there is a clear reason to expand them.

### 5.1 Goal Contract

Contains:
- goal text
- scope
- constraints
- reporting requirement
- stop conditions

### 5.2 Step Contract

Contains:
- objective
- objective class / phase
- done condition
- fail condition
- expected evidence
- risk level
- budget

This is semantic, not a fixed command recipe.

The initial rebuild should keep `objective class / phase` simple. It exists so the orchestrator can understand which tasks belong to the same larger objective without needing a complex state machine.

### 5.3 Execution Result

This is the canonical truth after every action.

Contains:
- exact executed action
- execution mode
- cwd
- env delta
- start/end timestamps
- exit status
- stdout/stderr summary
- log references
- artifact references
- input references
- failure classification, if any

All recovery and reporting must consume this same object.

### 5.4 Context Packet

This is the exact packet shown to the LLM each turn.

For the rebuild, there should be one authoritative packet definition per actor type:
- worker packet
- orchestrator packet

The context packet must be inspectable during live runs.

### 5.5 Event Record

Contains:
- id
- timestamp
- actor
- session/run id
- task id if applicable
- event type
- payload

All state changes and executed commands should be traceable through events and evidence.

## 6. Context Management

This section is intentionally explicit because context was a major failure point before.

### 6.1 Conceptual Context Layers

The active context should be built in layers.

This section is conceptual. The concrete v1 packets are defined in later subsections.

The conceptual layers are:

1. Behavior frame
   - system/behavior prompt
   - `AGENTS.md` constraints and repository rules
   - behavior parameters
   - runtime mode

2. Session foundation
   - goal
   - scope
   - constraints
   - reporting requirement

3. Current task/step
   - current objective
   - done condition
   - fail condition
   - expected evidence

4. Plan state
   - current high-level plan
   - current active step
   - completed/blocked step markers

5. Conversation context
   - recent conversation turns in full
   - summarized older conversation history

6. Latest execution truth
   - exact action
   - result
   - logs
   - artifacts

7. Running summary
   - short current narrative of what has happened and what remains unclear

8. Retrieved relevant history
   - only prior facts or evidence relevant to the current task

9. Capability contributions
   - optional hints or validated external knowledge

### 6.2 Worker Active Context v1

The initial worker context packet should contain:

1. `behavior_frame`
2. `session_foundation`
3. `current_step`
4. `plan_state`
5. `recent_conversation`
6. `older_conversation_summary`
7. `latest_execution_result`
8. `running_summary`
9. `relevant_recent_results`
10. `memory_bank_retrievals`
11. `capability_inputs`
12. `operator_state`

`operator_state` includes:
- current scope and permission state
- approval state
- model
- context usage

This is the authoritative v1 worker packet definition.

This is intentionally structured and generous. It should not be squeezed into a tiny packet just to save tokens.

`running_summary` is an explicit packet section. It should contain a short factual narrative of:
- what has been established
- what failed
- what remains unclear
- what the current blocker is, if any

### 6.3 Non-Droppable Core

During an active step, the system must always preserve:
- `behavior_frame`
- `session_foundation`
- `current_step`
- `plan_state`
- `latest_execution_result`
- current unresolved blocker
- currently relevant artifact references

These must survive rebuild and compaction.

### 6.4 Rules

- Do not aggressively shrink active context.
- Do not drop the current task thread.
- Do not drop the behavior frame.
- Do not use memory as a substitute for healthy active context.
- Do not silently rewrite artifact truth.
- Do not prefer stale summaries over fresh execution results.
- Keep recent conversation turns in full until they are clearly no longer needed.
- Summarize older conversation history conservatively and preserve user intent, constraints, and unresolved questions.

### 6.5 Truth Priority

When signals conflict, trust in this order:

1. latest execution result
2. artifacts/logs referenced by the latest result
3. running summary
4. retrieved memory
5. older artifacts or summaries

### 6.6 Worker Memory Bank v1

The worker should have a single structured local memory-bank store.

This is not a second reasoning system. It is a support layer for continuity and retrieval.

For v1, memory-bank behavior should remain minimal. The rebuild should not yet solve complex reconciliation between active context and offloaded memory.

Active context remains primary. Memory-bank offloading and retrieval should be introduced conservatively after the core loop is working well in live runs.

Allowed entry types:
- `fact`
- `hypothesis`
- `failed_attempt`
- `artifact_ref`
- `useful_command`
- `blocker`
- `next_option`

Rules:
- entries should be short and structured
- entries should be evidence-backed where possible
- raw logs must not be copied into the memory bank
- speculative reasoning must not be promoted as fact

The initial rebuild should start with one memory-bank structure, not many documents.

### 6.7 Orchestrator Run Context v1

The orchestrator should use a separate run-level context stack.

The initial orchestrator packet should contain:

1. `behavior_frame`
2. `run_foundation`
3. `run_plan_state`
4. `recent_conversation`
5. `older_conversation_summary`
6. `running_summary`
7. `worker_status`
8. `latest_task_results`
9. `shared_evidence_refs`
10. `memory_bank_retrievals`
11. `pending_approvals`
12. `operator_state`

The worker and orchestrator should not have separate worker-loop architectures, but they do need different context packets because they own different decisions.

### 6.8 Shared Structured Evidence Layer

The system may maintain a shared evidence/facts layer between workers and the orchestrator.

Allowed shared items:
- validated facts
- target fingerprints
- validated findings
- canonical artifact references
- validated access state
- validation outcomes

Forbidden shared items:
- free-form worker reasoning
- speculative summaries presented as facts
- raw log dumps as shared memory
- unvalidated conclusions promoted as trusted outcomes

This shared layer should remain narrow. It is a shared evidence ledger, not a shared brain.

### 6.9 Compaction And Rebuild Policy

Compaction is a support mechanism, not the default operating mode.

Initial rebuild rule:
- keep the active packet generous
- rebuild the packet every turn from canonical sources
- compact only when clearly needed
- when compacting, preserve the current task thread and latest execution truth in full
- prune low-value items before summarizing
- preserve recent conversation turns before collapsing them into summary

Per-turn rebuild order:
1. rebuild from canonical sources
2. include the non-droppable core
3. add relevant recent results
4. add memory-bank retrievals only if relevant
5. add capability inputs only if relevant
6. if still too large:
   - prune repeated low-value items first
   - summarize older history second
   - do not compact current-step or latest-result truth

### 6.10 Inspectability

The system must let the operator inspect:
- the current context packet
- the recent conversation segment
- the summarized older-history segment
- what memory was retrieved
- what capability contributions were added

Without this, context problems become invisible.

## 7. Minimal Agentic Worker Flow

This is the simplest possible worker loop we should build first.

### 7.1 Worker Flow

1. Receive goal or step contract.
2. Build context packet.
3. Ask LLM for the next action.
4. Validate only:
   - scope
   - permission/approval requirement
   - executability
5. Execute exact action.
6. Record execution result.
7. Feed execution result back to the LLM.
8. LLM chooses one of:
   - `retry_modified`
   - `pivot_strategy`
   - `ask_user`
   - `step_complete`
9. Repeat until the step is done or honestly blocked.
10. When all steps are done, render the report from evidence.

### 7.2 Worker Progress Rule

Only one generic progress rule should exist:
- the next action must materially differ by command, target, evidence, or purpose

If not, the worker must:
- pivot
- ask the user
- or conclude the step honestly

We should avoid many specialized anti-loop rules.

### 7.3 Worker Fallback Rule

Fallback must stay narrow.

Allowed:
- report that the primary LLM response is malformed or unavailable
- request user input when the worker is actually interactive
- stop honestly when recovery cannot continue

Forbidden:
- synthesizing commands
- acting like a second planner
- inventing recovery logic from scraps of text

## 8. Orchestrator Workflow

The orchestrator should remain simple and only coordinate.

### 8.1 Orchestrator Flow

1. Receive run goal.
2. Produce task contracts.
3. Schedule a task to a worker.
4. Worker runs the same closed loop used by the CLI.
5. Worker emits events and artifacts.
6. Orchestrator records the result as one of:
   - observation
   - claim
   - validated outcome
7. If the result is phase-advancing or high-impact, orchestrator may create a validation task before trusting it.
8. Only validated outcomes may:
   - advance a run phase
   - cancel sibling tasks for the same phase
   - be promoted to trusted findings
9. Orchestrator updates run state.
10. Orchestrator decides only:
   - schedule next task
   - wait for approval
   - requeue
   - validate claim
   - cancel obsolete sibling tasks
   - stop
   - finalize run report

### 8.2 Orchestrator Rules

- Orchestrator must not invent a second reasoning loop.
- Orchestrator must not shape commands for workers.
- Orchestrator must treat worker execution results and artifacts as truth.
- Orchestrator may coordinate, pause, stop, retry, or aggregate.
- Orchestrator must not blindly trust a worker claim just because it is marked successful.
- Validation should be required for phase-advancing outcomes and high-impact findings, not for every low-level observation.
- When a phase objective is validated as complete, sibling tasks for that same phase should normally be canceled unless the run explicitly requires them to continue.

### 8.3 Validation Gate

The orchestrator should use a simple three-level model:

1. `observation`
   - raw result or evidence
   - useful for ongoing reasoning
   - not yet trusted as a conclusion

2. `claim`
   - a worker asserts that an important condition is true
   - for example: access obtained, privilege escalation worked, finding reproduced

3. `validated outcome`
   - a separate validation step confirms the claim with sufficient evidence
   - only then may the orchestrator advance the larger objective

This keeps validation explicit without requiring a second validator for every small piece of evidence.

Important:
- `task_completed` means the worker believes its bounded task objective is finished
- `validated outcome` means the orchestrator trusts the result enough to advance a phase or promote a finding

These are not the same thing.

### 8.4 Worker Roles

The architecture should allow worker roles, but roles are not part of the initial rebuild iteration.

Likely future roles:
- operator / explorer
- validator
- reporter

Role rules:
- roles should reuse the same core worker loop
- roles should differ mainly through behavior prompt and behavior parameters
- roles must not create separate execution architectures
- roles should be introduced only when the minimal core loop is stable

### 8.5 Orchestrator-To-Worker Contract

The orchestrator should communicate with workers through a small structured contract, not through log scraping or conversational coaching.

The initial `task_contract` should contain:
- `run_id`
- `task_id`
- `objective`
- `objective_class`
- `scope`
- `constraints`
- `expected_evidence`
- `budget`
- `approval_state`
- `context_attachments`
- optional `role` field reserved for later use

This is the only work-start input the worker should need from the orchestrator.

### 8.6 Worker-To-Orchestrator Contract

Workers should emit structured records back to the orchestrator.

Initial record types:
- `task_started`
- `task_progress`
- `approval_requested`
- `execution_result`
- `task_claim`
- `task_completed`
- `task_blocked`
- `task_failed`
- `task_canceled`

These machine-readable records are the coordination contract.

For v1, `execution_result` should carry artifact references directly. A separate `artifact_created` record is not required unless later implementation proves a real need.

Human-readable logs remain important for replay and audit, but they are supporting evidence, not the primary worker/orchestrator protocol.

### 8.7 Forbidden Communication Patterns

The initial rebuild should forbid:
- orchestrator coaching a running worker with tactical hints
- workers using only prose logs as the machine contract
- orchestrator hiding command suggestions inside attachments or notes
- logs becoming the primary source of machine coordination truth

If the orchestrator wants to change direction, it should do so by task control:
- cancel task
- requeue task
- create replacement task
- create validation task
- create next-phase task

It should not nudge the current worker with ad hoc instructions mid-task.

## 9. Logging and Evidence

Logging must be complete enough to reproduce a session.

### 9.1 Evidence Layers

The system should keep four separate evidence layers:

1. `event stream`
   - lifecycle and coordination truth

2. `execution result records`
   - the structured result of each executed action

3. `command logs`
   - full stdout/stderr and execution trace for replay and audit

4. `artifacts`
   - produced or collected files

These layers must stay separate. Logs are not a substitute for structured results, and structured results are not a substitute for full logs.

### 9.2 Every Executed Command Must Record

- exact command or argv
- cwd
- env delta
- timestamp start/end
- exit code
- stdout/stderr refs
- produced artifact refs
- related step/task id

### 9.3 Event Logging Must Record

- task transitions
- approval requests/decisions
- worker start/stop
- interrupts
- report completion

### 9.4 Finding And Outcome Lifecycle

The system should distinguish between:

1. `observation`
   - raw evidence or output

2. `claim`
   - an interpreted result that still needs trust evaluation

3. `validated finding/outcome`
   - sufficiently confirmed to affect phase progression or be trusted in reporting

This lifecycle applies to both worker-level results and orchestrator-level aggregation.

### 9.5 Report Foundation

Reports should be written from evidence, not from memory or guesswork.

The report must summarize:
- goal
- scope
- method
- plan
- steps attempted
- findings
- validation status
- results
- remediation
- blockers/unknowns

The report should link to logs and artifacts, but it should not read like a dump of raw links.

Human readability is the primary report goal. Evidence links support the narrative; they do not replace it.

### 9.6 Report Standards

The reporting system should support standard structured security report formats.

The initial reporting target should support at least:
- OWASP-style reporting

Later formats may be added as capabilities, but the core report model should remain generic enough to render into multiple standards.

### 9.7 Worker Reports And Orchestrator Reports

In standalone worker mode:
- the worker report is the primary session report

In orchestrated mode:
- worker outputs should contribute structured findings and evidence
- the orchestrator run report is the primary final report

Worker-level notes or partial reports are supporting inputs, not the final authority for the run.

### 9.8 Reproducibility Minimum

Every reported finding should have enough supporting material to be peer-reviewable.

Minimum expected support:
- goal coverage
- plan coverage
- performed-step summary
- result summary
- human-readable finding text
- validation status
- reproduction summary
- evidence references
- remediation guidance

## 10. UI Surfaces

### 10.1 Scripted CLI

The plain CLI is primarily a development, testing, and automation surface.

It should expose:
- non-interactive or lightly interactive execution
- visible command execution
- context inspection when requested
- continue/resume of a prior session
- final summary and report path

Resume in the initial rebuild should be state-based only:
- restore conversation, plan state, evidence references, and memory state
- do not attempt to resume an already-running worker process

Interactive input behavior should support:
- input history navigation with up/down
- cursor movement and in-line editing with left/right
- practical terminal editing behavior suitable for long prompts

### 10.2 Interactive Worker Terminal UI

The interactive worker terminal UI is a primary product surface.

For the worker-facing interactive UI, the operator should always be able to see:
- the live execution and conversation stream
- the latest command being executed
- the latest result summary
- a short semantic high-level plan with step state
- current scope, permissions, model, and context usage
- an always-visible input area

The semantic plan shown to the user should remain short and human-readable. It should not explode into micro-steps for every shell action.

In the worker UI, the visible plan should represent only the core semantic steps of the current task. It should not show gritty execution details as separate plan items.

Examples of good worker-plan steps:
- identify target or input
- inspect metadata or surface
- attempt access or validation
- verify result
- produce report

Examples of bad worker-plan steps:
- every individual shell command
- every retry as its own plan step
- every file read as a plan step

### 10.3 Interactive Orchestrator UI

The interactive orchestrator UI is also a primary product surface.

It should show:
- run goal
- high-level run plan
- active, completed, and blocked tasks
- worker assignments
- pending approvals
- major findings and validation state
- next orchestration action

It must also show actual worker activity so the user can see the work being done.

Minimum worker visibility in orchestrator mode:
- current worker task objective
- latest worker command
- latest worker result
- access to full worker terminal/log output on demand

The orchestrator UI should let the user move from run-level overview to worker-level detail without losing the overall mission view.

The orchestrator UI must also include an interactive chat/session area with:
- a clear input field
- visible conversation history with the orchestrator
- the ability for the user to ask for status, blockers, rationale, and possible next actions
- the ability for the user to suggest new tasks or reprioritization
- continue/resume of a prior run session

Orchestrator resume in the initial rebuild should also be state-based:
- restore run state, task state, evidence, and conversation state
- do not attempt to resume running worker processes directly

User suggestions must not silently change execution. If a user request would alter execution, the orchestrator should:
1. interpret the request
2. explain the proposed change
3. ask for explicit confirmation
4. only then apply it

Interactive input behavior should also support:
- input history navigation with up/down
- cursor movement and in-line editing with left/right
- practical editing of long prompts without losing the current session view

### 10.4 Web

The web app is optional and should come later.

It should:
- read the same session/event/report model
- present runs, tasks, workers, evidence, and reports
- allow drill-down into full worker terminal output and artifacts
- preserve the same validation and task states shown in terminal UIs
- later allow image input where vision-capable models are enabled

It must not:
- create separate execution semantics
- force extra complexity into the core loop
- become the source of truth for workflow logic

## 10.5 Operator Experience Principles

These principles apply across worker UI, orchestrator UI, and later web UI.

- default to simple operator flows
- use progressive disclosure instead of exposing every low-level option at once
- show human-readable task labels, not just internal ids
- clearly show validation state for important findings and outcomes
- always make current goal, current step, and current blocker visible
- approvals must explain what is being approved and why
- proposed execution-changing plan changes must be summarized before confirmation
- reports should feel like part of the workflow, not a separate afterthought
- input handling should feel like a usable terminal application, not a raw scrolling log
- resuming an existing session or run should feel like continuing the same conversation, not starting over

When handing control back to the user, the agent should always provide:
- a short summary of the last meaningful execution or planning outcome
- the current status
- 2-3 optional suggested next steps that would make progress

These suggestions should help uncertain users, but they must never feel mandatory.

## 11. Capabilities

Capabilities should plug into the core through narrow interfaces.

Initial architecture requirement:
- the core must define where capabilities can contribute
- the first rebuild iteration does not need to deeply implement many of them

### 11.1 Good Capability Examples

- runbook hint provider
- fingerprint provider
- CVE candidate lookup
- vulnerability validation helper
- report enricher

### 11.2 Capability Rules

- capabilities may contribute context, observations, or structured evidence
- capabilities must be removable
- capabilities must not own the loop

Validation behavior and specialized worker roles should be treated as later capabilities layered onto the stable core, not as complexity added before the core loop works.

### 11.3 Runbook Role

Runbooks are advisory context only.

They may provide:
- likely tooling
- likely evidence expectations
- common validation paths
- common pitfalls

They must not:
- be treated as fixed recipes
- override the LLM
- inject hidden command sequences

### 11.4 Vulnerability Validation Role

Vulnerability testing should eventually follow a generic evidence flow:

1. fingerprint target
2. generate candidates
3. validate candidate existence from trusted sources
4. derive validation method
5. attempt controlled validation
6. classify result:
   - validated
   - not validated
   - inconclusive

This is a capability, not part of the minimal core loop.

## 12. Minimal Safety And Approval Model

The first rebuild iteration should keep safety and approval handling intentionally simple.

### 12.1 Default Execution Approval

By default, any action that would execute should require explicit user approval.

Initial approval choices:
- `this time`
- `always allow`
- `no`

This should apply in both worker and orchestrator interactive flows when execution is about to happen.

`always allow` must be session-scoped. It must not silently persist across unrelated future sessions.

### 12.2 Allow-All Override

The initial rebuild may support an explicit `--allow-all` override.

This is a convenience mode for trusted local development and testing. It must be explicit and visible in the UI.

### 12.3 Ownership

- the worker or orchestrator may prepare the proposed action
- the UI must present the approval clearly
- the user is the one who grants or denies approval

In orchestrated mode, approvals still belong at the orchestrator surface, not hidden inside worker logs.

### 12.4 Requirements

- every approval prompt must clearly explain what will be executed
- the user must be able to tell whether the approval is one-time or persistent
- approval state must stay visible during the session
- interrupts and stop actions must always work cleanly

### 12.5 Explicit Non-Goals For v1

The first rebuild iteration should not yet introduce:
- a large permission matrix
- detailed action-class policies
- complex danger scoring
- many approval flags or special modes

These may be added later, but only after the minimal core loop works well.

### 12.6 Hard Denials

Even in the minimal model, some things remain outside approval:
- out-of-scope actions remain denied
- actions prohibited by `AGENTS.md` remain denied

Approval must not override scope or explicit project rules.

## 13. Minimal State Model

The first rebuild iteration should keep state/status modeling intentionally small.

States are not the same as reasons. Reasons should be separate metadata so that the state model does not explode.

### 13.1 Run Or Session State

- `active`
- `waiting_user`
- `stopped`
- `completed`

Meanings:
- `active`: execution or planning can continue
- `waiting_user`: progress can continue as soon as the user acts
- `stopped`: ended without goal completion
- `completed`: goal reached and final report/state written

### 13.2 Task State

- `pending`
- `running`
- `waiting_user`
- `blocked`
- `done`
- `failed`
- `canceled`

Meanings:
- `pending`: not started yet
- `running`: actively being worked
- `waiting_user`: waiting on user approval or explicit user input
- `blocked`: cannot honestly continue in its current form
- `done`: objective achieved
- `failed`: bounded unsuccessful attempt
- `canceled`: intentionally stopped because obsolete, replaced, or canceled by controller

Important distinction:
- `waiting_user` means user action can directly unblock progress
- `blocked` means the task cannot honestly continue in its current form, even if it is clearly explained to the user

### 13.3 Finding Or Outcome State

- `observation`
- `claim`
- `validated`
- `rejected`
- `inconclusive`

These states apply to findings and other important outcomes such as access claims or privilege-escalation claims.

### 13.4 Approval State

- `not_needed`
- `pending`
- `approved_once`
- `approved_session`
- `denied`

### 13.5 State Discipline

The rebuild should not add more states unless there is a clear, proven need.

In particular, the system should avoid:
- multiple overlapping blocked/waiting states
- mixing state and reason into one enum
- deep state hierarchies early in the rebuild

## 14. Visual Schematics

### 12.1 Component Interaction

```text
          +------------------+
          |   Planner        |
          | semantic steps   |
          +---------+--------+
                    |
                    v
          +------------------+
          | Context Manager  |
          | active packet    |
          +---------+--------+
                    |
                    v
          +------------------+
          |   Worker Loop    |
          | LLM decisions    |
          +----+--------+----+
               |        |
        action |        | result
               v        ^
          +------------------+
          |    Executor      |
          | exact execution  |
          +---------+--------+
                    |
                    v
          +------------------+
          | Evidence Store   |
          | logs/artifacts   |
          +----+--------+----+
               |        |
               |        +-------------------+
               |                            |
               v                            v
          +------------------+      +------------------+
          | Reporter         |      | Capabilities     |
          | human report     |      | optional inputs  |
          +------------------+      +------------------+
```

### 12.2 Simplest Worker Closed Loop

```text
goal/step
   |
   v
build context packet
   |
   v
LLM chooses next action
   |
   v
validate approval/executability
   |
   v
execute exact action
   |
   v
record execution result
   |
   v
LLM evaluates result
   |
   +--> retry_modified
   |
   +--> pivot_strategy
   |
   +--> ask_user
   |
   +--> step_complete
```

### 12.3 Simplest Orchestrator Flow

```text
run goal
   |
   v
produce task contracts
   |
   v
schedule task to worker
   |
   v
worker runs same closed loop as CLI
   |
   v
worker emits events + evidence
   |
   v
orchestrator classifies result
   |
   +--> observation
   +--> claim
   +--> validated outcome
   |
   +--> if needed, create validation task
   |
   v
orchestrator updates run state
   |
   +--> schedule next task
   +--> cancel obsolete sibling tasks
   +--> wait for approval
   +--> requeue
   +--> stop
   +--> finalize run report
```

## 15. Validation Strategy

Every major implementation slice must include real LLM validation.

Minimum live checks:
1. one local-file workflow such as ZIP/password recovery
2. one network workflow such as router reconnaissance/reporting

During those runs, we should inspect:
- context packet
- memory retrieval
- command logs
- report quality

## 16. Open Review Questions

These should be resolved in the next review pass:

1. How minimal should the initial planner be?
2. How much memory functionality belongs in the first iteration?
3. What is the smallest useful approval model?
4. Which capability extension points should be defined now, versus later?
5. How much event/state structure do we need before implementation begins?
