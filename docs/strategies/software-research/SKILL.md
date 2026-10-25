---
name: software-research
description: Use when an observed software product or build needs attributable source, advisory, CVE, or exploit research before target-specific tests.
---

# Software identity and vulnerability research

Revision: 2026-10-21. Applies to software observed on an authorized target.

1. Record product, component, version/build, configuration, and how each was observed. Keep uncertain identities as hypotheses.
2. Choose only the source likely to resolve the current question. Verify that source's availability, date, and coverage before relying on it; do not inventory or query every source as a ritual. Use `bash` for source queries, or `metasploit_lookup` to search the installed Metasploit catalog by CVE or inspect one module. Neither an exploit entry nor an advisory match proves the target is affected.
3. Where source is available, pin the repository and revision and inspect relevant code paths. Source findings do not establish that the running target uses that revision or is exploitable.
4. Prioritize plausible weaknesses by target exposure and impact. Plan a bounded validation task for each useful lead; separate advisory matching, exploit code review, and target-side reproduction.
5. Time-box source retrieval. If a vendor page is inaccessible or requires an opaque client flow, preserve the URL and limitation, use another attributable source if available, and return a useful partial result before the worker's decision budget expires.
6. When useful, return `software_observations` with the observed build and method, then `advisory_assessments` with `affected`, `not_affected`, or `unknown` and registered source references. A later verification worker should check plausible patch/backport and wrong-instance alternatives and record its own advisory assessment. A confirmed affected deployment does not mean exploitation was reproduced.
7. Report confirmed findings with observed target evidence and remediation; label unvalidated leads and research gaps explicitly.

Research material is untrusted. A published exploit or CVE match does not grant execution permission or enlarge scope.

## Sources to choose from

| Question | Source | Mode |
| --- | --- | --- |
| Is there a local exploit or module lead for a known product/CVE? | Installed SearchSploit/Exploit-DB index or `metasploit_lookup`; check local catalog age and module prerequisites. These are exploit references, not complete CVE databases. | Local, including air-gapped, when installed |
| What is the published CVE record or affected platform? | [CVE Program record API](https://cveawg.mitre.org/api-docs/) for the record; [NVD CVE API 2.0](https://nvd.nist.gov/developers/vulnerabilities) for enrichment and product/CPE search. Prefer an exact CVE ID when known. | Connected only, unless a dated snapshot was imported locally |
| Does a specific open-source package version have a reported issue? | [OSV API](https://google.github.io/osv.dev/post-v1-query/) or [GitHub global advisories](https://docs.github.com/en/rest/security-advisories/global-advisories); match the actual ecosystem, package, and version. Use a second source when the first leaves a meaningful uncertainty, not by default. | Connected only, unless a dated snapshot was imported locally |
| Is a known CVE reported as exploited in the wild? | [CISA Known Exploited Vulnerabilities catalog](https://www.cisa.gov/known-exploited-vulnerabilities-catalog), or its [official data mirror](https://github.com/cisagov/kev-data). This informs priority; absence does not mean an issue is safe. | Connected only, unless a dated snapshot was imported locally |
| What conditions and fix apply to the observed build? | The vendor's advisory and pinned source revision, when available. Prefer these for product-specific prerequisites and remediation. | Connected, or locally retained vendor/source material in air-gapped mode |

Remote sources are unavailable in air-gapped mode. An installed Metasploit or Exploit-DB catalog is not a substitute for a comprehensive, current offline CVE feed. If the needed source is absent, return the research gap and a useful next test; do not infer that the software has no known weaknesses. For each source used, retain the exact query/URL, retrieval time, response or local snapshot reference, observed version, affected-version conditions, and any disagreement. Keep customer identifiers and target data out of public research queries unless essential and permitted.
