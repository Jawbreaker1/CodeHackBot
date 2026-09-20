# Worker foundation audit and rebuild

This is dated implementation evidence. `architecture.md` owns the active contracts and `TASKS.md` owns acceptance status.

## Decision

Replace the worker's competing control paths while preserving the tested executor, approval contract, evidence capture and application surfaces. The deciding model owns plans, recovery and interpretation. Runtime code owns validation, budgets, recording, approval, execution and termination. This is the same worker for standalone and delegated tasks.

## Findings and changes

| Demonstrated problem | Replacement |
| --- | --- |
| Goal keywords selected a startup-only planner; stored replan conditions had no revision transition. | A short plan can accompany any decision. Plan changes have a visible UI event, saved turn number and preceding execution reference. |
| Separate action reviewer and step evaluator could disagree; a final satisfied step could omit part of the original goal. | One whole-goal evaluator checks the original goal and done condition after actions or completion proposals. |
| Missing paths, command errors and password signals could terminate recovery before model reasoning. | Actual outcomes return to the deciding model. It may revise its plan, ask for missing input, or explicitly report a blocker within the same budget. |
| Target/prerequisite regex guesses were elevated to curated execution facts. | Removed that inference and the synthetic facts layer. Observations retain their original provenance. |
| Severity ranking favored old failures; deduplication collapsed repeated commands with different observations. | Keep every recorded execution in order with timestamps, invocation and log references. |
| Conversation normalization destroyed line breaks; question callbacks bypassed retention limits; prompts had no overall bound. | Preserve multiline answers through one append path. Build a separate bounded model view with marked excerpts and retained references. Protect task/policy/newest input or fail explicitly if it will not fit. |
| Multiline output could appear to introduce packet metadata, and live workers omitted an output line while claiming exact contents. | Quote model-facing output strings so newlines remain data. Exact-content evaluation must compare every requested line against output evidence, not a summary. |
| Resume granted a fresh turn allowance and could encounter uncertain pending actions. | Persist original limit and consumed turns. Refuse automatic replay of unknown outcomes. State version 2 rejects old version 1 resume. |
| Progress persistence errors were ignored; UI and worker could become competing state writers. | Stop on configured recording failures, persist before action effects, and give progress writes one worker-side owner. Race regression covers the UI integration. |
| Response parsing repaired ambiguous aliases and could extract executable text from malformed outputs. | One documented decision schema; malformed decisions consume a turn and produce feedback without execution. Truncated provider responses cannot become decisions. |

The retired worker planner, action reviewer, step evaluator, their parsers and obsolete diagnostics/tests were deleted. Mode names remain only for standalone chat input classification. Whole-goal evaluation is in `internal/workergoal` and `internal/workerloop/evaluation.go`.

## Validation and limits

Deterministic regressions cover recovery, plan changes, omitted goal requirements, negative evidence, unavailable evaluation, malformed decisions, repeated observations, immutable snapshots, context bounds, approval denial, cancellation, operator answers, persisted budgets and pending-action refusal. The built application has nine terminal scenarios, including generic orchestration with dependent validation. A race check caught duplicate UI/worker writes; the fix passed affected-package race checks.

Live evidence is under `sessions/worker-foundation-20260920/`. The controlled fixture requires one worker to attempt an absent file, revise its plan, read a two-line observation and compute its SHA-256. Only three reviewed read-only commands are allowed. It tests lifecycle and evidence plumbing; it is not a vulnerability-discovery or generic pentest capability acceptance test.

The first Daybreak run (`assessment-20260919-223101-692129103`, UTC directory date) recovered and collected correct evidence but exhausted six turns because the evaluator could not see historical plan changes. This exposed a real context omission: only the current plan was available. Plan history and guided plan visibility were added; the failed run remains preserved.

The Qwen run (`assessment-20260919-223219-829422805`) used the confirmed `qwen/qwen3.8-27b` profile, low reasoning, 32,768 output allowance and at most two requests concurrently. The coordinator split the explicitly requested single-worker exercise into three tasks. It finished nine model calls, but the final content summary omitted `status: available`. This is not a pass for the requested scenario, nor evidence of adaptive recovery inside one Qwen worker. Full logs retain both lines. Do not solve that semantic gap with fixture-specific runtime rules.

Three subsequent Daybreak runs completed their control flow but failed independent artifact review: all worker answers omitted the second file line (`status: available`), and evaluators accepted those answers. The rendered output let that line resemble another metadata field. The three failed runs are `assessment-20260919-223541-1659837083`, `assessment-20260919-223659-3109474765`, and `assessment-20260919-223806-1521155641`. Output fields were then quoted, and the generic exact-content evaluation instruction was tightened. These failures are preserved and are not acceptance passes.

Three consecutive Daybreak runs after that evidence-format correction passed independent artifact review:

- `assessment-20260919-224223-1411006717`
- `assessment-20260919-224342-3313759273`
- `assessment-20260919-224451-3720559777`

Each used one worker and the same three separately approved read-only commands, preserved initial/revised plans, kept the original goal/scope/budget, and reported both file lines and the correct hash in worker and final answers. Actual stdout files, command logs, chronological history, all pre-inference snapshots, and visible plan updates were inspected. The independent check is `sessions/worker-foundation-20260920/review.py`, with detailed results in `review.json`. It explicitly fails the four earlier Daybreak runs and passes only these last three. This passes the controlled recovery fixture, not broad worker or pentest acceptance.

Remaining acceptance work: generic multi-step investigations across varied capability fixtures, accurate local-model reporting, consistent advanced standalone provider settings, long-context/retrieval quality and exact provider token sizing. The byte ceiling is not an exact tokenizer. Scope isolation, full assessment resume and independently verified security findings remain separate product requirements.
