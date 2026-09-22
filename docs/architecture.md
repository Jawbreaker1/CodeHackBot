# Architecture

Status: active implementation contracts and bounded future direction. Updated 2026-09-21.

## Product and implementation order

BirdHackBot is being built as System Verification's security testing platform. The primary product will be an orchestrator coordinating bounded investigations, validation, and evidence-backed reporting. All workers use one execution engine.

Current sequence: **core cleanup → subscription API wrapper → orchestration → source-assisted assessment**. `TASKS.md` owns immediate work; `ROADMAP.md` owns future direction. Astra is the development model. The intended OpenAI pentest runtime is Daybreak on GPT-5.6 Sol, alongside local models. Provider/model access must be verified during integration.

Foundation acceptance precedes capability expansion: complete and validate the agentic worker, then the coordinator around that same worker, before further knowledge/source integration. Cleanup acceptance is narrower than worker acceptance. Standalone and delegated tasks now use the same adaptive decision loop; the coordinator no longer forces a separate direct-execution path. Multi-step and provider acceptance still require inspected live evidence.

The active runtime implements a local worker, subscription inference bridge, and generic guided assessment coordinator. The built application exercises independent and dependent delegated tasks from startup. Reports are model-authored drafts; independent finding review and source-to-deployment correlation are not implemented. The web adapter now persists session navigation and conversation metadata beside each assessment, discovers it on restart, and exposes explicit resume for interrupted/incomplete runs; exact-once recovery of unknown external effects remains out of scope.

The previous detailed design is preserved in `docs/archive/pre-core-cleanup-2026-09-19/architecture.md`. It is historical and does not override this document.

## User experience requirement

Usability is part of safe operation and a core acceptance requirement. The application must help operators make informed decisions and recover from mistakes. Normal use must not require understanding the internal architecture or assembling a long command line.

- Once installed, launching `birdhackbot` without flags must open the primary guided application. First use guides provider setup/sign-in; later launches reuse appropriate preferences and offer a new assessment or an existing session.
- Guide the operator from a plain-language goal through target scope and execution permissions to a concise review before testing starts. Make the active scope, provider, and permission mode visible. Remembering preferences must not silently grant broader execution permissions.
- Manage supporting services such as the local subscription bridge through the application. Normal startup must not require a second terminal, manual token-file handling, or knowledge of internal ports.
- Explain choices where they arise, offer relevant next steps, and show progress, results, and an obvious stop control. Errors must say what happened and how to recover; technical diagnostics remain available on demand.
- When approval is required, explain the proposed action, affected target, and expected impact in plain language, with the exact invocation available for review. Honor the current approval policy and avoid redundant questions. Clear presentation must preserve actual runtime enforcement.
- Reveal advanced settings when needed. Flags remain useful for automation and troubleshooting, and both interfaces must use the same worker, scope, approval, and evidence contracts.

The first guided lab surface now starts with `birdhackbot` without flags and is shared by `birdhackbot-orchestrator`. In a real terminal it uses a Bubble Tea two-pane conversation/status surface; redirected automation can select the plain adapter with `BIRDHACKBOT_PLAIN=1`. It discovers local model choices, exposes a small settings flow, remembers provider preferences, manages the subscription bridge for an existing sign-in, lets the selected LLM conduct the intake conversation, and supports exploratory discovery from a plain-language authorized objective. The coordinator can request bounded, read-only local observations such as workspace entry names, fixed host identity metadata (`uname`, hostname, and `/etc/os-release`), or this host's network metadata; each request is shown and approved before it runs. Structured observation evidence is rendered as a readable result with expandable raw data. It reviews the model's proposed goal/scope before starting, serializes action approvals across workers, and accepts coordinator conversation while workers run. `/resume` reopens an unfinished coordinator session from its durable snapshot; completed sessions remain report-only. First-time subscription sign-in, packaged operation outside the checkout, and unfamiliar-operator acceptance remain gaps; the complete product requirement is not yet met. `docs/runbooks/acceptance-gates.md` defines the usability check.

## User interface surfaces

The product has two UI adapters over the same assessment runtime:

