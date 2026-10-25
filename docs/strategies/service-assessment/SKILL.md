---
name: service-assessment
description: Use when an observed listener or protocol needs reliable identification, exposure analysis, configuration checks, and test prioritization.
---

# Service and protocol assessment

1. Bind each observation to a target address, port or endpoint, transport, and time. Distinguish a banner, probe inference, and authenticated version evidence; avoid treating product names from one scanner as fact.
2. Identify the service's role and trust boundary. Compare accessible functionality, authentication requirements, transport protection, and configuration with the expected deployment. Follow surprising behavior with a narrow independent check. If the service advertises a related in-scope endpoint, inspect that endpoint with a bounded read-only request before treating the service inventory as complete.
3. Research product-specific leads only after identity and prerequisites are credible. Separate an exposed service, a vulnerable version hypothesis, and a reproduced weakness.
4. Before a wide port or endpoint sweep, estimate whether the chosen timing, retries, and deadline can cover the range. Prefer a short, informative first pass; expand only when the question warrants it. With Nmap, verify timing semantics rather than assuming a nominal rate makes a deliberately delayed scan fast. A host timeout can end without a usable port table.
5. Split a justified wide sweep into disjoint ranges or other independent outcomes only when parallelism helps. Assign each worker a bounded partition, a useful result even if a sibling fails, and a share of the total target probe/connection budget. Count how many commands the partition needs against the worker's decision budget. If it needs many repeated scans, use a small reviewed batch helper with progress and partial-result checkpoints, or schedule a smaller partition and return its remaining range for a later round. A per-process rate limit is not an aggregate limit across workers. Do not duplicate ranges or rerun an infeasible scan unchanged. Allocate later analysis by distinct observed services or protocols when useful. Share concise observations and evidence references, not entire scan logs; coordinate any state-changing checks.
6. Report exact probe settings, completed coverage, responses, confidence, and untested ranges. A filtered or silent endpoint is a bounded negative observation; a timed-out range is not a clean result.

Use installed protocol clients and Kali tools after verifying capabilities and obtaining execution approval.
