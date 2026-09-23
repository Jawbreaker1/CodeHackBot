---
name: evidence-reporting
description: Use when worker results must become reproducible, prioritized findings or a formal assessment report.
---

# Evidence synthesis and reporting

This guide informs evidence synthesis. The assessment runtime writes the canonical Markdown report from recorded plans, findings, and worker results. Do not create a parallel report with `bash` unless the operator has requested an additional deliverable.

1. Reconcile each worker's assigned goal, actual method, outcome, and evidence references. A completed tool call is not a proven finding or a completed assessment goal. Preserve the test window, scope, approved/skipped work, and limitations actually recorded.
2. Group duplicate observations by root cause and affected boundary. Keep **reproduced**, **candidate**, negative tests with their precise coverage, and unresolved gaps distinct. A CVE or scanner match without target validation remains a candidate.
3. Give each finding an affected target and prerequisites, clear reproduction steps, observed result, business or technical impact, exact evidence references, confidence, priority, and actionable remediation. Record a validation task for a reproduced claim. Minimize secrets and customer data; verify that cited local artifacts exist and support the claim.
4. Write for two audiences: a short executive summary stating the objective, what was actually established, priority risks, and recommended next actions; then a technical section with scope, method, coverage, individual findings, limitations, and a reproducible evidence trail. Do not turn an untested area or failed tool into a clean bill of health.
5. The canonical `report.md` remains the complete local evidence trail. After the assessment ends, the operator may ask the coordinator for an OWASP WSTG-aligned or PTES-aligned Markdown version. The coordinator selects the named format; the runtime renders a new file under the session's `reports/` directory from recorded findings, task results, and gaps and links it in chat. The templates live in `internal/assessment/templates/`. Never describe a format as certification or imply that every standard test was performed.
6. For web work, use versioned OWASP WSTG test references only when the work actually performed supports them. If using CVSS, supply the correct version, vector, and rationale from measured facts; otherwise retain qualitative severity and confidence without inventing a score. PDF or DOCX packaging requires an explicit export workflow and review of the rendered result.

References: [OWASP WSTG v4.2 reporting](https://wstg.owasp.org/v4.2/5-Reporting/), [PTES reporting](https://www.pentest-standard.org/index.php/Reporting), [FIRST CVSS v4.0 specification](https://www.first.org/cvss/v4.0/specification-document).