- **Terminal client.** Keep the existing [Bubble Tea](https://github.com/charmbracelet/bubbletea) implementation for the interactive CLI, with its scripted/headless path for automation and air-gapped operation. Bubble Tea owns terminal input, rendering, and local interaction only; it must not own assessment state, worker execution, approvals, or evidence semantics. The current dependency is the v1 module and remains pinned until the UI boundary is stable. Upstream now documents a v2 module and an upgrade path, so migration is a separate compatibility task rather than an incidental dependency change.
- **Web application.** `cmd/birdhackbot-web` serves the browser client over a versioned Go HTTP API. The browser is chat-first: the shared model-led intake protocol handles discussion and clarification, then returns a compact proposal for explicit review. The server owns the assessment lifecycle, durable session metadata, customer/session grouping, typed worker read models, approvals, polling-based progress, live coordinator replies, model selection, explicit confirmed session deletion, and report access; the browser must never call the LLM provider or execute tools directly. Unclassified draft conversations can be dragged into an existing customer workspace from the sidebar; the assignment is durable metadata and does not move or rewrite evidence. The operator surface keeps a calm coordinator transcript in the center, expandable typed runtime annotations between turns, and a worker inspector with task phases, current steps, actions, approvals, context/budget usage, evidence references, findings, and activity. Each customer can have multiple assessment sessions, and the customer read model exposes their session statuses, model-authored draft findings, individual report links, and a unified Markdown report. Authentication, origin/CSRF handling, and streaming events remain required before remote deployment.

Both surfaces call the shared `internal/assessment` coordinator; neither should call `internal/workerloop` directly. The local subscription bridge remains an inference-provider adapter and is not the user-facing assessment API. Bind the web server to loopback by default until authentication, authorization, CSRF/origin handling, and deployment policy are implemented. Do not add an embedded desktop wrapper until the browser product is useful; a Wails-style wrapper can be considered later if native packaging is required.

## Ownership

| Component | Owns | Does not own |
| --- | --- | --- |
| Model | Investigation strategy, visible plan revision, tool choice, interpretation, whole-goal evaluation | Runtime permissions or manufactured evidence |
| Worker loop | Model/action/result cycle, bounded steps, completion evaluation | Tool-specific retries or a hidden second planner |
| Executor | Explicit invocation, cwd, output capture, exit status, cancellation | Repairing commands or guessing shell syntax |
| Approval surface | Operator decision on the prepared invocation and cwd | Rewriting the approved command |
| Context | A reviewable view of current task and prior observations | Turning stale failures into current execution truth |
| UI adapters | Input and visible progress for terminal or browser users | Independent worker execution semantics |
| Application service | Assessment commands, subscriptions, approvals, and read models shared by both UIs | Rendering or direct tool execution |
| Assessment coordinator | Run goal, delegation, budgets, shared evidence, validation, final report | A duplicate worker reasoning engine |

Keep each change tied to a demonstrated failure or explicit requirement. Prefer small runnable slices; stop once the stated validation passes. Do not build speculative abstraction layers or tool-specific policy machinery.

## Shared worker control flow

Every turn selects exactly one decision: `action`, `update_plan`, `step_complete`, `ask_user`, or `blocked`. `step_complete` proposes completion of the original task, not an individual plan step. Any decision may include a short `plan` with a summary, one to six semantic steps, and an active step. Plans guide the model; runtime code does not infer workflow phases from goal keywords or advance steps from tool output.

Plan revisions retain their turn and preceding execution-log reference. They are visible in the guided UI, saved context, and subsequent model requests. This records that planning happened; it does not prove the planned actions happened. Plans cannot replace the original goal, done condition, scope, approvals, or budget.

After an executed action or a completion proposal, one evaluator checks the entire original goal and done condition against recorded observations. `in_progress` and evaluator `blocked` feedback return to the deciding model, which can revise its approach, ask for information, or explicitly stop. Tool failures and negative test results remain observations. Unavailable or malformed evaluation stops with a visible failure; it cannot establish success. Invalid decisions consume a turn and return schema feedback without executing anything.

## Action and execution contract

An action has exactly one execution form:

```json
{"type":"action","command":"printf","args":["%s","hello world; literal text"],"use_shell":false}
```

```json
{"type":"action","command":"printf '%s' 'hello world' > result.txt","use_shell":true}
```

In direct mode, `command` names an executable and `args` contains literal arguments. The runtime does not split a command string or interpret argument metacharacters. A malformed direct command is returned as a validation failure for the model to correct.

In shell mode, `command` is the entire script and `args` must be absent. Execution uses `/bin/sh -c`; a login shell must not silently change the prepared working directory or environment.

The executor prepares an invocation before approval, snapshots caller-owned arguments/environment overrides, and resolves the working directory. Approval displays that invocation and directory. Only explicit once/session approval permits execution. The same prepared plan is then executed and recorded.

Commands receive closed stdin. On Unix, processes run in a separate session; cancellation kills the owned process group. This is not a sandbox against a deliberately escaping process. The deployment environment still supplies isolation.

## Evidence and task completion

Execution results preserve invocation, mode, cwd, timestamps, exit status, output previews, log references, and output artifact references. Stdout and stderr stream to separate local files while the tool runs. Previews are bounded to 8 KiB per stream and explicitly indicate truncation. The completed log includes the full output, copied without buffering it all in memory.

Model-facing output fields are quoted strings with escaped newlines. A line such as `status: available` inside tool output must remain output, not appear to be a new packet metadata field. Exact-content requests are evaluated against the evidence field rather than lossy summaries.

A command exit status describes the process. It does not prove the user goal. Every task uses the same whole-goal evaluation before completion. An unavailable or invalid evaluator cannot establish success, and a bare completion claim without recorded execution evidence is insufficient.

Current-result summaries describe the latest execution. Earlier results remain observations available to the model; they do not replace the current result because a severity ranking considers them stronger. A new task starts with fresh execution results. Conversation history can be retained as history.

Execution assessments/signals are limited to process facts: actual exit status, cancellation/timeout, and whether both output streams are empty. Raw stdout/stderr remain evidence for model interpretation and findings; the executor does not classify their wording with phrase or regex matching. The regex target/prerequisite inference and synthetic authoritative-facts layer have been removed. Full evidence can be read from the recorded artifacts when previews are insufficient. A future findings pipeline must distinguish observations, hypotheses, local reproductions, and target-validated findings.

## Context ownership and bounds

Persisted worker state retains all execution observations in order, including repeated invocations with different logs/timestamps. Model context is a separate deep copy. Under pressure it bounds older command bodies and output bodies first while retaining execution identities and evidence references, then older conversation and retrieval excerpts; the latest command and output can become marked excerpts with log references. Original goal, done condition, policy/scope, current feedback, plan history, and the newest operator message are protected. Oversized protected context fails visibly before inference rather than silently discarding instructions.

The default client input ceiling is 48 KiB of combined message text. The guided local profile keeps that conservative default for Qwen 3.8; the guided subscription and web subscription profiles give Daybreak a 128 KiB application ceiling after the live run demonstrated that the shared limit was too small. That is an application safety bound, not a claim about the provider's actual context window, and it must be revised only against verified provider behavior. Worker views reserve 8 KiB for instructions and JSON quoting; the final client check applies to the actual message text. This is a byte ceiling, not an exact model tokenizer or a guarantee for arbitrary provider context windows. Subscription requests ask the bridge backend for up to 128,000 output tokens when supported; if a backend revision rejects that optional field, the bridge retries once without it and uses the backend's configured default. The provider may still impose a lower limit or return an incomplete response. Incomplete provider responses (`length` or `content_filter`) never become executable decisions.

The UI exposes concise model-authored summaries, plans, action impacts, evaluator results, and typed runtime events as expandable annotations. It does not display hidden chain-of-thought or treat provider reasoning traces as evidence. This keeps the operator informed about the model's decisions while preserving the distinction between a rationale summary and private reasoning tokens.

Recent operator messages preserve line breaks and indentation. Older conversation notes are bounded excerpts, not authoritative semantic memory. Deep copies isolate UI snapshots and compact model views from mutable execution state. Long investigations still require evaluation of retrieval quality and model-specific context sizing.

### Active-context strategy

The runtime separates local application context from the model-visible context. Local state owns authorization, scope, approvals, budgets, execution records, and durable evidence; the model receives a bounded projection of that state for its next decision. This follows the same separation described in the [OpenAI Agents context guidance](https://openai.github.io/openai-agents-python/context/): application state is not implicitly conversation history, and history management must be explicit.

The projection keeps the original goal and completion condition, policy and scope, current evaluator feedback, plan history, newest operator input, and references to recorded evidence. It compacts older output before older conversation and retrieval excerpts, marks omitted material, and fails visibly if protected instructions alone exceed the configured ceiling. Retrieved material and model-authored summaries remain untrusted inputs; they can inform a decision but cannot replace runtime facts or broaden scope. Conversation rollover is bounded and persisted, while execution records remain lossless locally.

The current implementation covers bounded projections, protected anchors, conversation rollover, plan history, visible truncation, and separate compact projections for coordinator and worker handoffs. The next context increments are relevance-ranked retrieval across a run, a durable multi-worker event history, and independently verified run summaries. Each should be added only with a fixture that demonstrates the information loss it prevents; context size alone is not a reason to add another compaction layer.

### Context layers, document offload, and knowledge transfer

Long assessments are reliable only when the application can remember more than it can place in one model request. Context is therefore layered, with one owner for each kind of truth:

| Layer | Owner | What it contains | What the model receives |
| --- | --- | --- | --- |
| Durable session authority | Assessment runtime and session snapshots | Goal, declared scope, policy, approvals, budgets, plans, task states, operator messages, all execution observations, and final status | A bounded projection; the model cannot rewrite this authority |
| Coordinator working view | Coordinator prompt builder | Goal and scope, limits and usage, plan history, compact result cards, evidence references, bounded operator conversation, findings and gaps | The next planning packet, with summaries and references instead of complete command bodies |
| Worker working view | Shared worker packet and model-view builder | Task goal and done condition, inherited scope and approval mode, plan state/history, recent conversation, latest execution, selected prior results, capability inputs, and operator state | A separately compacted packet with protected instructions and evidence references |
| Offloaded documents | Local evidence and context store | Full command logs, stdout/stderr, artifacts, context snapshots, plan/research notes, and provider metadata | Stable references plus bounded excerpts selected for the next decision |
| Operator view | CLI and web adapters | Typed runtime events, current worker status, approvals, context meter, summaries, and findings | Never a source of truth and never a replacement for the persisted session |

The durable layers are intentionally lossless locally. A model request may omit an output body, but the corresponding execution record keeps its invocation, working directory, exit status, timestamps, log references, artifact references, and bounded summary. A compact summary is a navigation aid, not proof. The model must cite the registered log or artifact when a finding depends on exact output.

The document offload format is designed for rehydration rather than a second hidden memory system. Every stored document should carry the session ID, task ID when applicable, turn or stage, creation time, model/backend, source or invocation, and a short description of omitted material. Evidence documents additionally carry the prepared invocation, cwd, exit/cancellation status, log and artifact references, and the model-facing summary. Context snapshots record the rendered sections, byte ceiling, bytes used, compaction notes, and the protected sections that were retained. Writes remain local, append-oriented where practical, and atomic when replacing a session snapshot so an interrupted run does not silently corrupt the authority record.

In the current layout, the assessment authority is `assessment.json`; each delegated task owns `tasks/<task-id>/session.json` and `result.json`, with its mutable workspace under `work/`, full command records under `logs/`, and inspectable packet snapshots under `context/`. The coordinator passes the next model only compact result cards and those stable paths. This makes an evidence-heavy run inspectable and resumable without copying its entire history into every prompt.

Knowledge moves through explicit contracts:

1. **Coordinator to worker.** The coordinator sends one bounded task with its question, done condition, inherited goal and scope, approval mode, dependencies, relevant compact result cards, and references to prior evidence. The worker receives a separate mutable workspace and does not inherit the coordinator's full prompt or every other worker's raw output.
2. **Worker to coordinator.** A worker returns status, a concise model-authored summary, an error when applicable, and structured execution evidence. The coordinator receives compact summaries and stable references; full bodies are rehydrated only when the next decision needs them and the runtime permits the read.
3. **Worker to worker.** Workers do not share a global mutable conversation. A later worker can learn from an earlier worker only through the coordinator's result catalog and explicitly registered evidence paths. This makes provenance and task dependencies inspectable after resume.
4. **Operator to coordinator.** New chat messages are bounded conversation context. They can clarify the goal or redirect work inside the existing scope, but they do not become evidence, broaden authorization, or grant a pending action. Approval is a separate typed runtime event.
5. **Model to runtime.** Plans, summaries, findings, and retrieved documents are untrusted claims. The runtime validates the decision schema, enforces budgets and dependencies, and records approvals and executions independently of the model's wording.

At each inference boundary the packet builder follows the same order: retain protected anchors, include the current task or planning question, add compact recent results and plan history, then select bounded excerpts by relevance from referenced documents. It removes complete output bodies before identities and references, and it records the omission in `context_notes`. It never injects every document “just in case,” treats a document as an instruction, or lets a retrieved note override scope, approval, or runtime state. If the protected anchors alone exceed the allowance, the request fails visibly so the operator can shorten the task or change the model profile.

This design makes context usage inspectable without pretending that byte counts are token counts. The coordinator and each worker measure their own exact rendered request, while provider-reported usage is retained as a separate metric. A future relevance index, cross-run knowledge catalog, or semantic run summary must preserve the same provenance and rehydration rules and must ship with a fixture proving which loss it prevents.

The browser assessment inspector exposes the latest worker request as a typed
context meter: bytes used, the configured application ceiling, remaining bytes,
and percentage used. The assessment overview reports the largest current
worker request so concurrent work remains easy to scan. This is measured from
the exact system and user message text sent by the application after projection;
it is deliberately labelled an input-byte ceiling rather than a token count.
Provider-reported cumulative token usage remains a separate assessment metric.

## Persistence and stopping

Session state is one local JSON snapshot per worker session, written through a temporary file and atomic replacement. Version 2 persists the original turn limit and consumed turns. Resume never replenishes that budget. Version 1 snapshots remain inspectable JSON but cannot be resumed because they lack reliable budget accounting. A pending invocation with an unknown outcome is never replayed automatically; inspect its evidence before starting a new task. This is not a multi-worker event store and does not provide exactly-once recovery of external tool effects.

Canceled runs persist an aborted outcome. The TUI keeps ownership of terminal input, routes typed prompt events to the guided console, and waits for the application to finalize before quitting. Task preparation runs outside the UI update handler. Per-action approvals and worker questions use the same prompt event path as setup; the UI never creates a second stdin reader.

The worker stops when execution evidence, configured context inspection, or progress persistence fails. Progress is persisted synchronously before an action starts, with a single worker-side writer; queued UI events cannot overwrite newer snapshots. Optional UI transcripts still have best-effort paths; production evidence journaling is separate future work.

## Scope and security boundaries

`AGENTS.md` owns authorization and testing rules. Session prompts carry those rules, but the current worker has no enforced target allowlist or network sandbox. It reports this limitation explicitly. A private address or local path does not automatically establish scope or skip a plan step.

Lab execution must occur inside the configured isolated environment. Runtime-enforced engagement scope is required before the platform's customer assessment path is ready. Keep that implementation small and explicit; do not infer authorization from arbitrary command strings.

Evidence remains local. Subscription/cloud inference will transmit selected model context to its provider; provider integration must make that choice explicit and keep credentials out of model context, tool environments, and logs.

## Subscription API wrapper: implemented first slice

`cmd/birdhackbot-llm-bridge` exposes authenticated loopback `POST /v1/chat/completions`. It translates the worker's text messages to Codex Responses requests with no tools and collects completed stream items. BirdHackBot retains action selection, approval, execution, and evidence ownership. Local model access remains available separately.

The bridge reads the existing file-based Codex ChatGPT login. Codex owns sign-in and token refresh; the adapter uses only the account RPC for refresh, never an agent turn. API-key credentials are rejected and there is no model or billing fallback. A separate private local token authenticates worker requests and is supplied through `--llm-token-file`, including on resume; secrets never enter session state or model context.

The selected model ID is passed unchanged, and the backend's resolved name is preserved in its response. Cancellation propagates upstream. Authentication/access/usage-limit failures remain failures; unfinished streams cannot establish completion. This first slice implements only the worker's text contract. Native OAuth UI, remote serving, client streaming, richer discovery, and concurrency scheduling are deferred.

This depends on a Codex backend compatibility endpoint, not a stable public inference API. The [subscription runbook](runbooks/subscription-bridge.md) owns setup, sources, exact limits, and verified behavior. Cloud context transmission is explicit; same-user credential access still depends on OS/VM isolation.

## Orchestration: generic guided foundation

`internal/assessment` calls the shared adaptive worker loop with separate task working directories and inherited scope/approval rules. Mutable directories are separated for ordinary work; they are not filesystem security boundaries. The coordinator proposes a batch of up to two independent tasks, reviews their results, and decides the next batch or final synthesis. When a search or recovery objective has independent bounded strategies or candidate partitions, those strategies may run in parallel with isolated state; a later validation or synthesis task reconciles their evidence. Dependencies must reference completed earlier tasks. The built application test runs two independent capability tasks, then a dependent validation task, using the same path an operator starts from. No fixed phase sequence or separate worker execution engine is introduced. The worker capability frame assumes a full Kali assessment environment, asks the model to verify available tooling, and permits a small task-local helper when standard tools are insufficient. Source and validation evidence remain attached to the task for review and later reuse.

The behavior frame is the product contract for every model-facing role. Planning, worker decisions, evaluation, intake, and interactive coordinator chat all receive the same repository `AGENTS.md` rules plus the role-specific runtime parameters. In particular, the coordinator is told that a full Kali environment is a supported part of BirdHackBot's architecture, while still distinguishing that product capability from an individual binary or version that has not been verified on the current host. A conversational response must not use a narrower, conflicting prompt that drops this context.

The initial limits are four coordinator rounds, eight tasks, six steps per task, and 96 model calls shared by planning, workers, and evaluators. Actual usage is recorded when the provider returns it; missing usage stays explicit. Decisions, full worker contexts, progress snapshots, execution evidence, operator conversation excerpts, and final state are retained locally. Ctrl-C cancels the shared context and waits for workers to finalize before reporting an aborted assessment. Guided resume continues from recorded results and the consumed model-call budget; it never replays an action with an unknown external outcome. Exactly-once recovery of external effects and remote web authentication remain deferred.

All roles inherit the same model client. Guided local preferences include explicit reasoning effort and an output-token allowance (initially 32,768), with a ten-minute request timeout. Subscription requests retain the bridge's supported wire contract and current timeout. Provider timeouts remain incomplete results, distinct from an operator stop. Server context and parallel load settings remain externally managed; the coordinator permits at most two concurrent requests.

The coordinator may request one correction of an invalid JSON decision under the same budget; rejected proposals never execute or become evidence. Worker questions are serialized through the same console as approvals, and answers return to the worker's existing step budget. Plain-language operator messages are answered through the configured model and appended to the next coordinator planning context; they cannot approve actions, alter scope, or become evidence. A proposed final worker answer reaches the semantic evaluator alongside recorded evidence; the answer itself is not new execution evidence.

Draft findings require recorded evidence paths. A model-reported reproduction also requires a completed dependent validation task and evidence from that task; those structural checks do not independently verify its security meaning. Reports retain the distinction between candidates and model-reported reproductions and require operator review. Failed workers and exhausted budgets remain visible, including in partial reports.

## Adaptive investigation and source-assisted assessment

Planning is adaptive and proportional to the task. Simple work may run directly; broader or dependent investigations need a visible plan. The coordinator can add, revise, cancel, or delegate tasks as discoveries arrive. Each delegated task carries an objective, inherited scope and permissions, relevant evidence, a budget, and a completion criterion. Workers return evidence, conclusions, uncertainty, and blockers. The coordinator reconciles their results and controls follow-up work within the shared budget; worker count alone must not drive delegation.

Software discovery must support vulnerability research during the assessment. Preserve the observed product/version and supporting evidence with its uncertainty. As useful software identities become available or change, query advisories and vulnerability references using permitted online sources or imported local snapshots. Check affected versions and relevant prerequisites; record source links, retrieval dates, and snapshot versions where applicable. Show the freshness and coverage limits of offline research. Research queries should use the software facts needed for matching without unnecessary customer or internal-target identifiers. A lookup failure is an explicit research gap, not evidence that no vulnerabilities exist.

Advisory matches become investigation candidates. The coordinator prioritizes useful leads and delegates scoped validation, revising its plan from the results. A matching software banner or CVE record alone is insufficient to report a confirmed target vulnerability. Online research does not expand the authorized testing scope.

The coordinator's non-empty task list is a proposed test sequence. In the browser path the operator selects the bounded tasks that may run before workers start; omitted tasks are recorded as skipped and cannot produce evidence. Per-action approvals still apply to every invocation. Findings preserve advisory identifiers, affected software, source references, severity, and confidence as structured fields; identifiers are not parsed into a claim and a match alone remains a candidate. Execution logs and registered artifact references remain the evidence register for the formal report.

The web analysis view is a read model over assessment state, separate from the coordinator transcript. It aggregates sessions by customer, ranks findings using the reported severity, confidence, and candidate/reproduced status, shows why each item is prioritized, and lists next actions and gaps. This ranking is deliberately transparent and does not pretend to be CVSS scoring. The Markdown report includes the selected test sequence, findings, advisory references, evidence references, remediation, and assessment gaps. These are model-authored drafts requiring operator review; a formal report is not a guarantee that no vulnerability exists.

Source-assisted work links observed software identity to an attributable repository and pinned revision. Model workers investigate candidate weaknesses, preserve uncertainty about deployment matching, and validate candidates against scoped fixtures. Source code and repository instructions are untrusted input; cloning does not authorize running build scripts.

Advisory research and source-assisted analysis complement assessment of configuration, authentication, authorization, and application behavior. The platform must be able to investigate weaknesses with no published advisory when evidence warrants it.

The primary competitive measure is unique, independently verified vulnerabilities discovered, including the proportion of known defects found and false claims. Reproducibility, operator effort, target effects, time, and aggregate model usage constrain that result. Compare with capable matched baselines; agent count and tool availability alone do not establish improvement.

## Kali tooling and reusable knowledge: required, not yet integrated

- Discover available tools and record their versions and relevant dependencies. Let workers choose suitable tools from actual capabilities instead of assuming every Kali package is installed. Third-party tools use the same scope, approval, cancellation, and evidence contracts.
- Retrieve relevant playbooks as investigation guidance with provenance and revision. The model adapts them to evidence; playbooks must not become hidden fixed workflows or override session permissions.
- Let workers build and validate custom applications/helpers when needed. Preserve source, dependencies, invocation, and validation evidence. Reuse starts with a small local catalog of versioned, tested tools, with customer data and credentials excluded from shared entries. Reuse does not grant execution permission or prove suitability for a new target.
- Query existing local vulnerability/exploit resources before adding a new database service. Keep advisory records, exploit examples, and target validation distinct. Inventory installed resources and freshness; an external lookup script is not a local database.

The current machine has Kali, Nmap, an Exploit-DB archive, and Metasploit metadata. Archived playbooks and legacy tool-generation code provide reference material. These observations do not establish active-core integration; the dated inventory is in `DISCOVERIES.md`.

## Air-gapped operation: required, not yet validated

Air-gapped mode must support the complete assessment workflow inside the customer's isolated environment. All model roles, including planning, evaluation, summaries, and any retrieval models, use local inference. Playbooks, advisory snapshots, available source, toolchains, model weights, and required build/runtime dependencies must be provisioned locally. Source unavailable locally remains an explicit gap.

Setup must offer this mode directly, show readiness and dataset dates, and explain missing resources. No cloud inference, external research, authentication refresh, telemetry, automatic update, or dependency-download fallback is permitted. A local failure remains a visible local failure. New knowledge and dependencies enter through deliberate offline imports with provenance, version/integrity information, and applicable licenses; no broad updater framework is needed for the first slice.

The deployment boundary must block external egress for the application and its child tools while permitting only the approved assessment network and local supporting services. A model prompt or application flag cannot establish this isolation. Verify the workflow with network isolation in place and observe attempted as well as successful connections. Local inference and an air-gapped deployment are separate claims. Connected mode can use permitted current research; offline reports must describe the imported knowledge's limits without implying current internet coverage.

## Validation

- Run focused regression tests for changed contracts, then deterministic CI and appropriate race checks.
- Major behavior changes also require repeated real-model checks with actual context/session artifacts inspected.
- Label focused smoke checks separately from generic capability acceptance or product acceptance.
- `docs/runbooks/acceptance-gates.md` owns the gates and evidence requirements.
