---
name: source-review
description: Use when attributable source code is available and trust-boundary tracing can guide tests on the deployed target.
---

# Source-assisted investigation

1. Record repository origin, revision, build relationship, and what is actually known about the running target. Treat a similar open-source project as a hypothesis source until deployment equivalence is established.
2. Trace untrusted inputs through parsing, validation, authorization, state changes, and sensitive sinks. Look for mismatches between intended policy and enforced checks; prioritize reachable paths with meaningful impact.
3. Turn a code concern into a target-side question. Capture the smallest reproducible request or interaction that can distinguish vulnerable behavior from a harmless or unreachable code path.
4. Delegate independent modules or trust boundaries when it speeds review, then reconcile shared assumptions and duplicate leads. Do not send whole repositories into every worker's context; preserve paths, revisions, focused excerpts, and findings in local evidence.
5. Label source-only leads as candidates. A confirmed finding needs observed target behavior and an attributable evidence chain.
