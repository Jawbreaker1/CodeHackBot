# Repository Guidelines

## Purpose

BirdHackBot is being built as System Verification's security testing platform for authorized assessments. Current validation runs in the authorized lab; operating scope remains governed by `AGENTS.md`.

Authorization and scope rules live in `AGENTS.md`.
The rebuild source of truth is `docs/architecture.md`.

## Active Documentation

Current active docs are intentionally minimal:
- `AGENTS.md`
- `PROJECT.md`
- `README.md`
- `TASKS.md`
- `ROADMAP.md`
- `DISCOVERIES.md`
- `docs/roe/public-test-targets.md`
- `docs/runbooks/acceptance-gates.md`
- `docs/runbooks/subscription-bridge.md`
- `docs/runbooks/web-application.md`
- `docs/runbooks/tool-capabilities.md`
- `docs/strategies/catalog.md` and its selectively read worker guides
- `docs/architecture.md`

`TASKS.md` owns status, `docs/architecture.md` owns contracts, and `ROADMAP.md` owns future direction. Update the relevant active docs with behavior changes; archive superseded designs instead of retaining conflicting instructions. Assessments are dated evidence, not implementation status.

Historical material lives in `docs/archive/` and is not authoritative.
The `legacy/` tree is also non-authoritative: it is preserved as historical reference only and reflects the pre-rebuild codebase that became too complex and difficult to reason about.

## Repository Structure

- `legacy/`: pre-rebuild implementation snapshot, including the old Go module
- `cmd/`: rebuild implementation root for new entrypoints
- `internal/`: rebuild implementation root for new packages
- `config/`: rebuild runtime defaults
- `docs/`: active governance docs
- `docs/archive/`: historical reference only
- `sessions/`: local session artifacts and evidence
- `scripts/`: local helper scripts

## Build

- `go build -buildvcs=false ./cmd/birdhackbot`
- `go build -buildvcs=false ./cmd/birdhackbot-orchestrator`
- `go build -buildvcs=false ./cmd/birdhackbot-web`
- `go build -buildvcs=false ./cmd/birdhackbot-llm-bridge`
- The legacy snapshot remains buildable from `legacy/` if needed for reference.

## Test

- `./scripts/ci.sh` runs the deterministic local/GitHub CI checks.
- CI includes `scripts/check_guided_app.py`: the built application runs in a real terminal against a deterministic local model fixture. Python 3 and a Unix PTY are required.
- CI includes `scripts/check_webapp.py`: the built browser binary is started on loopback and exercised through HTTP against the same deterministic model fixture, including two sessions aggregated under one customer report.
- Rebuild-root tests now exist for the implemented core packages.
- Real LLM validation remains required for major behavior slices.
- Repeated live validations can be run with `scripts/repeat_worker_run.sh`.

## Working Rules

- Prefer deletion over patching on this branch.
- Keep changes proportional to demonstrated problems and explicit product requirements. Use the smallest coherent implementation; avoid speculative abstractions, fallback chains, and scenario-specific edge-case handling.
- Give each implementation slice a concrete done condition. Once it works and its required validation passes, stop expanding it to cover hypothetical cases. Essential execution, scope, and evidence guarantees remain part of the core requirements.
- When a solution grows, first simplify it or narrow the slice. Record deferred concerns briefly instead of implementing them preemptively; revisit them when evidence or an agreed requirement justifies the work.
- Do not implement new behavior that conflicts with `docs/architecture.md`.
- Validate major implementation slices with real LLM runs.
- Keep files modular and refactor early when ownership becomes unclear.
- Do not treat `legacy/` or old main-branch behavior as design truth for the rebuild; use `docs/architecture.md`, `TASKS.md`, and active docs instead.

## Locked Invariants

These are stable project truths and must be re-anchored before making non-trivial behavior changes.

