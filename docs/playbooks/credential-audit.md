# Playbook: Credential Audit (Lab)

Goal
- Identify weak or default credentials within the approved scope.

Pre-flight
- Confirm explicit permission for credential testing.
- Ensure rate limits and lockout policies are respected.

Safe Defaults
- Use low-rate checks and avoid account lockouts.
- Do not reuse credentials outside the approved scope.

Steps (Example)
1) Enumerate login surfaces.
2) Check for default credentials (vendor baselines).
3) Validate password policy and MFA presence.

Evidence
- Record tested endpoints and outcomes.
- Do not store real credentials in logs; use redaction.
