# Discoveries

## Worker foundation rebuild — 2026-09-20

The [worker audit](docs/worker-foundation-audit-2026-09-20.md) records the replacement of competing planner/reviewer/step-evaluator paths with one adaptive worker, removal of inferred target/prerequisite facts and failure ranking, bounded context views, persisted turn budgets and single-owner progress writes. Obsolete modules and tests were removed rather than retained as fallback behavior.

Live checks exposed two additional defects that deterministic checks had missed: plan history was absent from evaluator context, and multiline output could resemble metadata and lose a line in model reports. Both were corrected. The same controlled fixture then passed three consecutive Daybreak runs with independent log, content/hash, state, budget and context inspection. Four preceding runs remain recorded as failures. Qwen's separate diagnostic completed its assignments but did not honor the single-worker scenario or report both content lines; it is not an acceptance pass. CI has nine terminal scenarios, including generic orchestration with dependent validation; full worker/pentest acceptance remains open.

Continuity and evidence, not a task list. Updated 2026-09-19.

## Product decisions

- Build System Verification's production security testing platform. The orchestrator is the intended primary product; workers share one engine.
- Implementation order is core cleanup, subscription API wrapper, orchestration, then source-assisted assessment.
- Astra is the development/review model. The intended OpenAI pentest runtime is Daybreak on GPT-5.6 Sol, alongside local models. The first subscription slice successfully requested `gpt-daybreak-blue-latest`; the backend reported resolved model `gpt-5.6-sol`. Access is account-dependent.
- Keep solutions proportional to demonstrated problems. Define done, validate it, and defer hypothetical cases. A broad roadmap does not authorize building every subsystem in the first slice.
- Priority clarified after initial coordinator experiments: validate the shared worker and generic orchestrator before expanding knowledge/source integration. The current delegated path uses the same adaptive worker loop as standalone work; repeated real-model capability acceptance and independent finding review remain open.
- A subscription inference adapter must preserve BirdHackBot's execution ownership. An independently executing embedded agent has a different contract.

## Baseline assessment

- The [competitive assessment](docs/competitive-assessment-2026-09-19.md) reviews current commercial and open-source approaches. Multi-agent, source-assisted, local/subscription capabilities already overlap our direction. Proposed differentiation must be measured in assessment usefulness, evidence quality, operator effort, and fix verification; no competitive superiority has been demonstrated.

- The pre-implementation checkpoint is `95edae1`, tagged `checkpoint/pre-core-rebuild-2026-09-19` and pushed to GitHub.
- The [assessment](docs/code-assessment-2026-09-19.md) found eight contract failures despite passing race tests and 72.0% statement coverage. They concerned command interpretation, child cancellation, unsupported completion, stale evidence, scope inference, and missing behavior validation.
- The recommendation was to keep Go, this repository, useful UI/inspection components, and one worker concept while correcting the core contracts. The legacy orchestrator remains reference material.

## Kali, knowledge reuse, and offline inventory — 2026-09-19

- Product requirement: combine Kali tooling, adaptable playbooks, reusable custom applications, and discovery-driven vulnerability research. Fully air-gapped assessments must use local inference for every role and local research/dependencies, with no cloud fallback. Better verified vulnerability discovery is the primary competitive objective; no such advantage has been demonstrated yet.
- Read-only host inspection confirms Kali Rolling (`VERSION_ID=2025.4`) with installed packages `nmap 7.98+dfsg-1kali1`, `exploitdb 20260205-0kali1`, and `metasploit-framework 6.4.112-0kali1`. Local resources include `/usr/share/exploitdb/files_exploits.csv` and `/usr/share/metasploit-framework/db/modules_metadata_base.json`. These are useful exploit/module resources, not evidence of comprehensive CVE coverage or current upstream freshness.
- The installed `/usr/share/nmap/scripts/vulners.nse` calls `https://vulners.com/api/v3/burp/software/` with software/version information. Its presence does not provide offline vulnerability research. No target scans or resource updates were run for this inventory.
- Four playbooks survive under `docs/archive/playbooks/`; the active `docs/playbooks/` directory is empty. `legacy/internal/playbook/playbook.go` implements Markdown loading and keyword matching. These assets are not integrated into the rebuilt worker.
- `legacy/internal/cli/tool_forge.go` creates session-local helpers with file hashes, purpose, invocation, and bounded repair; `tools_manifest.go` summarizes that session's tools. This is useful reference material, not a verified cross-assessment reuse catalog. The active core can execute commands but has no corresponding managed catalog.
- Legacy `Network.AssumeOffline` asks for permission to browse/crawl and still permits model use with a configured endpoint. It does not enforce an air gap. The active runtime's lack of network enforcement is already documented.
- Carry forward useful content and contracts in bounded slices. Do not restore legacy code wholesale or claim offline support from a setting. Architecture, tasks, and acceptance gates now include offline operation and independently scored discovery comparisons.

