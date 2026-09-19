# Code assessment — 2026-09-19

**Recommendation: retain this repository and Go, selectively replace the runtime core, and build the orchestrator on that core.** Preserve useful UI, inspection, fixtures, and development infrastructure. Keep `legacy/` as reference material. Neither cosmetic refactoring nor a wholesale restart is justified by the evidence.

This is an assessment and proposed implementation sequence, not an implemented architecture change. The existing worker remains the shared-engine concept; its current execution, evidence, and lifecycle implementations should not be treated as fixed commitments.

The product target is System Verification's production security testing platform: orchestration first, source-assisted investigation, reproducible target validation, and professional reporting. Astra is the development/review model. Per the user's clarification, the intended OpenAI pentest runtime is Daybreak on GPT-5.6 Sol, alongside local models. This assessment makes no claim that a particular subscription or model endpoint has been integrated or validated.

## What was examined

- Active revision: `678454ba2033b4fcf8e2373c44098d844f58ddf9`. Product-direction documentation already had uncommitted changes; active Go code was unchanged.
- Read active design/governance documents and traced worker planning, actions, approvals, execution, results, completion, state persistence, provider calls, and CLI lifecycle.
- Counted 8,186 non-test Go lines in active `cmd/` and `internal/`. The largest ownership hotspots are `interactivecli/shell.go` (1,134), `workerloop/loop.go` (750), and `interactivecli/bubble.go` (681).
- Inventoried 43,741 non-test Go lines in `legacy/internal/` and sampled process handling, scope handling, and tool-specific retry/evidence code. This was not a full legacy audit.
- Deterministic CI passed earlier in this session. A fresh `go test -race -coverprofile=/tmp/birdhackbot-assessment-coverage.out ./...` passed: 72.0% aggregate statement coverage. The nested legacy module is excluded from that command.
- Added eight isolated diagnostic probes through a Go overlay. All eight failed their intended contract assertions on the unchanged implementation. They use synthetic packets, local `printf` commands, and a short-lived `sleep` child. No live model or network target was used. These tests demonstrate specific behaviors, not their frequency in real assessments.

The project has a functioning build and substantial tests. Passing tests are useful regression protection, but several currently protect incomplete contracts. The age or identity of the model that wrote the code cannot establish its quality; reproduced behavior can.

## Reproduced defects

| ID | Contract violation and observed result | Consequence |
| --- | --- | --- |
| A1 | `UseShell:false`, executable `printf`, and argument `one; printf two` produced `onetwo` in shell mode. | Literal argument data becomes executable syntax. Approval can describe direct execution while the executor chooses a shell. |
| A2 | Preparing `printf "%s" "hello world"` with shell mode disabled split the quoted text using `strings.Fields`; output contained literal quotes and lost the intended argument grouping. | Ordinary commands are not executed as represented. A typed argument array is missing from the model action contract. |
| A3 | After context cancellation, the fixture's child `sleep` was still alive and `Run` was still waiting on inherited output pipes after 200 ms. The probe explicitly killed its child during cleanup. | Stopping a worker does not reliably stop its tool descendants or promptly return control. |
| A4 | A packet requesting a service version, whose only execution was successful `pwd`, was marked `satisfied`. | Successful process execution is incorrectly promoted to task success without checking the goal. |
| A5 | The same unrelated `pwd` plus a completion claim satisfied a planned step when the evaluator client was unavailable. | Evaluator failure weakens the completion requirement instead of leaving the claim unverified. |
| A6 | A successful current `pwd` was marked blocked when recent results contained an unrelated earlier missing-file failure. | Retained history can contaminate a new task or override recovery. |
| A7 | A private IP alone caused a plan's authorization/scope step to advance, with no explicit engagement scope in the packet. | An address classification is treated as established scope. |
| A8 | A packet with a goal/objective/summary but an entirely empty behavior frame passed validation without any issue. | The validator cannot detect missing behavior rules independently of other missing fields. |

Implementation locations and causes:

