# Local strategy catalog

Revision: 2026-09-23. Provenance: BirdHackBot project guidance, maintained in this repository. This compact index is shared by the coordinator and workers. The coordinator may suggest up to two paths per task; the worker selects a useful guide with `load_strategy` and may change course as evidence develops. A guide's full text is loaded only for that worker and retained with its source and checksum. Guides are strategic decision aids, not runbooks, permission, or evidence. Match the observed problem and desired outcome, not just a keyword; do not load every guide.

| Guide | Use when |
| --- | --- |
| `investigation/SKILL.md` | An ambiguous or multi-step goal needs hypotheses, bounded tests, evidence-based replanning, and a clear stop condition. |
| `network-discovery/SKILL.md` | Authorized network targets or the default gateway must be identified and scoped before deeper service work. |
| `service-assessment/SKILL.md` | An observed listener or protocol needs fingerprinting, exposure analysis, configuration checks, and test prioritization. |
| `software-research/SKILL.md` | An identified product or build needs attributable source, advisories, CVEs, exploit references, and applicability research. |
| `source-review/SKILL.md` | Available source needs trust-boundary tracing and hypotheses tied back to the deployed target. |
| `web-application/SKILL.md` | A browser-facing application needs UI journeys, HTTP evidence, and available source/configuration compared. |
| `api-assessment/SKILL.md` | A scoped HTTP or RPC API needs endpoint, object-authorization, schema, and state-transition assessment. |
| `identity-access/SKILL.md` | Accounts, roles, sessions, tokens, or permission boundaries need controlled authentication and authorization testing. |
| `configuration-exposure/SKILL.md` | Deployment, secrets handling, storage, or host configuration may create unintended access or data exposure. |
| `credential-recovery/SKILL.md` | An explicitly authorized encrypted artifact or account needs staged candidate recovery and independent verification. |
| `vulnerability-validation/SKILL.md` | A source finding, scanner result, advisory, or exploit lead needs bounded target-side reproduction before a claim. |
| `evidence-reporting/SKILL.md` | Several test results must be reconciled into reproducible, prioritized findings and a clear report. |

Choose the most specific fitting guide; add `investigation` only when its planning method will actually help. If none fits, continue from the observed evidence and record the knowledge gap. Guide examples require normal tool verification and session approval before execution.
