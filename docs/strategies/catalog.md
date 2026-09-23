# Local strategy catalog

Revision: 2026-09-23. Provenance: BirdHackBot project guidance, maintained in this repository. These guides are optional working knowledge, not scope, approval, or evidence. When a guide could improve an unfamiliar approach, prevent a costly mistake, or address a method-limited negative result, select it with the worker's `load_strategy` decision using its relative path below. The selected guide stays in that worker's context with source and checksum across later turns. Adapt it to observed facts and installed tools; do not load every guide.

| Guide | Use when |
| --- | --- |
| `investigation/SKILL.md` | Any ambiguous or multi-step assessment: turn the goal into testable hypotheses, choose bounded methods, and revise from results. |
| `credential-recovery/SKILL.md` | Authorized recovery of access to an encrypted local artifact or account: identify format, stage candidate search, protect recovered secrets, and verify independently. |
| `software-research/SKILL.md` | Identified software needs source, advisory, CVE, or exploit research and target-specific validation. |
| `web-application/SKILL.md` | Scoped web application work needs browser behavior, HTTP/API evidence, and source/configuration review combined. |

If no guide fits, continue with the general investigation method and record the knowledge gap. Guides may cite tools or commands as examples; verify availability and obtain the ordinary session approval before execution.