- A1/A2: [action preparation](/home/johan/birdhackbot/CodeHackBot/internal/workerloop/actionprep.go:25), [implicit shell selection](/home/johan/birdhackbot/CodeHackBot/internal/execx/executor.go:165), and [approval before execution planning](/home/johan/birdhackbot/CodeHackBot/internal/workerloop/loop.go:293). Approval uses the original `UseShell` flag; the later executor derives its own mode. These are boundary defects, not problems a better prompt can reliably fix.
- A3: [process configuration](/home/johan/birdhackbot/CodeHackBot/internal/execx/noninteractive_unix.go:10) creates a session, while [command construction](/home/johan/birdhackbot/CodeHackBot/internal/execx/executor.go:165) uses default `CommandContext` cancellation without descendant teardown or bounded pipe draining.
- A4/A5: [direct success shortcut](/home/johan/birdhackbot/CodeHackBot/internal/workerloop/directsemantics.go:79) and [planned completion fallback](/home/johan/birdhackbot/CodeHackBot/internal/workerloop/stepsemantics.go:45) accept evidence without establishing its relevance to the active objective.
- A6: [truth ranking](/home/johan/birdhackbot/CodeHackBot/internal/context/truth.go:16) ranks old failures above new successes. [New task construction](/home/johan/birdhackbot/CodeHackBot/internal/interactivecli/shell.go:921) carries previous results without task identifiers or a relevance decision. Always preferring the newest result would also be wrong; evidence needs identity, relevance, and explicit supersession.
- A7: [scope normalization](/home/johan/birdhackbot/CodeHackBot/internal/workerloop/planner.go:171) and [private-address/path shortcut](/home/johan/birdhackbot/CodeHackBot/internal/workerloop/planner.go:207). The [session foundation](/home/johan/birdhackbot/CodeHackBot/internal/session/foundation.go:9) contains only a goal and reporting requirement. Supply explicit engagement boundaries and enforce them through the execution environment; avoid a command-name or text-inference policy engine.
- A8: [packet validation](/home/johan/birdhackbot/CodeHackBot/internal/context/validation.go:41) tests rendered prompt text, but [rendering](/home/johan/birdhackbot/CodeHackBot/internal/behavior/frame.go:48) always adds headings. The existing missing-goal-and-behavior test passes because the missing goal supplies the fatal error.

## Additional gaps established by inspection

**Evidence and persistence need a different foundation.** [Execution](/home/johan/birdhackbot/CodeHackBot/internal/execx/executor.go:86) buffers stdout/stderr without a size limit and writes their contents only after the process finishes. The initial log records that execution started, but does not stream output. Summaries retain only the first five lines; result heuristics inspect those summaries. Full logs exist after normal completion, but that is insufficient for long-running tools, crashes, and large outputs. Artifact references are always nil at this boundary. The worker's reduced result drops invocation, environment, and timing fields; its result type has no run/task/execution identity.

[Session persistence](/home/johan/birdhackbot/CodeHackBot/internal/sessionstate/state.go:27) directly overwrites a JSON file. There is no atomic replacement or durable event journal. Crash corruption and recovery behavior were not fault-injected in this review. Context packets currently mix durable task state, selected evidence, prompt text, and UI state. Adding concurrent workers to this structure would make ownership and recovery harder.

**The product's main capabilities do not exist in the active runtime yet.** The [orchestrator entrypoint](/home/johan/birdhackbot/CodeHackBot/cmd/birdhackbot-orchestrator/main.go:20) is a placeholder. There is no implemented source-to-deployment correlation pipeline or structured findings/report service in the active packages. These are new implementation work, not features unlocked by renaming the CLI.

**Provider support is a prototype boundary.** The [client](/home/johan/birdhackbot/CodeHackBot/internal/llmclient/client.go:14) is a concrete chat-completions client with fixed request assumptions. It has no built-in authentication or streaming, capability negotiation, rate-limit coordination, or aggregate budget enforcement. It preserves usage in its normalized response, which is worth retaining as an idea. Provider-specific reasoning-text fallback should not be a generic machine-action contract. The subscription bridge needs a bounded provider experiment before choosing its implementation; an embedded agent service and a raw inference service have different execution ownership.

**The UI must become a client of the runtime.** Ctrl-C [immediately quits the TUI](/home/johan/birdhackbot/CodeHackBot/internal/interactivecli/bubble.go:184) after canceling its context; worker finalization is not awaited there. Task preparation can call the model synchronously from the UI update handler. Both the TUI and the [approval reader](/home/johan/birdhackbot/CodeHackBot/cmd/birdhackbot/main.go:300) use stdin. These paths need integrated lifecycle/PTY tests; this assessment did not reproduce a terminal-input failure. Keep the display work, but move task ownership, approvals, and stop/finalization into the runtime.

**Evaluation capture is stale.** [The repeat harness](/home/johan/birdhackbot/CodeHackBot/scripts/repeat_worker_run.sh:91) does not pass a unique session directory, then copies from `sessions/rebuild-dev`. It can collect missing or stale session data. Repair artifact attribution before using it to compare old and new behavior. More coverage alone will not resolve these contract gaps.