- The multi-agent orchestrator is the primary product direction. Complete and validate its shared agentic worker before further orchestrator/capability expansion; core cleanup and subscription access are already implemented (user decisions, 2026-09-19–20).
- Usability is a core product and safety requirement. Normal startup must use one command without mandatory flags, with guided setup and in-app assistance. Keep advanced controls available for automation and diagnosis; the guided flow must use the same runtime and permission rules. The interaction contract is in `docs/architecture.md`.
- One worker loop supplies the shared execution engine; its standalone CLI remains a development, diagnosis, and single-task surface.
- Orchestration owns the assessment, bounded delegation, shared evidence, validation, and final report. Worker reasoning must not be duplicated in the orchestrator.
- Source-assisted investigation connects observed software to attributable source revisions and target-validated findings; source suspicions alone do not establish target exploitability.
- Subscription-backed model access through a local REST bridge is a product requirement. Provider choice must preserve execution ownership, expose limits, and never silently switch to paid API billing.
- Kali tooling, adaptable playbooks, reusable custom tools, and discovery-driven vulnerability research are core capabilities. Their value must be demonstrated in independently verified vulnerability discovery, including missed defects and false claims, against capable competitors.
- Fully air-gapped operation is a required deployment mode: all reasoning, research, tooling, and reporting stay inside the approved local environment, with no cloud fallback. Local-model access alone does not meet this requirement; see the architecture and acceptance gates.
- Direct single-command execution paths are development/debugging support only and must not become the main behavioral truth of the system.
- The runtime should guide and enforce hard boundaries, not replace LLM reasoning with hardcoded workflow logic.
- Do not add scenario-specific guardrails, tool-specific steering, hidden workflow phases, or fallback logic that acts like a second planner.
- Use the LLM for task logic and adaptive behavior to the greatest extent possible; keep runtime-owned logic minimal and generic.
- `constraints` and richer policy/permission systems are intentionally deferred until the core mechanics are sound.
- Every real session must define target boundaries clearly enough for the runtime to enforce scope honestly, but that does not justify adding heavy scope-inference or action-target policy machinery before the core loop is sound.

Live validation is capability-oriented rather than tied to a named target or legacy fixture. Use authorized synthetic or customer-like environments to exercise generic discovery, service and software identification, authentication and authorization, configuration, application behavior, source-assisted analysis, evidence capture, recovery, and reporting. Each run must declare exact scope and a reproducible done condition.

Testing discipline:

- Major behavior slices require live LLM validation, not only local tests.
- User-facing changes must also be exercised through the built application: startup, operator inputs, approvals, progress, results, and stopping as relevant. Maintain repeatable terminal checks in CI and run the real-model user path for major slices; package tests alone do not pass this requirement.
- Repeated live runs are the default for behavioral conclusions because outputs are non-deterministic.
- Use 3 runs per scenario unless the check is explicitly labeled as a smoke test.
- Smoke/debug runs must never be presented as acceptance evidence for platform capability or pentest effectiveness.
- Live validation is not a pass unless the actual context snapshots and persisted session state are inspected and found sound.
- The current local worker benchmark is `qwen/qwen3.8-27b` (Q6_K), confirmed by the user on 2026-09-19. Use the user's approximately 70k-token context configuration (updated 2026-09-26), at most two concurrent inference requests, and reasoning effort **low**. Do not substitute the older Qwen 3.5 baseline or another variant without asking. Further Qwen inference is paused by operator request as of 2026-09-26; keep using Daybreak until they resume local-model testing.
- Qwen may need substantial output capacity even with low reasoning. The guided local profile requests 32,768 output tokens with a ten-minute request limit; do not confuse a provider timeout with operator cancellation. LM Studio previously reported 50,176 tokens and parallelism two; one small 2026-09-26 smoke request observed a loaded 70,144-token window with parallelism two. Stability near the enlarged limit remains unvalidated.
- Confirm the active model and relevant inference settings with the user before changing a live validation setup. Historical Qwen 3.5 results remain labeled with their original model and cannot establish Qwen 3.8 acceptance.

## Implementation Discipline

Before coding a non-trivial implementation slice, state:
- `Objective`
- `Architecture anchor`
- `Why this is not a patch`
- `Validation plan`

If these cannot be stated clearly, stop and reassess before changing code.
