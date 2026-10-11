---
name: web-application
description: Use when a browser-facing application needs UI behavior, HTTP traffic, and available source or configuration assessed together.
---

# Web application investigation

Revision: 2026-10-10. Applies only to declared web targets and permitted user roles.

1. Map the app's actual entry points and roles through bounded browser and HTTP observations. Preserve URLs, request/response evidence, account role, and relevant state. When sampling a larger link inventory, prefer functional surfaces that handle accounts, user data, controls, or consequential workflows over language, legal, or marketing pages. A read-only GET of a directly linked in-scope page is still a read-only observation if it returns a login page or redirect; stop there rather than attempting login or following an unreviewed destination. When scope names an exact origin, disable automatic redirects and review each destination before following it. Use the preprovisioned Playwright helper where browser behavior matters; verify its installed version and read its local README first.
2. Compare client-visible behavior with exposed HTML/JavaScript, API responses, server configuration, and attributable source when available. Follow the most material data or control flows across these surfaces and their trust boundaries; check browser startup order when it can change the result. Distinguish separate sensitive values within one workflow by source and lifetime, so a checked storage behavior for one does not hide an untested path for another. Treat lexical extraction from bundled or minified code as navigation only: verify a method, value source, storage operation, or network destination at its actual call site rather than inferring it from nearby text. Form hypotheses about authorization, input handling, session state, external effects, and deployment differences. Distinguish a path suggested by code from behavior actually observed on the target.
3. Give independent workers distinct surfaces or roles. Keep browser profiles, credentials, and captures isolated; coordinate before testing state-changing flows. A browser observation, source suspicion, and scanner output are complementary evidence, not interchangeable proof.
4. Choose the least disruptive check that can validate each hypothesis. Capture reproducible request/response pairs and screenshots or traces when they clarify a finding. Do not treat a visual state alone as proof of server-side authorization.
5. Consolidate confirmed behavior, impact, prerequisites, and remediation into the assessment evidence register. Preserve uncertain leads as gaps rather than findings.
6. When a broader coverage review is useful, compare the observed features and trust boundaries with applicable [OWASP Top 10:2025](https://top10.owasp.org/) risk areas. Use the [Web Security Testing Guide](https://owasp.org/projects/web-security-testing-guide/) for concrete web checks, and [ASVS](https://owasp.org/projects/asvs) or API guidance when those fit the observed application. Record what was actually tested, what remains unknown or out of scope, and which gap merits a bounded next test. Top 10 is an optional risk lens, not ten attacks to execute or a claim of complete coverage.

The general investigation guide remains applicable. This guide supplies a lens, not a fixed checklist or permission to traverse beyond scope.