## Keep, adapt, replace

| Area | Decision | Reason |
| --- | --- | --- |
| Repository, Git history, Go, build/CI | Keep | No evidence warrants a language or repository migration. The active codebase is small enough for staged replacement. |
| Context inspection, execution visibility, local evidence habit | Keep the capability; adapt formats | These make model behavior reviewable. Bind records to durable identities and distinguish raw observations from interpretations. |
| TUI rendering, operator views, repo/config helpers | Reuse selectively | Useful work; detach it from runtime lifecycle and shared stdin ownership. |
| Worker concept | Keep | One worker engine should serve orchestrator delegation and standalone diagnosis. |
| Action/executor/approval boundary | Replace coherently | Explicit argv or explicit shell script, immutable approved spec, isolated cwd/environment, cancellation, streaming, and limits belong in one contract. |
| Task state, evidence selection, completion logic, persistence | Replace coherently | Remove uncorrelated success shortcuts and severity-based truth selection. Separate durable state from context projection. |
| Provider interface and adapters | Redesign | Local and Daybreak runtimes, subscription access, cancellation, structured actions, and shared limits need explicit capabilities. |
| Existing tests and lab fixtures | Retain selectively and strengthen | Keep useful behavioral checks; replace assertions that enshrine incorrect contracts. Add independent positive and negative truth fixtures. |
| Legacy orchestrator | Reference only | It contains useful lessons, including process-group termination, but also tool-specific retries and output steering that conflict with the intended generic runtime. Do not transplant it wholesale. |

## Proposed implementation sequence

1. **Establish a trustworthy execution boundary.** Introduce explicit action/result identifiers and execution specifications; bind approval to the actual specification. Add streamed evidence, bounded resources, descendant termination, and honest aborted outcomes. Convert A1–A3 into tracked regression tests as each change lands. The first complete slice must work under one headless worker and remain callable by the existing CLI.
2. **Introduce the smallest durable run model and orchestrator.** Separate run, task, execution, observation, hypothesis, and finding identities. Make context a projection, not the database. Implement explicit evidence links, unresolved/verified outcomes, safe crash recovery, worker isolation, bounded delegation, shared budgets, and broadcast stop. Remove A4–A8 through these contracts and targeted validator fixes. Start with a coordinator and two workers against a deterministic local fixture; each slice must remain runnable. An LLM can judge evidence relevance, but an unavailable judge cannot silently establish it.
3. **Integrate runtime providers and subscription access.** Resolve the inference/execution boundary with a small compatibility spike, then implement adapters for the intended local and Daybreak backends. Expose shared limits and cancellation; never silently move subscription traffic to paid API billing. This design can be explored during the first slice, but must integrate through the same execution-owning runtime.
4. **Deliver the source-assisted assessment and reporting path.** Connect observed product/version to repository and attributable revision, keep cloned code isolated, preserve licensing/provenance, investigate candidate weaknesses, and validate against the actual scoped fixture. Produce structured findings with reproduction, impact, supporting artifacts, and remediation. A repository hypothesis or local reproduction alone must not become a confirmed deployed finding.

The first production design should stay local and small. A distributed scheduler, broad agent hierarchy, or heavyweight policy language is not required to prove these contracts. Models own investigation strategy; code owns identity, execution, boundaries, persistence, and attribution.

Acceptance should include process-tree stop and recovery, concurrent task isolation, unrelated/stale evidence rejection, negative/fixed/version-mismatched fixtures, and provider failure/limit handling. Major behavioral slices still require the project's repeated real-model runs with actual context and session artifacts inspected. Compare findings, unsupported claims, operator interventions, time, and aggregate usage with the same model in a capable baseline harness. No comparative performance claim is established by this review.

## Local reproduction evidence

Diagnostic sources, overlay mapping, and captured results are in [sessions/assessment-20260919](/home/johan/birdhackbot/CodeHackBot/sessions/assessment-20260919). This directory is intentionally ignored by Git, like other session evidence. The probes are Linux-specific and intentionally fail against the assessed revision; they are not added to normal CI.

```sh
go test -overlay=/home/johan/birdhackbot/CodeHackBot/sessions/assessment-20260919/overlay.json \
  -run '^TestAssessment' -count=1 -timeout=30s -v \
  ./internal/execx ./internal/workerloop ./internal/context
```

No production Go code was changed for this assessment. Existing product-direction edits and the user's unrelated `jokes.txt` were preserved.
