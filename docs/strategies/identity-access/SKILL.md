---
name: identity-access
description: Use when login, sessions, tokens, accounts, or role boundaries need controlled authentication and authorization testing.
---

# Identity and access boundaries

1. Define authorized identities, roles, accounts, and expected access before testing. Do not infer that one granted account authorizes attempts against others.
2. Trace where identity is established, carried, refreshed, revoked, and checked. Compare decisions across roles and resources using isolated sessions and representative non-sensitive test data.
3. Prefer precise, low-volume checks that distinguish a policy failure from client-only behavior, stale session state, or a test fixture mistake. Coordinate lockout, rate-limit, and state-changing effects before broader attempts.
4. Preserve token handling securely: capture only what is needed to reproduce a finding, redact secrets in routine summaries, and keep raw evidence in restricted task storage.
5. Verify successful access at the intended boundary, report the exact role/resource relationship, and avoid claiming privilege escalation from an unverified response.
