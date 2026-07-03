## Objective

Describe the concrete goal of this change.

## Architecture Anchor

Link or name the relevant section of `docs/architecture.md`, `PROJECT.md`, `TASKS.md`, or `AGENTS.md`.

## Why This Is Not A Patch

Explain why this change preserves the generic architecture instead of adding a task-specific workaround.

## Genericity Checklist

- [ ] No scenario-specific guardrails were added.
- [ ] No tool-specific steering was added unless documented as capability context.
- [ ] No fallback logic was added that acts like a second planner.
- [ ] No command-output string parsing, regex heuristics, or brittle path guessing was added without explicit architecture justification.
- [ ] Any new execution facts, signals, or recovery semantics are generic and provenance-backed.

## Validation

- [ ] `./scripts/ci.sh`
- [ ] Live LLM validation, if behavior changed:

Describe live runs or explain why they are not required.
