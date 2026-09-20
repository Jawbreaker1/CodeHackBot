# Architecture

Status: active implementation contracts and bounded future direction. Updated 2026-09-20.

## Product and implementation order

BirdHackBot is being built as System Verification's security testing platform. The primary product will be an orchestrator coordinating bounded investigations, validation, and evidence-backed reporting. All workers use one execution engine.

Current sequence: **core cleanup → subscription API wrapper → orchestration → source-assisted assessment**. `TASKS.md` owns immediate work; `ROADMAP.md` owns future direction. Astra is the development model. The intended OpenAI pentest runtime is Daybreak on GPT-5.6 Sol, alongside local models. Provider/model access must be verified during integration.

Foundation acceptance precedes capability expansion: complete and validate the agentic worker, then the coordinator around that same worker, before further knowledge/source integration. Cleanup acceptance is narrower than worker acceptance. Standalone and delegated tasks now use the same adaptive decision loop; the coordinator no longer forces a separate direct-execution path. Multi-step and provider acceptance still require inspected live evidence.

The active runtime implements a local worker, subscription inference bridge, and generic guided assessment coordinator. The built application exercises independent and dependent delegated tasks from startup. Reports are model-authored drafts; independent finding review, full assessment resume, and source-to-deployment correlation are not implemented.

The previous detailed design is preserved in `docs/archive/pre-core-cleanup-2026-09-19/architecture.md`. It is historical and does not override this document.

## User experience requirement

Usability is part of safe operation and a core acceptance requirement. The application must help operators make informed decisions and recover from mistakes. Normal use must not require understanding the internal architecture or assembling a long command line.

- Once installed, launching `birdhackbot` without flags must open the primary guided application. First use guides provider setup/sign-in; later launches reuse appropriate preferences and offer a new assessment or an existing session.
- Guide the operator from a plain-language goal through target scope and execution permissions to a concise review before testing starts. Make the active scope, provider, and permission mode visible. Remembering preferences must not silently grant broader execution permissions.
- Manage supporting services such as the local subscription bridge through the application. Normal startup must not require a second terminal, manual token-file handling, or knowledge of internal ports.
- Explain choices where they arise, offer relevant next steps, and show progress, results, and an obvious stop control. Errors must say what happened and how to recover; technical diagnostics remain available on demand.
- When approval is required, explain the proposed action, affected target, and expected impact in plain language, with the exact invocation available for review. Honor the current approval policy and avoid redundant questions. Clear presentation must preserve actual runtime enforcement.
- Reveal advanced settings when needed. Flags remain useful for automation and troubleshooting, and both interfaces must use the same worker, scope, approval, and evidence contracts.

The first guided lab surface now starts with `birdhackbot` without flags and is shared by `birdhackbot-orchestrator`. It discovers local model choices, remembers provider preferences, manages the subscription bridge for an existing sign-in, reviews the goal/scope before starting, and serializes action approvals across workers. Flags still select the standalone development worker. First-time subscription sign-in, packaged operation outside the checkout, reopening assessments, and unfamiliar-operator acceptance remain gaps; the complete product requirement is not yet met. `docs/runbooks/acceptance-gates.md` defines the usability check.

## User interface surfaces

The product has two UI adapters over the same assessment runtime:

