# Planner Action Executability

Date: 2026-03-07  
Status: Implemented (Sprint 37)

## Purpose

Prevent structured planner output from collapsing a task into a bare tool label (`john`, `zip2john`, `nmap`, `curl`) when the task contract clearly requires a target, input artifact, or output-binding decision.

The goal is not to hardcode tool sequences. The goal is to reject under-specified direct commands and hand them back to the LLM execution loop in a generic way.

## Problem

Live planner traces showed:
- the planner request payload already contained the real goal, scope, working directory, hypotheses, and relevant playbook guidance,
- but the raw planner JSON still emitted bare command actions for proof-sensitive tasks.

Example failure class:
- task goal: recover ZIP password using local wordlists,
- raw planner action: `command="john"` with no args.

This is not primarily a context-window problem. It is a structured-output action-shape problem.

## Contract

For `action.type=command`, the planner output must be self-executable.

A direct command is considered under-specified when:
- it does not bind to any task-contract anchor even though anchors exist,
- and it gives the worker no concrete input/target/output choice to execute.

Task-contract anchors may come from:
- task targets,
- file/path references in the goal/title,
- expected artifact basenames,
- dependency-driven workflow context.

If a direct command is under-specified:
- do not accept it as a normal command task,
- promote it to `assist`,
- preserve the original tool name only as an optional hint,
- require the worker LLM to expand it into a bounded executable step at runtime.

## Why This Is Generic

This policy does not teach tool-specific command syntax.

It only enforces:
- executable action shape,
- binding to task context,
- LLM-led expansion when planner JSON collapses too far.

That keeps planning loose and agentic while making execution fail-closed.

## Soundness Mapping

This change is consistent with:
- `docs/runbooks/execution-contract-matrix.md`
  - `plan`: no execution starts without a valid plan/task contract.
- `docs/runbooks/workflow-state-contract.md`
  - `execute`: task action must be executable before lease execution.
- `docs/runbooks/acceptance-gates.md`
  - ZIP gate depends on true `answer_success` and `contract_success`, which under-specified tool labels actively undermine.

## Implemented Sprint 37 Slice

1. Persist planner traces for successful runs so request payload and raw structured output are inspectable.
2. Add a generic command executability gate after raw planner parse / task normalization.
3. Promote under-specified command tasks to assist mode with task-contract grounding.
4. Add regressions:
   - bare command with clear anchors -> promoted to assist,
   - concrete command with bound args/targets -> preserved,
   - benign command without anchors -> preserved.
5. Validate with a bounded live ZIP planner smoke.

Validation:
- `go test ./cmd/birdhackbot-orchestrator ./internal/orchestrator`
