---
name: web-application
description: Use when a browser-facing application needs UI behavior, HTTP traffic, and available source or configuration assessed together.
---

# Web application investigation

Revision: 2026-09-23. Applies only to declared web targets and permitted user roles.

1. Map the app's actual entry points and roles through bounded browser and HTTP observations. Preserve URLs, request/response evidence, account role, and relevant state. Use the preprovisioned Playwright helper where browser behavior matters; verify its installed version and read its local README first.
2. Compare client-visible behavior with exposed HTML/JavaScript, API responses, server configuration, and attributable source when available. Form hypotheses about trust boundaries, authorization, input handling, session state, and deployment differences.
3. Give independent workers distinct surfaces or roles. Keep browser profiles, credentials, and captures isolated; coordinate before testing state-changing flows. A browser observation, source suspicion, and scanner output are complementary evidence, not interchangeable proof.
4. Choose the least disruptive check that can validate each hypothesis. Capture reproducible request/response pairs and screenshots or traces when they clarify a finding. Do not treat a visual state alone as proof of server-side authorization.
5. Consolidate confirmed behavior, impact, prerequisites, and remediation into the assessment evidence register. Preserve uncertain leads as gaps rather than findings.

The general investigation guide remains applicable. This guide supplies a lens, not a fixed checklist or permission to traverse beyond scope.