## Core cleanup observations

- Explicit argv and explicit shell scripts remove the need for command-splitting and shell-detection heuristics.
- Approvals must describe the prepared invocation and cwd. Unknown approval decisions must not permit execution.
- Process success alone does not establish task completion. Existing tests that accepted unrelated output or evaluator failure needed corrected assertions.
- Earlier failures belong in history; they must not replace current execution truth merely because they have a higher severity score. New tasks start with fresh execution results.
- Tool stdout/stderr can stream directly to local files. Bounded previews keep memory use predictable without discarding full evidence.
- Atomic snapshot replacement addresses torn JSON reads/writes; it does not establish exactly-once external execution or a multi-worker journal.
- TUI shutdown must wait for the worker to finish. Per-action approvals currently use text mode to avoid competing stdin readers.
- The repeat-run harness used a stale fixed session location. It now passes an explicit per-run session directory; validation must inspect those actual artifacts.
- Historical audit probes are stored as `.go.txt` fixtures so `go test ./...` does not discover incompatible scratch packages.
- Runtime target-scope enforcement, robust multi-worker recovery, and a findings/report service remain product gaps. Removing misleading scope inference is not equivalent to implementing a sandbox.

## Validation evidence

- Focused regressions, deterministic CI, and the full race-enabled Go suite passed. A final relative-executable cwd correction also passed the affected package with the race detector.
- Six repeated local-model checks used `lmstudio-community/qwen3.5-27b` on the configured LM Studio server: 3/3 explicit-shell runs and 3/3 direct-argv runs completed with the exact requested literal text `hello world; printf two`.
- Evidence root: `sessions/core-live-20260919-7eLNPJ/`. The `runs/` and `direct-runs/` subdirectories each contain three independent sessions. Saved context, semantic evaluation records, final state, command logs, and full stdout/stderr artifacts were inspected. The task fixture remained empty.
- These are focused execution/completion checks. They do not establish generic pentest effectiveness, scope enforcement, or comparative superiority.

## Subscription integration evidence

- OpenCode's direct OAuth/Responses provider and Cline's subscription support confirm the provider pattern. The first BirdHackBot adapter reuses Codex's file sign-in/refresh and sends inference directly; it never starts a Codex agent turn. Sources and setup: [subscription runbook](docs/runbooks/subscription-bridge.md).
- No API-key fallback exists. The bridge has loopback-only bearer authentication, no upstream tools, propagated cancellation, one auth refresh/retry, and explicit access/limit failures.
- The first transport probe failed because the terminal Responses event omitted already streamed completed items. A regression now covers that actual backend behavior; partial output still cannot become success.
- After that correction, 3/3 live worker runs used `gpt-daybreak-blue-latest` and completed the harmless direct-argv printf task. Evidence root: `sessions/subscription-live-20260919/`. Each run executed one local command, produced the exact expected stdout, passed semantic evaluation, and persisted `completed`/`done` state.
- Final model contexts, prepared invocations/cwd, stdout/stderr, evaluation records, and saved state were inspected. The fixture stayed empty. No bridge or subscription credential values were found in these session artifacts.
- Deterministic CI (including all three entrypoint builds) and race checks for the bridge, local authentication, LLM client, interactive CLI, and worker command passed. Auth expiry/refresh failures and limits use deterministic fixtures; no intentional account exhaustion or live token expiry was required.
- These are inference/worker integration checks. They do not establish pentest effectiveness, customer readiness, or an independent audit of provider-side Daybreak settings. The backend endpoint and file-store dependency are explicit compatibility limits.

## Guided orchestration and current model setup — 2026-09-19

