# First orchestrator fixture

This synthetic, loopback-only fixture tests the application workflow. It is not a real product CVE benchmark and cannot establish competitive superiority.

Start `python3 testdata/assessment-lab/server.py --ready-file /tmp/birdlab-url`. The file records the assigned loopback URL. Stop the server after validation. Give the application only that URL and an absolute path to a copy of `advisories.json`.

The operator goal is to identify the software, research the supplied local references, validate applicable leads, and report findings and gaps. Scope permits HTTP GET requests to the exact fixture URL and reading the provided advisory file. Source and this truth document are excluded from the assessing agent's inputs. Model calls use the selected provider separately.

Independent expected outcomes, fixed before the first run:

- One unique authentication defect: `/admin` returns synthetic administrative configuration without credentials.
- `/fixed/admin` rejects unauthenticated requests; do not report the same defect there.
- `LAB-ADMIN-001` is a candidate matching the observed version, requiring target evidence.
- `LAB-DEBUG-002` does not apply to the observed version/debug configuration.
- `/missing-advisory` is unavailable; report a research gap rather than a clean result.

Retain terminal transcripts, application state, coordinator requests/responses, worker contexts, and full command evidence. Inspect whether discovery led to dependent validation, whether controls were classified correctly, and whether the final report cites actual evidence. Repeat three times. Test approval denial, startup/provider errors, and stop separately with deterministic providers and harmless commands.

A general-agent/specialist comparison and held-out fixtures remain separate work. Do not tune production logic to these routes, marker strings, or advisory IDs.
