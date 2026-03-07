# LLM Freedom Guardrail Audit (Sprint 36 / Phase 8)

Date: 2026-03-03  
Scope: non-LLM command adaptation/repair/fallback paths in CLI + orchestrator runtime.

## Inventory and classification

| Path | Entry points | Owner | Policy rationale | Classification |
| --- | --- | --- | --- | --- |
| `internal/cli/assist_command_contract.go` | `normalizeAssistCommandContract` | CLI executor contract | Enforce executable command shape; reject prose-as-command and unsafe shell-operator shapes. | `generic_capability` |
| `internal/cli/cmd_exec.go` | `handleRun`, `handleMSF` | CLI runtime | Runtime shell/MSF execution path with interruption + summary behavior. | `generic_capability` |
| `internal/cli/tool_forge.go` | `executeToolRun` | CLI tool runtime | Execute generated helper tools through same bounded runtime path. | `generic_capability` |
| `internal/cli/assist_recovery.go` | `handleAssistCommandFailure`, `tryImmediateAssistRepair`, `suggestAssistRecovery` | CLI recovery policy | Bounded recovery and LLM repair orchestration after failed actions. | `generic_capability` |
| `internal/orchestrator/worker_runtime_assist_loop.go` | assistant `type=command` execution branch | Orchestrator assist runtime | Applies shared runtime mutation pipeline before scope validation; emits stage telemetry and bounded loop guards. | `generic_capability` |
| `internal/orchestrator/worker_runtime_assist_exec.go` | `executeWorkerAssistCommandWithOptions`, `executeWorkerAssistTool` | Orchestrator assist runtime | Executes prevalidated commands without hidden second-pass mutation (`skipRuntimeMutation`), while retaining generic runtime guardrails. | `generic_capability` |
| `internal/orchestrator/runtime_command_prepare.go` | `prepareRuntimeCommand` | Orchestrator executor | Single preparation funnel: target fallback + runtime adaptation. | `generic_capability` |
| `internal/orchestrator/runtime_command_pipeline.go` | `applyRuntimeCommandPipeline` | Orchestrator executor | Unified ordered pre-exec mutation pipeline with stage telemetry (`target_attribution`, prepare, weak-action adapters, runtime command adaptation, input repair, archive adaptation). | `generic_capability` |
| `internal/orchestrator/runtime_command_utils.go` | `applyCommandTargetFallback` | Orchestrator scope/runtime policy | Inject in-scope target only when network-sensitive command is missing target args. | `generic_capability` |
| `internal/orchestrator/command_adaptation.go` | `adaptCommandForRuntime` | Orchestrator command adaptation | Bounded nmap/runtime guardrails (timeouts/retries/rates/script sanitation). | `generic_capability` |
| `internal/orchestrator/runtime_input_repair.go` | `repairMissingCommandInputPaths` (+ shell wrapper variants) | Orchestrator input repair | Repair missing local/dependency artifact paths for command continuity. | `generic_capability` |
| `internal/orchestrator/runtime_archive_workflow.go` | `adaptArchiveWorkflowCommand` (+ archive helpers) | Orchestrator hard-support policy | Capability-level local archive workflow adaptation under hard-support policy gate. | `generic_capability` |
| `internal/orchestrator/runtime_vulnerability_evidence.go` | `adaptWeakVulnerabilityAction`, `ensureVulnerabilityEvidenceAction*` | Orchestrator evidence policy | Require concrete vuln-evidence command forms for vulnerability-reporting goals. | `generic_capability` |
| `internal/orchestrator/worker_runtime_command_repair.go` | `attemptCommandContractRepair` | Orchestrator repair loop | Heuristic + assistant-assisted repair for command contract failures. | `generic_capability` |

## Result

- No production adaptation path above should depend on scenario literals (specific host/device/file names).
- Scenario/device/domain examples should remain in docs/tests only.
- Runtime adaptation policy remains capability-level and gated by scope/safety/contracts.

## Regression guard

- Enforced by `internal/orchestrator/hardcoding_guard_test.go`.
- The test fails when adaptation/repair runtime paths contain scenario literals such as:
  - `secret.zip`
  - `192.168.50.1`
  - `Johans-iphone`
  - `systemverification.com`
  - `scanme.nmap.org`
