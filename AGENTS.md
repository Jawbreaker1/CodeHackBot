# Agent Directives

## Authorization & Scope
- Authorized security testing only. Current scope is limited to Johan Engwall’s closed lab systems on internal networks.
- No customer or third-party testing until written authorization and RoE exist (owner/approver, storage location, in-scope/out-of-scope targets, allowed/prohibited actions, testing windows, escalation contacts).
- Evidence collection is allowed only inside the lab and must use minimal, non-sensitive data; no data may leave the lab network.

## Allowed & Prohibited Actions
- Allowed: recon, scanning, controlled exploitation, and privilege escalation within scope to validate findings.
- Prohibited by default: DoS, persistence, and real data exfiltration. Any exception requires explicit opt-in and documented approval.

## Session Configuration & Safety
- Every session must define target boundaries and enforce sandbox limits (internal networks only).
- Human oversight is required for risky steps (exploitation, escalation).
- Kill-switch policy: CLI must honor Ctrl-C (SIGINT) and SIGTERM, terminate child processes safely, and emit an “aborted” report. Orchestrators must be able to broadcast a stop signal to all agents.
- Executor permissions: default to approve-every-tool-use; allow explicit session-level overrides (e.g., full access) only inside the VM sandbox and with clear user acknowledgment.

## Reporting Quality
- Produce professional-grade, reproducible findings and evidence suitable for peer review by experienced pen testers.
- Reports must follow OWASP-style formatting at minimum and include scope, steps to reproduce, impact, evidence, and remediation guidance.

## Project Docs
- Project structure, build/test commands, and coding conventions live in `PROJECT.md`.

## Third-Party Inspiration
- Cline may be used for conceptual inspiration only. Do not copy any code or documentation verbatim.
- Any third-party code or assets must be properly licensed and attributed before inclusion.

## Maintainability
- Flag growing files early and recommend refactors when a file becomes large or hard to navigate.
