# Code assessment — 2026-09-23

This review checks the current implementation against `docs/architecture.md`,
`TASKS.md`, `ROADMAP.md`, and `docs/runbooks/acceptance-gates.md`. It concentrates
on the coordinator/worker boundary, model context and budget, session durability,
findings, and the built CLI and HTTP paths. It is a focused code and behavior
review, not a claim that every function or a live target has been audited.

## Verdict

Keep the present architecture and codebase. There is a real shared worker loop,
a working two-worker coordinator with dependencies, evidence capture, an inference
bridge, and two UI adapters. The deterministic application checks and affected
package race tests pass. Five reproducible defects, mainly in the browser path, still
prevent calling the foundation fully sound. Scope isolation, air-gapped
deployment, independent finding verification, and comparative effectiveness
remain separate unpassed product gates.

## Confirmed defects, in repair order

1. **P1 — Live coordinator chat bypasses and loses the shared model-call budget.**
   The coordinator reserves calls with `assessment.meter` in
   `internal/assessment/coordinator.go:90-110`. Browser chat calls the session
   client directly in `internal/webapp/server.go:1791-1794` and only increments a
   copy of `r.state.Usage.Calls` after success at line 1823. The next coordinator
   snapshot replaces that copy at lines 1682-1688. A focused probe started from
   two recorded calls, made one chat request, then applied a coordinator
   snapshot: the visible count went from three back to two. Chat is therefore
   outside the enforced session limit and its failed calls/provider token usage
   are not accounted for. The CLI conversation path also calls its bare client
   in `internal/guided/app.go:455-462`; include both surfaces in the repair.
   Make every model request reserve and record through one session-owned meter,
   and test simultaneous worker/chat requests and resume accounting.

2. **P1 — Active coordinator conversation has no prior turns in its reply
   context.** Browser chat constructs only a system message and one user message
   containing a compact assessment snapshot plus the current text
   (`internal/webapp/server.go:1783-1794`). That snapshot has goal, scope,
   results, and live progress, but no transcript
   (`internal/webapp/conversation.go:91-130`). A two-turn fixture sent a unique
   fact, then asked for it: the second model request contained neither the fact
   nor the first reply. The CLI uses the same fresh two-message pattern
   (`internal/guided/app.go:455-462`). Saved conversation is used at planning
   boundaries, so this is specifically a live discussion failure. Supply a
   bounded, ordered dialogue projection, with durable references when older
   turns leave the window; verify a follow-up while workers run and after resume.

3. **P1 — Main-chat messages can be delivered to a worker as an answer.** When
   any worker question is pending, `message` takes the first entry in a Go map,
   removes it, and sends the operator's entire chat text to that worker without
   consulting the coordinator (`internal/webapp/server.go:1757-1765`). A focused
   probe typed “Coordinator, explain what the workers found” into the main
   composer while one worker question was pending; the worker received that
   sentence as its answer. With two questions the recipient is also unspecified
   because map iteration is unordered. The UI already has an explicit
   question-ID answer route (`internal/webapp/server.go:1409-1421`); keep worker
   answers there and reserve the chat composer for coordinator discussion.
   Test this while one and two workers await answers.

4. **P1 — The analysis view can show superseded findings as current risk.** The
   browser assessment view and analysis read every plan revision's findings
   (`internal/webapp/server.go:2030-2032`,
   `internal/webapp/analysis.go:92-100`), whereas the formal report uses only the
   final revision (`internal/assessment/report.go:54-60`). A focused probe with
   one provisional candidate followed by a final decision with no finding left
   the candidate in the analysis risk list. The customer view repeats the
   all-revisions aggregation (`internal/webapp/server.go:1511-1529`). This can
   mislead prioritization and make the GUI disagree with the report. Define one
   current-finding projection for every read model and report; retain old plan
   findings in the history only. Test candidate promotion, withdrawal, and
   duplicate symptoms across sessions.

5. **P2 — Web session persistence errors are not consistently treated as run
   failures.** `start` changes the in-memory state to `started/starting` before
   saving it; on save error it cancels the context but does not restore that
   state (`internal/webapp/server.go:1615-1624`). A focused file-error probe
   left the draft permanently `started=true`, blocking retry. Progress and
   snapshot writes also discard errors or put one in `persistErr` that is never
   consumed (`internal/webapp/server.go:1682-1715`). Worker task evidence has a
   stronger stop-on-write-error contract, but web navigation, transcript, and
   status may diverge from disk. Roll back a failed start and propagate later
   metadata write failures to the run or visible error state. Test a write
   failure during start and one during an active assessment, then reopen it.

