# Public Test Targets RoE

## Purpose
This allowlist permits limited testing against external targets that are explicitly published for legal security testing.

## In-Scope Targets
- `scanme.nmap.org`

## Allowed Activity (Strict)
- Passive recon and service discovery.
- Low-impact, rate-limited scanning for exposure validation.
- OWASP-style reporting based only on observed evidence.

## Prohibited Activity
- Exploitation, brute force, credential attacks, or authentication attempts.
- Denial-of-service or high-volume scanning.
- Any action that changes target state or risks service disruption.
- Any testing of domains/hosts not listed above.

## Execution Guardrails
- Default to conservative scan settings (low rate, bounded runtime, minimal scripts).
- Stop immediately on signs of instability or explicit provider warning.
- Keep all evidence local in `sessions/` and redact unnecessary target details in shared summaries.

## Change Control
- Add/remove targets only via repo change with explicit approval from project owner.
