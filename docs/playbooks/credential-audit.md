# Playbook: Credential Audit (Lab)

## Goal
Validate whether authentication controls resist weak/default credential abuse inside authorized scope.

## Required Input
- Explicit approval for credential testing.
- In-scope login surfaces (web, SSH, RDP, API, VPN, etc.).
- Lockout/rate-limit policy and safe attempt budget.

## Guardrails
- Never test outside approved targets.
- Keep attempts low-rate and lockout-aware.
- Do not store plaintext secrets in logs or reports.

## Planning Checklist
1. Enumerate authentication entry points and expected account types.
2. Set attempt limits per target/account.
3. Define stop condition for lockout or account impact.

## Procedure
1. Surface mapping.
   - Identify login forms, API auth endpoints, admin consoles, remote access services.
2. Default credential validation.
   - Test vendor-default pairs only where product fingerprint is confident.
3. Weak password policy checks.
   - Validate minimum length/complexity, common password blocking, password reuse controls.
4. MFA and account protection checks.
   - Verify MFA coverage, lockout behavior, and recovery flow weaknesses.
5. Controlled low-rate attempts (if approved).
   - Prefer targeted checks over broad spraying.

## Decision Gates
- If lockout or user impact risk appears: stop active attempts and report immediately.
- If valid credentials obtained: prove minimal access, then stop and escalate to reporting.

## Evidence & Reporting
- Record endpoint, method, attempt count, and outcome.
- Redact credentials; store only proof-of-access token or masked identifier.
- Include remediation: disable defaults, enforce MFA, tighten policy/rate limits, monitor auth anomalies.
