# Runtime Mutation Stage Map

Date: 2026-03-06  
Status: Active (Critical 3 reference)

## Purpose

Track exactly where command intent can be changed before execution.
This is the control doc for Sprint 37 Critical 3 (mutation-stack simplification).

## Current Stage Map (Orchestrator Command Actions)

Primary execution path anchors:
- `internal/orchestrator/worker_runtime.go`
- `internal/orchestrator/runtime_command_prepare.go`
- `internal/orchestrator/runtime_input_repair.go`
- `internal/orchestrator/worker_runtime_command_repair.go`
- `internal/orchestrator/runtime_archive_workflow.go`
- `internal/orchestrator/runtime_vulnerability_evidence.go`

Observed mutation stages:
1. action normalization (`normalizeTaskAction`)
2. optional target attribution enforcement (`enforceAttributedCommandTarget`)
3. pre-exec prepare pipeline (`prepareRuntimeCommand`)
4. weak-action adapters (report/vuln/archive support paths)
5. scope validation
6. runtime input repair before assist/builtin command execution
7. command contract repair after failed run (`attemptCommandContractRepair`)
8. retry-specific adaptation (for example nmap host-timeout retry)

## Current Risks

- Same command can be modified by multiple layers with different owners.
- Order is not expressed as one typed stage contract in runtime telemetry.
- Some adapters are capability-level, but stacking still increases intent drift risk.

## Target Contract (Critical 3)

Move toward one explicit ordered mutation pipeline:
1. normalize
2. safety/scope-required adaptation
3. capability support adaptation (bounded and auditable)
4. validation
5. execute
6. post-failure repair (single bounded path)

Rules:
- every mutation stage must be explicit in progress telemetry,
- no silent overlap of equivalent rewrite responsibility,
- no scenario-literal rewrite behavior in production paths.

## Change Control (Maintenance Rule)

Any PR that adds/removes/reorders runtime command adaptation must update:
- this file,
- `docs/runbooks/llm-freedom-guardrail-audit.md`,
- associated regression tests.