- **Terminal client.** Keep the existing [Bubble Tea](https://github.com/charmbracelet/bubbletea) implementation for the interactive CLI, with its scripted/headless path for automation and air-gapped operation. Bubble Tea owns terminal input, rendering, and local interaction only; it must not own assessment state, worker execution, approvals, or evidence semantics. The current dependency is the v1 module and remains pinned until the UI boundary is stable. Upstream now documents a v2 module and an upgrade path, so migration is a separate compatibility task rather than an incidental dependency change.
- **Web application.** Add a browser client over a versioned Go HTTP API. The server owns assessment lifecycle, persistence, approvals, event streaming, artifact/report access, and authentication. The browser must never call the LLM provider or execute tools directly. The first web slice should expose assessment creation, explicit scope review, start/stop, approval decisions, live progress, and report/evidence links.

Both surfaces call one application service above `internal/assessment`; neither should call `internal/workerloop` directly. The local subscription bridge remains an inference-provider adapter and is not the user-facing assessment API. Bind the web server to loopback by default until authentication, authorization, CSRF/origin handling, and deployment policy are implemented. Do not add an embedded desktop wrapper until the browser product is useful; a Wails-style wrapper can be considered later if native packaging is required.

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

Output-derived assessments/signals remain labeled hints for model interpretation; they no longer select recovery actions or force task blockage. The regex target/prerequisite inference and synthetic authoritative-facts layer have been removed. Full evidence can be read from the recorded artifacts when previews are insufficient. A future findings pipeline must distinguish observations, hypotheses, local reproductions, and target-validated findings.

## Context ownership and bounds

Persisted worker state retains all execution observations in order, including repeated invocations with different logs/timestamps. Model context is a separate deep copy. Under pressure it drops older output bodies first while retaining execution identities and evidence references, then older conversation and retrieval excerpts; latest output can become a marked excerpt with a log reference. Original goal, done condition, policy/scope, current feedback, plan history, and the newest operator message are protected. Oversized protected context fails visibly before inference rather than silently discarding instructions.

The default client input ceiling is 48 KiB of combined message text. Worker views reserve 8 KiB for instructions and JSON quoting; the final client check applies to the actual message text. This is a byte ceiling, not an exact model tokenizer or a guarantee for arbitrary provider context windows. Output allowances remain explicit provider settings. Incomplete provider responses (`length` or `content_filter`) never become executable decisions.

Recent operator messages preserve line breaks and indentation. Older conversation notes are bounded excerpts, not authoritative semantic memory. Deep copies isolate UI snapshots and compact model views from mutable execution state. Long investigations still require evaluation of retrieval quality and model-specific context sizing.

### Active-context strategy

The runtime separates local application context from the model-visible context. Local state owns authorization, scope, approvals, budgets, execution records, and durable evidence; the model receives a bounded projection of that state for its next decision. This follows the same separation described in the [OpenAI Agents context guidance](https://openai.github.io/openai-agents-python/context/): application state is not implicitly conversation history, and history management must be explicit.

The projection keeps the original goal and completion condition, policy and scope, current evaluator feedback, plan history, newest operator input, and references to recorded evidence. It compacts older output before older conversation and retrieval excerpts, marks omitted material, and fails visibly if protected instructions alone exceed the configured ceiling. Retrieved material and model-authored summaries remain untrusted inputs; they can inform a decision but cannot replace runtime facts or broaden scope. Conversation rollover is bounded and persisted, while execution records remain lossless locally.

The current implementation covers bounded projections, protected anchors, conversation rollover, plan history, and visible truncation. The next context increments are relevance-ranked retrieval across a run, a durable multi-worker event history, and independently verified run summaries. Each should be added only with a fixture that demonstrates the information loss it prevents; context size alone is not a reason to add another compaction layer.

## Persistence and stopping

Session state is one local JSON snapshot per worker session, written through a temporary file and atomic replacement. Version 2 persists the original turn limit and consumed turns. Resume never replenishes that budget. Version 1 snapshots remain inspectable JSON but cannot be resumed because they lack reliable budget accounting. A pending invocation with an unknown outcome is never replayed automatically; inspect its evidence before starting a new task. This is not a multi-worker event store and does not provide exactly-once recovery of external tool effects.

Canceled runs persist an aborted outcome. The TUI waits for an active worker to return and finalize before quitting. Task preparation runs outside the UI update handler. Per-action approval uses the text interface; the TUI currently requires explicit session-level approval to avoid competing stdin readers.

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

`internal/assessment` calls the shared adaptive worker loop with separate task working directories and inherited scope/approval rules. Mutable directories are separated for ordinary work; they are not filesystem security boundaries. The coordinator proposes a batch of up to two independent tasks, reviews their results, and decides the next batch or final synthesis. Dependencies must reference completed earlier tasks. The built application test runs two independent capability tasks, then a dependent validation task, using the same path an operator starts from. No fixed phase sequence or separate worker execution engine is introduced.

The initial limits are four coordinator rounds, eight tasks, six steps per task, and 96 model calls shared by planning, workers, and evaluators. Actual usage is recorded when the provider returns it; missing usage stays explicit. Decisions, full worker contexts, progress snapshots, execution evidence, and final state are retained locally. Ctrl-C cancels the shared context and waits for workers to finalize before reporting an aborted assessment. Resume/replay of whole assessments remains deferred.

All roles inherit the same model client. Guided local preferences include explicit reasoning effort and an output-token allowance (initially 32,768), with a ten-minute request timeout. Subscription requests retain the bridge's supported wire contract and current timeout. Provider timeouts remain incomplete results, distinct from an operator stop. Server context and parallel load settings remain externally managed; the coordinator permits at most two concurrent requests.

The coordinator may request one correction of an invalid JSON decision under the same budget; rejected proposals never execute or become evidence. Worker questions are serialized through the same console as approvals, and answers return to the worker's existing step budget. A proposed final worker answer reaches the semantic evaluator alongside recorded evidence; the answer itself is not new execution evidence.

Draft findings require recorded evidence paths. A model-reported reproduction also requires a completed dependent validation task and evidence from that task; those structural checks do not independently verify its security meaning. Reports retain the distinction between candidates and model-reported reproductions and require operator review. Failed workers and exhausted budgets remain visible, including in partial reports.

## Adaptive investigation and source-assisted assessment

Planning is adaptive and proportional to the task. Simple work may run directly; broader or dependent investigations need a visible plan. The coordinator can add, revise, cancel, or delegate tasks as discoveries arrive. Each delegated task carries an objective, inherited scope and permissions, relevant evidence, a budget, and a completion criterion. Workers return evidence, conclusions, uncertainty, and blockers. The coordinator reconciles their results and controls follow-up work within the shared budget; worker count alone must not drive delegation.

Software discovery must support vulnerability research during the assessment. Preserve the observed product/version and supporting evidence with its uncertainty. As useful software identities become available or change, query advisories and vulnerability references using permitted online sources or imported local snapshots. Check affected versions and relevant prerequisites; record source links, retrieval dates, and snapshot versions where applicable. Show the freshness and coverage limits of offline research. Research queries should use the software facts needed for matching without unnecessary customer or internal-target identifiers. A lookup failure is an explicit research gap, not evidence that no vulnerabilities exist.

Advisory matches become investigation candidates. The coordinator prioritizes useful leads and delegates scoped validation, revising its plan from the results. A matching software banner or CVE record alone is insufficient to report a confirmed target vulnerability. Online research does not expand the authorized testing scope.

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
