# Repository Guidelines

## Project Structure & Module Organization
- The repository is currently empty; establish a predictable layout and keep this guide updated.
- Suggested paths: `src/` (production code), `tests/` or `__tests__/` (automated tests), `scripts/` (dev tools), `docs/` (design/architecture), `assets/` (fixtures, samples).

## Build, Test, and Development Commands
- No tooling is configured yet. When added, document a minimal command set.
- Example targets to standardize on:
  - `npm run dev` or `make dev` for local development
  - `npm test` or `make test` for automated tests
  - `npm run build` or `make build` for production builds

## Coding Style & Naming Conventions
- Adopt the canonical formatter/linter for the chosen language (e.g., `prettier`, `black`, `gofmt`) and check in config.
- Naming: directories in `kebab-case`, files in `kebab-case` or `snake_case`, types/classes in `PascalCase`.
- CLI implementation language: Go (for a single static binary and straightforward SSH/concurrency).

## Testing Guidelines
- Choose one testing framework and keep it consistent.
- Recommended conventions: `*.test.*` or `*.spec.*` filenames; tests under `tests/` or colocated with source.
- Aim for high coverage on core logic (planning, scope enforcement, report generation, replay) and add regression tests for known findings.

## Commit & Pull Request Guidelines
- Commit style is not established; default to Conventional Commits (e.g., `feat: add scan orchestration`).
- PRs should include a short description, linked issue if any, and screenshots for UI changes.

## Reporting Standards
- Reporting rules and evidence expectations are defined in `AGENTS.md` and should be treated as required for any automated output.

## Guidance Documents
- `docs/tooling/` provides short, curated guidance for available tools (e.g., Metasploit).
- `docs/playbooks/` contains scenario-based playbooks (network scan, web enumeration, credential audit, zip access) for safe, repeatable workflows.

## Execution Environment & Access
- Initial access modes: local terminal execution and/or SSH into the Kali VM.
- Future option: add an API-driven control plane to support orchestration of multiple agents.

## Exploit Knowledge Sources
- The agent should be able to query local exploit metadata sources (e.g., Metasploit module database) to discover relevant modules dynamically.
- Use read-only access for discovery; execution still follows the session permission level and safety rules in `AGENTS.md`.
- Integration path: MVP uses scripted `msfconsole` queries and output parsing; plan to migrate to `msfrpcd` (RPC) for structured access in later iterations.

## Planning Phase (MVP)
- Each session starts with a planning phase that outlines recon → enumeration → validation → escalation → reporting.
- The plan must be recorded in session artifacts for traceability and replay (e.g., `sessions/<id>/plan.md`).

## Execution Model
- MVP uses a single agent with two internal roles: planner (decides next steps) and executor (runs commands with logging and guardrails).
- Script generation should be template-first; ad-hoc scripts must be saved under session artifacts with command + output captured.

## Roadmap Notes
- Orchestrator layer for multi-agent coordination is a possible later task; not required for the first usable version.

## Prompting & Behavior
- Maintain a dedicated behavior/system prompt so the LLM’s purpose, safety rules, and workflow are explicit.
- Suggested location: `prompts/system.md` (agent reads this at startup).

## Language & Tooling Policy
- Default to Bash for orchestration and Python for logic, parsing, and report generation.
- Prefer existing tools; only generate new scripts when needed, and capture them as artifacts for audit.
- `/init` should optionally run a “computer inventory” to detect available tooling and versions in the target VM.
- The agent must handle both offline and online environments; never assume internet access.
- Inventory output should be saved per session (e.g., `sessions/<id>/inventory.md`).
- Minimum inventory: OS/kernel, Python version, shell, network tools (e.g., `nmap`), Metasploit (`msfconsole`), privilege status, and available disk space.

## CLI Interaction Notes
- The CLI should support slash commands similar to Codex CLI. Example: `/permissions` to set the execution approval level.
- `/init` should generate baseline scaffolding documents (e.g., `AGENTS.md`, `PROJECT.md`, basic config placeholders).
- Permission levels should mirror Codex CLI: `readonly`, `default` (approval required for executions), and `all` (fully allowed).
- `/run <command> [args...]` should execute via the guarded runner and log output under the session `logs/` directory.
- Display a concise ANSI-colored summary after major actions (e.g., `/run`, `/init`), and expose `/status` to show the current task.
- Support ESC to interrupt a running major action and return to the prompt for next steps.
- `/msf [service=...] [platform=...] [keyword=...]` should run a non-interactive Metasploit search (msfconsole `-q -x`) and parse results.
- `/report [path]` should generate an OWASP-style report from the template, defaulting to `sessions/<id>/report.md`.

## Context Management (File-First)
- Use a file-first memory model: store summaries and artifacts on disk; keep only a small working set in live context.
- Strategy: after each phase, compact raw tool output into a short summary file and archive the full logs under `sessions/`.
- Keep a living “known facts” summary per session to avoid repeating data in prompts.
- Avoid vector DBs by default; consider embeddings only if recall becomes an issue.
- Default thresholds (configurable): keep the last 20 tool outputs in live context; summarize every 5–10 steps or when the context is ~70% full; always persist raw logs to disk for later evidence retrieval.
- These limits should be user-manageable via config and a CLI slash command (e.g., `/context`).
- Inspiration: Cline’s “Memory Bank” uses file-based summaries; use it as a reference for workflow design, adapted for security-testing needs (evidence retention, auditability).
- The “memory bank” for this project is session-scoped and lives under `sessions/<id>/` (summaries, known facts, logs), not a global repo-level store.

## Configuration Layout
- Keep configuration simple for now to support parallel agents without collisions.
- Suggested hierarchy:
  - `config/default.json` for global defaults
  - `config/profiles/<name>.json` for reusable profiles
  - `sessions/<id>/config.json` for per-run snapshots
- Override order: `default.json` → profile → session config → CLI flags.

## Evidence Ledger
- Support an optional evidence ledger per session, enabled via a CLI slash toggle (e.g., `/ledger on|off`).
- When enabled, generate a unique session ledger file in Markdown (e.g., `sessions/<id>/ledger.md`) that links findings to exact commands and raw log paths.

## Session Replay
- Support a replay mode that runs as its own session to re-run key steps from a saved plan/ledger for verification.
- Replay should be opt-in and respect current scope, permissions, and safety rules.
- CLI example: `BirdHackBot --replay <session-id>` to start a replay session (lookup under `sessions/`).

## Session Resume
- Support resuming an interrupted session by loading its saved context artifacts.
- CLI example: `BirdHackBot --resume <session-id>` to continue a prior session from `sessions/`.
- Provide `/resume` as a CLI slash command to list existing sessions and select one to continue.

## Session Artifacts
- Each session directory should include `plan.md`, `inventory.md`, optional `ledger.md`, `logs/`, `artifacts/`, and `session.json` (status/metadata).

## Future Improvement: Target Baselines
- Consider a target-scoped baseline store (e.g., `targets/<id>/`) to reuse recon facts across sessions without re-discovery.
- Include staleness checks before reuse to avoid relying on outdated recon data.