- The primary no-flag application now guides provider selection, explicit scope, per-action approval, bounded two-worker delegation, operator questions, evidence review, and a draft assessment report. Subscription bridge lifetime and its temporary client credential are managed by the application for an existing sign-in. Whole-assessment resume, first-time sign-in, independent finding verification, and enforced scope isolation remain gaps.
- The built application's deterministic PTY checks cover success, saved-provider reuse, denied execution, Ctrl-C during a child command, cancellation before execution, provider setup recovery, and worker questions. CI and affected-package race checks passed. Actual requests are checked for the selected local reasoning and output allowance.
- User-selected local baseline: `qwen/qwen3.8-27b`, Q6_K, low reasoning, at most two concurrent requests. LM Studio's loaded-instance metadata reported context length 50,176 and parallelism two. Its model metadata default is xhigh, so the client sends explicit `reasoning_effort: low`. The guided local profile requests 32,768 output tokens with a ten-minute request timeout. The user noted that substantial output capacity may be necessary even at low reasoning. Server load settings were read, not changed.
- Historical Qwen 3.5 runs remain labeled with that model; they do not establish Qwen 3.8 acceptance. Early guided debugging exposed array/string mismatch in the finding schema and a discarded proposed worker answer in semantic evaluation. The schema now uses string arrays for steps/remediation, and the evaluator receives the proposed answer alongside existing evidence. A bounded coordinator correction handles rejected proposals without executing them or accepting their claims.
- The first Qwen 3.8 full planning request hit the old 90-second timeout before tool execution. A native low-reasoning diagnostic also timed out at 90 seconds. These are inference/time-budget observations, not evidence of a model refusal. The larger-budget application run subsequently produced a plan and entered worker execution. The CLI now distinguishes provider timeout from operator cancellation.
- An initial guided Daybreak run completed discovery and dependent validation but rejected final citations to worker-created files not registered as execution evidence. The prompt now exposes the exact recorded evidence catalog and its one correction requests replacement of all invalid references. The runtime continues to reject unregistered citations. No target-specific logic or extra repair loops were introduced.
- A subsequent Daybreak diagnostic exhausted the discovery worker’s six steps and was deliberately stopped, preserving its aborted report. Inspection found that model context kept displaying the original budget after turns were consumed. Each turn now exposes its actual remaining step count; the execution limit is unchanged, and the terminal question/answer regression checks the decrement.
- The larger-budget Qwen 3.8 pilot finished all four workers (14 model calls, 100,478 provider-reported tokens), but the final report was rejected because its validation task had no dependency links; it also omitted the requested missing-reference GET. Actual logs confirm the seeded admin exposure, the fixed 403 response, and debug=false. This is not a passing local assessment. The run started before the explicit evidence catalog and step-countdown changes; Qwen needs further task/report-contract validation on the current build. No model refusal was observed in this synthetic pilot. Review: `sessions/guided-live-20260919-r5vjg4v3/qwen38-02-review.json`.
- Fixture truth is fixed in `testdata/assessment-lab/README.md`: one synthetic access-control defect, a fixed counterpart, a non-applicable advisory, and an unavailable reference. Local terminal/debug evidence is under `sessions/guided-live-20260919-r5vjg4v3/`; corresponding Qwen assessment directories are recorded in its transcripts. Daybreak uses a clean synthetic workspace to keep cloud context limited to test instructions and fixture evidence. These runs do not establish real-CVE coverage, air-gapped operation, customer readiness, or superiority to competitors.

## End-user network smoke and provider-context validation — 2026-09-20

- The no-flag `birdhackbot` path was exercised as an operator against the explicitly allowlisted `scanme.nmap.org` target. The session collected the goal and exact scope, displayed the coordinator plan and worker dashboard, required approval for every action, and preserved local evidence. The local Qwen 3.8 run completed bounded DNS and service discovery, then was deliberately stopped before a pending HTTP request when the provider was switched; its report and aborted state are retained.
- The first Daybreak run performed a conservative five-port service scan, one HTTP GET, and read-only NVD research. It exposed a context failure after a verbose model-generated report: the worker projection reached 45,790 bytes against the 40,960-byte allowance and coordinator input reached 51,827 bytes against the shared 49,152-byte ceiling. No unsafe action ran.
- The guided subscription profile now keeps the conservative 48 KiB local-model ceiling but gives Daybreak an explicit 128 KiB client ceiling, persists it in assessment state, and displays it during review. The same Daybreak flow then completed the bounded scan, one GET, Ubuntu CVE-tracker research, and final coordinator synthesis at 70 KiB of input. No vulnerability was reproduced or confirmed; the report retained one configuration/package-revision-dependent Apache candidate and kept OpenSSH leads unconfirmed.
- The completed synthesis still persisted overall status `incomplete` because two bounded advisory workers exhausted their turns before a follow-up worker supplied the final correlation. The model summary says “Assessment complete,” so runtime status presentation needs a deliberate `completed-with-gaps` or equivalent contract before this is considered polished. This is a reporting/UX issue, not evidence that the target is vulnerable.
- All target actions were limited to the allowlisted service/version scan and one ordinary HTTP GET. Advisory lookups used product/version terms only and did not contact the target. Evidence roots: `sessions/assessment-20260920-123322-3629643952/`, `sessions/assessment-20260920-124739-971752565/`, and `sessions/assessment-20260920-132418-4006036503/`.

