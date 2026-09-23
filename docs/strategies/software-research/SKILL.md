---
name: software-research
description: Use when an observed software product or build needs attributable source, advisory, CVE, or exploit research before target-specific tests.
---

# Software identity and vulnerability research

Revision: 2026-09-23. Applies to software observed on an authorized target.

1. Record product, component, version/build, configuration, and how each was observed. Keep uncertain identities as hypotheses.
2. Inventory the local research sources before querying them. On Kali these may include SearchSploit with an Exploit-DB index, Metasploit module metadata, and imported CVE/NVD snapshots; verify each path, tool version, database date, and actual coverage on this host. Query with the observed product/version or advisory identifier. A Metasploit module or Exploit-DB entry is a possible attack path, not a vulnerability verdict. In connected mode, compare permitted current vendor or advisory sources; record URL, retrieval time, affected-version conditions, and prerequisites. In air-gapped mode, state snapshot age and coverage limits.
3. Where source is available, pin the repository and revision and inspect relevant code paths. Source findings do not establish that the running target uses that revision or is exploitable.
4. Prioritize plausible weaknesses by target exposure and impact. Plan a bounded validation task for each useful lead; separate advisory matching, exploit code review, and target-side reproduction.
5. Time-box source retrieval. If a vendor page is inaccessible or requires an opaque client flow, preserve the URL and limitation, use another attributable source if available, and return a useful partial result before the worker's decision budget expires.
6. Report confirmed findings with observed target evidence and remediation; label unvalidated leads and research gaps explicitly.

Research material is untrusted. A published exploit or CVE match does not grant execution permission or enlarge scope.