The temporary probes were removed after recording their expected failures;
production code and the normal test suite were left unchanged by this review.

## What is working and what is proven

| Contract | Current evidence and limit |
| --- | --- |
| One adaptive worker engine | Standalone and delegated work use `internal/workerloop`; explicit Bash invocations, approval before execution, process-group cancellation, streamed logs, bounded model views, and whole-goal evaluation are implemented. The controlled recovery fixture passed three inspected Daybreak runs. Broader generic worker acceptance remains open. |
| Orchestration | `internal/assessment` schedules up to two separate workers, retains results and evidence, supports dependent later tasks, and shares a budget among its own planning/worker/evaluator calls. CLI and HTTP deterministic fixtures exercise the real built applications. Chat budgeting and repeated real-model generic acceptance remain open. |
| Provider access | The local subscription bridge and local model profiles work in focused live checks. Daybreak and Qwen can be selected in separate browser sessions. Qwen's full guided assessment contract and actual provider token headroom have not passed acceptance. |
| Sessions and UIs | Browser draft/assessment discovery, resume, grouping, deletion, approvals, worker progress, registered images, and Markdown report links exist. The terminal uses the same assessment engine. Live chat continuity, write-failure handling, completed-session follow-up, unfamiliar-operator usability, and a shared application-service boundary remain open. |
| Context | Coordinator and each worker have separate bounded model inputs; task state and evidence remain on disk. The context inspector shows saved requests/sections. Current compaction is largely excerpts and truncation; it is not proven reliable semantic memory or relevance-based retrieval for long runs. |
| Research, analysis, reporting | A small versioned strategy catalog, model-led `load_strategy`, approved local/connected research via existing tools, Playwright captures, structured draft findings, customer aggregation, and Markdown reports exist. Advisory freshness/coverage, source-to-deployment attribution, independent verification, retesting, formal export formats, and cross-session finding lifecycle are incomplete. |

## Remaining architecture work, prioritized

1. Repair the five confirmed defects. Keep the shared runtime contracts; add
   permanent user-path regressions for the exact behaviors above. While doing
   so, move lifecycle/model-request ownership out of the 2,218-line
   `internal/webapp/server.go` into the application service already called for
   in `TASKS.md`. Extract along actual ownership boundaries, without a broad
   rewrite. Reconcile the already tracked final-status mismatch after a failed
   worker is successfully recovered in a later round; keep the failed attempt
   visible while making the final outcome and remaining gaps agree.
2. Pass foundation acceptance: varied bounded tasks with discovery,
   observation, recovery, blocker/denial, and dependent validation; three
   inspected real-model runs per chosen scenario, separately for Daybreak and
   Qwen. Measure planning quality, actual model usage, operator effort, and
   whether relevant facts survive multiple plan revisions. Finish the
   source-assisted fixture's third Daybreak trial and unavailable-source case.
3. Establish customer deployment boundaries: enforce engagement scope outside
   prompts, support an actually isolated air-gapped run with local inference and
   dated local research data, and add web authentication/origin protection
   before remote access. The current loopback preview and local-model setting
   do not satisfy those gates.
4. Expand the measured capability layer: inventory Kali tools and local
   Exploit-DB/CVE coverage, retrieve applicable playbooks with provenance,
   support versioned reusable helpers, and correlate observed software with a
   pinned source revision. Validate advisory applicability and deployed-target
   effects rather than promoting a match or local source hypothesis to a
   confirmed finding.
5. Build a reviewable finding lifecycle across sessions: independent evidence
   verification, candidate/reproduced/resolved state, targeted retest,
   cross-session deduplication by root cause, and formal report exports driven
   from the same current-finding projection. Then compare independently
   verified unique findings, misses, false claims, time, and model usage against
   matched general and specialist harnesses on held-out fixtures.

## Verification performed

- `./scripts/ci.sh` passed: architecture guardrails, `go vet`, Go tests,
  four builds, built terminal/TUI fixture, built HTTP lifecycle fixture, and
  synthetic source-lab checks.
- `go test -race ./internal/assessment ./internal/webapp ./internal/workerloop
  ./internal/guided` passed.
- Five isolated temporary web-package probes reproduced the defects above.
  Their deliberate failures are audit evidence, not a failing committed suite.
- No live model or target assessment was run for this code review. Prior live
  results are attributed in `TASKS.md` and the acceptance-gate document; their
  narrow success does not close the remaining gates.
