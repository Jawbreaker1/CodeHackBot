---
name: source-review
description: Use when attributable source code is available and trust-boundary tracing can guide tests on the deployed target.
---

# Source-assisted investigation

1. Record repository origin, revision, build relationship, and what is actually known about the running target. Treat a similar open-source project as a hypothesis source until deployment equivalence is established. Verify the selected revision in each Git read; put a commit before `--` when using a path separator.
2. Trace untrusted inputs through parsing, validation, authorization, state changes, and sensitive sinks. Look for mismatches between intended policy and enforced checks; prioritize reachable paths with meaningful impact.
3. Turn a code concern into a target-side question. Capture the smallest reproducible request or interaction that can distinguish vulnerable behavior from a harmless or unreachable code path.
4. Delegate independent modules or trust boundaries when it speeds review, then reconcile shared assumptions and duplicate leads. Do not send whole repositories into every worker's context. For a code finding, preserve the repository, pinned revision, relative path, and exact line range. To show code in Analysis, register a bounded JSON artifact with `version: 1`, `repository`, `revision`, `path`, and `lines: [{number, text}]`. Use original line numbers and actual, redacted source text; keep command output and metadata prose out of the `lines` array. Cite that exact artifact in the finding's evidence. A numbered text log is useful evidence but cannot be rendered as code without this structure.
5. Source can contain working credentials. Use focused, redacted excerpts for ordinary logs and review artifacts; keep any necessary raw copy in restricted local evidence. Preserve original line numbering and non-secret code identifiers when redacting so citations still identify the original control flow.
6. Label source-only leads as candidates. A confirmed finding needs observed target behavior and an attributable evidence chain. Reconcile each label with the tests actually recorded before writing the report.
