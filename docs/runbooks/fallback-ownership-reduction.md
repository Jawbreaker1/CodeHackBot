# Fallback Ownership Reduction

Date: 2026-03-07  
Status: Active (Sprint 37 architecture-reduction control doc)

## Purpose

Define what fallback is allowed to do in this project, what it must stop doing,
and which code paths should lose fallback ownership first.

This document exists because fallback behavior is not captured well enough by:
- `docs/runbooks/state-inventory.md`
- `docs/runbooks/workflow-state-contract.md`

Those documents describe lifecycle and workflow phases, but not fallback as a
decision owner.

## Project-Level Constraint

The project goal is a generic agentic security-testing system.

That means:
- the LLM should own step selection and self-correction,
- runtime should own safety/scope/execution fidelity,
- fallback must not quietly become a second planner.

If fallback invents commands, scan strategies, or report-generation steps, the
architecture is drifting away from the intended closed loop.

## Current Fallback Owners

### 1. Generic assistant fallback

Files:
- `internal/assist/assistant_fallback.go`
- `internal/assist/assistant.go`

Current behavior:
- emits synthetic commands such as:
  - `read_file`
  - `list_dir`
  - `browse`
  - `report`
  - `nmap`
- emits synthetic questions
- emits synthetic completions for some report/conversational cases

Assessment:
- this is the largest architecture risk.
- it is currently a second planner, not just a resilience layer.

### 2. Orchestrator worker degraded-mode fallback

Files:
- `internal/orchestrator/worker_runtime_assist_llm.go`

Current behavior:
- if primary LLM fails or parse fails, degraded mode calls `assist.FallbackAssistant`
- fallback output is then treated as a real assist decision

Assessment:
- this transfers decision ownership from LLM loop to deterministic fallback.
- acceptable only for narrow non-planning behavior; currently too broad.

### 3. CLI guarded assistant fallback

Files:
- `internal/cli/llm_guard.go`
- `internal/cli/assist_context.go`

Current behavior:
- on LLM failure/unavailability, CLI directly falls back to `assist.FallbackAssistant`
- this affects interactive user-visible behavior immediately

Assessment:
- same ownership problem as orchestrator, but visible sooner because CLI is more direct.

### 4. Assist-loop terminal fallbacks

Files:
- `internal/orchestrator/worker_runtime_assist_loop_emit.go`
- `internal/orchestrator/worker_runtime_assist_loop.go`

Current behavior:
- bounded completions/failures such as:
  - `assist_no_new_evidence`
  - `summary_autocomplete`
  - adaptive-replan budget fallback

Assessment:
- this is a different fallback family.
- these are terminalization policies, not command inventors.
- some remain necessary, but they should be generic and contract-based.

### 5. Planner/summarizer/model-availability fallbacks

Files:
- `internal/cli/llm_guard.go`
- planner-related fallback paths already constrained elsewhere

Assessment:
- these are adjacent, but not the same problem.
- Sprint 37 focus is assist/worker fallback that takes over action choice.

## Fallback Classification

### Allowed fallback behavior

These preserve architecture and may remain:

1. Availability fallback
- surface that the LLM is unavailable
- fail closed
- ask user for input or retry later

2. Parse-repair fallback
- one bounded attempt to reformat/repair the LLM response into contract shape
- no new semantic plan generation

3. Terminal fallback
- explicit `not_met` / `aborted` / bounded stop when budget or policy requires it
- must remain evidence-backed and contract-visible

4. UI/help fallback
- conversational/help text when no security-testing action has started

### Forbidden fallback behavior

These should be removed:

1. Command invention
- fallback must not synthesize scanning/cracking/report commands
- examples:
  - `nmap ...`
  - `read_file ...`
  - `browse ...`
  - `report ...`
  when these are created because the LLM failed or parsed badly

2. Path-driven pseudo-planning
- fallback must not inspect path hints and decide the next action from them