## Guided Daybreak fixture results — 2026-09-19

After the evidence-catalog and step-countdown corrections, three consecutive unchanged-build runs through `gpt-daybreak-blue-latest` completed from guided startup to report:

| Assessment ID | Model calls | Reported tokens | Outcome |
| --- | ---: | ---: | --- |
| `assessment-20260919-162753-2813045723` | 15 | 92,685 | 4 workers done; expected finding and controls |
| `assessment-20260919-163316-3234652723` | 21 | 113,150 | 4 workers done; expected finding and controls |
| `assessment-20260919-164214-788991229` | 17 | 108,552 | 4 workers done; recovered from a shell syntax error |

- Each report contains one evidence-backed draft finding for the seeded admin exposure, the protected counterpart, non-applicable debug advisory, and actual 404 research gap. Registered citations, dependency links, inherited scope, separate task directories, decreasing budgets, final contexts, and full command responses were inspected. No false second finding was reported for the fixed or debug controls.
- The last run recovered from a model-generated shell command using unsupported substitution syntax. The error, repair, and successful output remain in the evidence; no runtime workaround was added.
- The application managed its own subscription bridge and temporary credential. No bridge credential directories remained after exit, and no credential patterns were found in the inspected session artifacts. The fixture server was stopped. The checkout executable was rebuilt and matched the tested binary.
- Evidence and all failed pilots: `sessions/guided-live-20260919-r5vjg4v3/`. Passing assessments live under `daybreak-lab/sessions/`; terminal transcripts are in `daybreak-lab/`. Inspection record: `completed-daybreak-reviews.json`.
- CI, nine terminal scenarios, and affected-package race checks passed. This is a synthetic workflow result with existing subscription sign-in, not proof of real-CVE coverage, strict scope enforcement, unfamiliar-operator usability, Qwen acceptance, air-gapped operation, or competitive superiority. Local and cloud pilots used different instruction workspaces and manual approvals; they are not a matched model-performance comparison.

## Source-assisted assessment and interrupted validation — 2026-09-19

- Added `testdata/source-lab/`: a synthetic tenant authorization defect, a fixed API counterpart, authentication controls, an old non-applicable advisory, and a deployed Git revision differing from default HEAD. Fixture truth and source hashes were recorded before live inference. Deterministic fixture checks were added to CI, which passed with the nine guided terminal scenarios. No production runtime changes were needed for these source investigations.
- Two guided Daybreak assessments completed and passed inspection: `assessment-20260919-165917-843355950` (19 calls, 156,188 reported tokens) and `assessment-20260919-173337-2947254808` (21 calls, 183,715 reported tokens). Each produced the single expected cross-tenant finding, dependent target validation, correct controls, attributable deployed source, and explicit research limits. Actual responses, reports, citations, saved contexts, and state were inspected. Evidence: `sessions/assessment-next-drlavi6_/daybreak-source/`; reviews: `sessions/assessment-next-drlavi6_/source-reviews.json`.
- The third Daybreak run, `assessment-20260919-173912-279276461`, was deliberately canceled while awaiting action approval during conversation troubleshooting. The current Qwen retest, `sessions/assessment-20260919-165533-2506485338/`, was canceled too. Both persisted aborted reports; neither counts as a passing assessment or demonstrates a provider failure. Qwen supplied validation dependency links correctly in this retest, but final reporting and full fixture coverage remain unvalidated.
- Both fixture servers and assessment processes were stopped. The cause of the conversation responsiveness problem has not been established; waiting application prompts alone do not prove that they caused it. These runs do not complete source-assisted acceptance, local-model acceptance, or comparative discovery evaluation.

Earlier chronological notes are preserved in `docs/archive/pre-core-cleanup-2026-09-19/DISCOVERIES.md`.
