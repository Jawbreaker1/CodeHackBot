---
name: api-assessment
description: Use when an authorized HTTP or RPC API needs endpoint, object-authorization, schema, or state-transition assessment beyond browser-visible behavior.
---

# API assessment

1. Establish the API base, permitted accounts or roles, documentation or schema provenance, and representative objects. Map endpoints from observed traffic and available contracts, not imagined routes.
2. Prioritize trust boundaries: object ownership, tenant separation, role enforcement, input validation, and workflow transitions. Compare normal and altered requests with controlled test data; account for caches and asynchronous effects.
3. Choose bounded tests that show server-side decisions. A client UI restriction or error text alone cannot prove authorization. Record request method, path, role, relevant headers/body, response, and resulting state without exposing secrets.
4. Keep role sessions and mutable fixtures isolated across workers. Sequence tests that modify the same object. Verify a promising result from a second role or independent observation when possible.
5. Report affected operation, prerequisites, observed impact, and coverage. Distinguish unavailable endpoints from tested negative cases.
