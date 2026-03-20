# Discoveries

This file is continuity, not scheduling.

Rules:
- `DISCOVERIES.md` records realizations, risks, and lessons learned.
- It may include notes for future phases.
- It is not a task list.
- It does not override `TASKS.md` or `ROADMAP.md`.

## Current Notes

- Keep the rebuild architecture small and explicit.
- Favor generous, structured active context over aggressive compaction.
- Do not compensate weak context with guardrails or command shaping.
- Keep old implementation isolated under `legacy/`.
- Prefer phase-based planning over large speculative sprint plans.
- The unassisted worker loop is already plausible on local network reconnaissance.
- The ZIP workflow is much more sensitive to execution/tooling details than the router workflow.
- Context inspection exposed a real missing-core bug early: live `behavior_frame` was initially not wired into the packet.
- Separating repo root, session storage, and execution cwd is necessary; tying behavior/config loading to execution cwd was wrong.
- Clean fixtures matter. Workspace contamination can completely distort live validation results.
- Stronger operator-style role text in the user goal changes behavior, but that framing belongs in the behavior frame, not in the task goal.
- The next user-facing priority is a minimal interactive worker CLI shell; one-shot harness runs are no longer enough.
- A minimal interactive worker CLI shell now exists and works with live LM Studio runs, which will make direct operator testing much easier.
- Generic execution-result `assessment` and `signals` are useful. The next core weakness is not just choosing tools, but interpreting ambiguous tool outcomes correctly.
- Generic completion guidance helped the router workflow finish cleanly once enough evidence existed; this was a core loop issue, not a planning issue.
- The real-router run against `192.168.50.1` produced useful evidence, but the first action was too heavy for an interactive loop: `nmap -sV -sC -p- --open` took several minutes.
- The right lesson from the real-router run is generic command-cost judgment, not tool-specific `nmap` rules.
- Current router evidence on `192.168.50.1` includes:
  - `53/tcp` `domain`
  - `80/tcp` `httpd/3.0` with redirect to `/Main_Login.asp`
  - `7788/tcp` `unknown`
  - `18017/tcp` `Asus wanduck WAN monitor httpd`
  - `42065/tcp` `MiniUPnP 2.2.0`
- Single-run behavior is too noisy to trust for agent-quality conclusions. One run is useful as a smoke test; meaningful behavioral evaluation should use at least 2-3 runs per scenario.
- The repeated clean ZIP runs showed both outcomes:
  - one run drifted out of the fixture and searched `~`
  - the next run stayed anchored to `./secret.zip` and behaved plausibly
- That variance means future prompt/behavior changes should be judged on small repeated batches, not single examples.
- A repeat-run harness now exists at `scripts/repeat_worker_run.sh`.
- First 2-run localhost-router batch confirmed the value of repeated runs:
  - run 1 completed cleanly
  - run 2 stopped after failing to complete within the step budget
- Generic shell-syntax handling in the executor was worth adding. Compound commands with `;`, `||`, pipes, and redirection should not be treated as argv actions.
- After the shell-syntax fix, one ZIP spot-check no longer failed on execution mode; the remaining weakness was the next-step choice after confirming the archive is encrypted.
- A minimal structured worker task context now exists:
  - `state`
  - `current_target`
  - `missing_fact`
- In the first spot-check after adding it:
  - router final state reached `done`, target `127.0.0.1`, missing fact `(none)`
  - ZIP final state reached `blocked`, target `secret.zip`, missing fact `next evidence needed about secret.zip`
- Referencing `task_runtime` explicitly in the worker prompt was better than the earlier anchoring-only prompt tweak:
  - router still completed
  - ZIP stayed local and became more tool-directed without broadening into old session artifacts

- Phase 1 is effectively complete: the thin worker loop, exact execution, approval model, context inspection, session resume, and minimal interactive shell are all real and validated.
- The router/local-recon scenario is now good enough for the Phase 1 boundary: it still rereads evidence more than ideal, but it completes cleanly with coherent context and reproducible findings.
- The ZIP scenario remains the best indicator of the raw closed-loop limit. It is still unstable across repeated runs even though the context is now much cleaner and more inspectable than before.
- Recent generic result-truth fixes improved the active packet materially:
  - `missing_path` is now surfaced reliably in bad shell/tool chains
  - `incorrect_password` covers both direct incorrect-password text and `unable to get password` style output
- The next active problem is no longer broad architecture confusion. It is the narrower tension between:
  - stable named target identity in the active task
  - noisy latest command output that can widen the loop into worse local directions
- The right immediate next phase is not heavy memory-bank work. It is active-context quality: truth ordering, target stability, and better visibility into what is actually in the packet.
