---
name: credential-recovery
description: Use for explicitly authorized recovery of an encrypted artifact or account where candidate strategy, coverage, secret handling, and independent verification matter.
---

# Authorized credential and encrypted-artifact recovery

Revision: 2026-09-23. Use only for an explicitly in-scope local artifact or account. Credential attempts may be disruptive or sensitive; describe the target and effect in the action approval.

1. Identify the exact format and protection scheme. Confirm the converter and cracker support it; preserve the original artifact and hash. Check authorized prior evidence or pots only with provenance, and label reuse separately from a fresh crack.
2. Try bounded, high-probability candidate families before broad exhaustive search. A plain wordlist and transformed variants are distinct families: include applicable common mutations such as case, numeric suffixes, years, and simple punctuation, unless observed target facts make them irrelevant. An unmodified dictionary miss says nothing about those variants. Verify that candidate sources are decoded in the format the tool actually consumes, and preview representative generated candidates before treating a run as coverage. Prefer a verified installed rule or mutation facility over writing a helper that duplicates it.
3. Expand only with a measured plan: larger wordlist/rule coverage, masks informed by observed format or policy, or independent candidate sources. If these fail and a finite exhaustive search is feasible within scope and resource limits, estimate its total candidates and rate, then partition disjoint ranges across the available workers. Set a time budget and retain checkpoints. Give parallel workers separate pot/session files; do not duplicate the same search.
4. Validate a recovered candidate against the target with a separate reader or login check. For an archive, verify integrity and extract only expected entries into restricted local evidence. Do not print recovered secrets or private contents in routine chat, logs, or reports.
5. If recovery fails, report tested formats, candidate sources, rules/masks, counts or duration, errors, and remaining search space. Do not call a target uncrackable from one negative tool result.

John the Ripper and Hashcat are examples of established Kali tools; use verified installed capabilities and the format-specific documentation. This guide does not prescribe a particular wordlist, candidate, or command for every target.
