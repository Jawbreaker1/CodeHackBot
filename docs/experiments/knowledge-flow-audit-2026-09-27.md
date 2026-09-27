# Knowledge and tooling flow audit — 2026-09-27

This is an audit of one completed Daybreak browser assessment, not an acceptance result or a recipe for encrypted files. Source: the local `archive-retest-20260927` assessment `web-assessment-20260927-150154.410597718-000002` and its saved worker packets. Do not copy recovered secrets or raw archive contents into this note.

## What the trace establishes

- The coordinator received the local strategy catalog. Its first plan suggested `credential-recovery/SKILL.md` for the recovery task and `configuration-exposure/SKILL.md` for the independent safety task.
- The recovery worker loaded `credential-recovery/SKILL.md` on its first decision. The full guide appeared in every later captured worker input. The safety and final validation workers did not load their suggested guide; suggestions are optional, not automatic instructions.
- The coordinator saw catalog descriptions and worker results, not the full guide text. A worker can ask for a guide with `load_strategy`; it can also verify installed tools, inspect local research data, or make a task-local helper through approved `bash` actions.
- The coordinator prompt still includes a credential-recovery-specific instruction about candidate families and parallel exhaustive search. That policy overlaps the selectively loaded recovery guide. It is a maintainability concern: specialized technique guidance belongs in the catalog/guide path once cross-task evaluation confirms the coordinator can plan from it.
- The assessment completed in three coordinator rounds. Worker decisions used 14/16, 11/16, and 5/16 available turns. The saved tasks contain 11, 10, and 3 command logs respectively: 24 execution actions in total. At least two observed actions returned nonzero status. These counts identify investigation and approval cost; they do not by themselves prove why the model chose each action.

The trace does **not** support a claim that a missing recovery guide caused this run's delay. The guide was present early. It also does not prove the current guide catalog is sufficient for other targets, or that tool-selection advice is consistently applied. The coordinator's limited view of full guides is a deliberate architecture tradeoff that should be tested before changing it.

## Next evaluation

Run the updated application on unrelated, held-out objectives as well as one repeat of the local archive task. Give the coordinator only the objective and scope, not tool names or a command recipe. For each run, inspect saved coordinator and worker packets and record: catalog entries considered, task guide hints, guides actually loaded and when, whether a missing capability triggered a research task, verified tool/data availability, failed invocations and correction, model calls, approval actions, elapsed time, and whether independent evidence supports the result. Include at least one source/API assessment and one software/advisory investigation so success cannot be attributed to a recovery-specific instruction.

Only change the mechanism demonstrated to fail across those traces. If the model consistently chooses the wrong guide, improve catalog descriptions or selection feedback. If the right guide arrives but tool use fails, improve general capability discovery and method validation. If a worker discovers relevant knowledge but the coordinator loses it, improve the evidence-backed handoff. Keep specialized procedures inside selectively loaded guides or runbooks, not in global prompt rules or hardcoded target parsers.