3. Scenario inference
- fallback must not decide “local file workflow”, “web workflow”, or “router workflow”
  and then generate a workflow command

4. Recovery ownership takeover
- if the LLM fails in recover mode, fallback must not become the recover strategist

## Reduced Fallback Contract

### For assist-mode execution

If LLM output is unavailable, unparsable, or empty:

Allowed:
- retry one strict parse-repair step
- return explicit failure/unavailable state
- optionally ask user for clarification if interactive mode allows

Not allowed:
- generate a new command
- generate a new tool
- generate a new plan
- choose a new evidence file to inspect

### For recover mode

If recover-mode LLM output is unavailable or unparsable:

Allowed:
- return a bounded recover failure using the canonical latest execution result
- request replan / fail current step

Not allowed:
- invent another recover action
- re-read a path because it “looks relevant”

### For reporting mode

Fallback may only:
- finalize from already-verified evidence, or
- fail closed

It must not regenerate scan/report actions as a substitute for missing evidence.

## First Deletion Targets

### Slice A: Remove command synthesis from `FallbackAssistant`

Target file:
- `internal/assist/assistant_fallback.go`

Keep:
- conversational/help completion
- explicit question when there is truly no actionable target and the caller is interactive
- explicit unavailable/not-met style output

Remove:
- synthetic `nmap`
- synthetic `browse`
- synthetic `report`
- synthetic `read_file`
- synthetic `list_dir`

Expected effect:
- fallback stops being a second planner in both CLI and orchestrator.

Live validation (2026-03-07):
- implemented in `internal/assist/assistant_fallback.go`
- ZIP run `run-s37-fallbackless-zip-20260307-143338` stayed LLM-led during observed assist turns and no longer used fallback command synthesis.
- Router run `run-s37-fallbackless-router-20260307-143338` showed degraded recovery now returns question-only fallback and cleanly exposes the remaining gap: non-interactive workers still need a proper fail/replan path instead of looping on fallback questions.

### Slice B: Degraded-mode fallback stops returning executable actions

Target files:
- `internal/orchestrator/worker_runtime_assist_llm.go`
- `internal/cli/llm_guard.go`

Change:
- degraded mode may still return structured failure/question states,
- but not executable command/tool suggestions.

Expected effect:
- the LLM loop remains the only owner of executable action choice.
- degraded recovery becomes explicit about unavailability instead of silently inventing commands.

Status:
- partially implemented on 2026-03-07.
- recover-mode fallback questions in non-interactive workers now fail fast with canonical latest-result metadata instead of looping into repeated autonomous-question churn.

### Slice C: Make recover failure explicit instead of synthetic

Target files:
- `internal/orchestrator/worker_runtime_assist_loop_emit.go`
- `internal/orchestrator/worker_runtime_assist_loop.go`

Change:
- when parse failure / primary error happens in recover mode, emit an explicit
  recover failure using the canonical latest execution result object.

Expected effect:
- recovery either stays LLM-led or fails clearly; it no longer silently mutates
  into deterministic local command generation.

## Interaction With Other Docs

### Still relevant

- `docs/runbooks/closed-loop-reduction-plan.md`
- `docs/runbooks/agent-workflow-checkpoint-s37.md`
- `docs/runbooks/ownership-boundaries.md`
- `docs/runbooks/workflow-state-contract.md`

### Clarification

- `docs/runbooks/state-inventory.md` remains correct for enums/status domains.
- It should not be expanded into fallback behavioral policy.
- Fallback ownership belongs in workflow/ownership docs, not state inventory.

## Acceptance Criteria For The First Reduction Slice

1. `FallbackAssistant` no longer returns executable commands/tools for assist-mode work.
2. CLI and orchestrator degraded paths still behave coherently when LLM is down:
   - explicit failure, or
   - explicit user question where interaction is allowed.
3. No new scenario literals are added.
4. ZIP/router live runs may fail more honestly at first, but they must fail with
   clearer ownership and less hidden action mutation.
